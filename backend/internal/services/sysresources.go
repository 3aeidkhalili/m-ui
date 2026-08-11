package services

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ResourceStats is a snapshot of live server resource usage for the panel header.
type ResourceStats struct {
	CPUPct    float64 `json:"cpu_pct"`
	MemUsed   uint64  `json:"mem_used"`
	MemTotal  uint64  `json:"mem_total"`
	MemPct    float64 `json:"mem_pct"`
	DiskUsed  uint64  `json:"disk_used"`
	DiskTotal uint64  `json:"disk_total"`
	DiskPct   float64 `json:"disk_pct"`
	NetRxBps  float64 `json:"net_rx_bps"`
	NetTxBps  float64 `json:"net_tx_bps"`
	Cores     int     `json:"cores"`
	UptimeSec int64   `json:"uptime_sec"`
}

var (
	resMu         sync.Mutex
	prevCPUIdle   uint64
	prevCPUTotal  uint64
	prevNetRx     uint64
	prevNetTx     uint64
	prevResAt     time.Time
	resCache      ResourceStats
	resCacheAt    time.Time
	resHaveSample bool
)

const resCacheTTL = 1500 * time.Millisecond

// CollectResourceStats returns live CPU/RAM/disk/network usage. CPU% and network
// rate are computed from the delta since the previous call, so they are 0 on the
// first sample. Cached briefly so multiple viewers share one read.
func CollectResourceStats() ResourceStats {
	resMu.Lock()
	defer resMu.Unlock()
	if resHaveSample && time.Since(resCacheAt) < resCacheTTL {
		return resCache
	}
	now := time.Now()
	st := ResourceStats{Cores: cpuCores()}

	// ---- CPU (/proc/stat) ----
	if idle, total, ok := readCPU(); ok {
		if resHaveSample && total > prevCPUTotal {
			dTotal := float64(total - prevCPUTotal)
			dIdle := float64(idle - prevCPUIdle)
			if dTotal > 0 {
				st.CPUPct = clampPct((1 - dIdle/dTotal) * 100)
			}
		}
		prevCPUIdle, prevCPUTotal = idle, total
	}

	// ---- Memory (/proc/meminfo) ----
	st.MemTotal, st.MemUsed = readMem()
	if st.MemTotal > 0 {
		st.MemPct = clampPct(float64(st.MemUsed) / float64(st.MemTotal) * 100)
	}

	// ---- Disk (statfs /) ----
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil {
		bs := uint64(fs.Bsize)
		st.DiskTotal = fs.Blocks * bs
		st.DiskUsed = (fs.Blocks - fs.Bfree) * bs
		if st.DiskTotal > 0 {
			st.DiskPct = clampPct(float64(st.DiskUsed) / float64(st.DiskTotal) * 100)
		}
	}

	// ---- Network (/proc/net/dev), summed over physical ifaces ----
	if rx, tx, ok := readNet(); ok {
		if resHaveSample {
			dt := now.Sub(prevResAt).Seconds()
			if dt > 0 {
				if rx >= prevNetRx {
					st.NetRxBps = float64(rx-prevNetRx) / dt
				}
				if tx >= prevNetTx {
					st.NetTxBps = float64(tx-prevNetTx) / dt
				}
			}
		}
		prevNetRx, prevNetTx = rx, tx
	}

	st.UptimeSec = readUptime()

	prevResAt = now
	resHaveSample = true
	resCache = st
	resCacheAt = now
	return st
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func cpuCores() int {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 1
	}
	n := strings.Count(string(b), "processor\t")
	if n < 1 {
		n = 1
	}
	return n
}

func readCPU() (idle, total uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line := b
	if i := strings.IndexByte(string(b), '\n'); i >= 0 {
		line = b[:i]
	}
	f := strings.Fields(string(line))
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	var sum uint64
	for i := 1; i < len(f); i++ {
		v, _ := strconv.ParseUint(f[i], 10, 64)
		sum += v
		if i == 4 || i == 5 { // idle + iowait
			idle += v
		}
	}
	return idle, sum, true
}

func readMem() (total, used uint64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var avail uint64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(f[1], 10, 64)
		v *= 1024 // kB -> bytes
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total >= avail {
		used = total - avail
	}
	return total, used
}

func readNet() (rx, tx uint64, ok bool) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		if name == "lo" || strings.HasPrefix(name, "ppp") || strings.HasPrefix(name, "tun") ||
			strings.HasPrefix(name, "wg") {
			continue // skip loopback + VPN ifaces to measure real uplink
		}
		f := strings.Fields(line[i+1:])
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, true
}

func readUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return int64(v)
}
