package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// speedTestURL is a globally-reachable, uncached fixed-size download used to
// measure a relay's real egress throughput.
const speedTestURL = "https://speed.cloudflare.com/__down?bytes=20000000" // 20 MB

// OutboundSpeed is one relay benchmark result.
type OutboundSpeed struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	OK        bool    `json:"ok"`
	Mbps      float64 `json:"mbps"`
	LatencyMs int     `json:"latency_ms"`
	EgressIP  string  `json:"egress_ip"`
	Error     string  `json:"error"`
}

// sysctlTuning is the host network profile for fast long-haul VPN egress.
const sysctlTuning = `# MultiVPN network speed tuning (BBR + FQ + large buffers)
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_mtu_probing=1
net.ipv4.tcp_slow_start_after_idle=0
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216
net.core.netdev_max_backlog=16384
net.ipv4.tcp_notsent_lowat=16384
`

// TuneNetwork enables BBR congestion control, the FQ qdisc, TCP Fast Open and
// larger socket buffers on the host — the biggest single speed win for VPN
// traffic crossing lossy international links. Idempotent.
func TuneNetwork() (bool, string) {
	if !Cfg.ProvisioningEnabled {
		return false, "در حالت توسعه اجرا نمی‌شود"
	}
	_ = exec.Command("modprobe", "tcp_bbr").Run()
	const path = "/etc/sysctl.d/99-multivpn-speed.conf"
	if err := os.WriteFile(path, []byte(sysctlTuning), 0o644); err != nil {
		return false, "نوشتن sysctl ناموفق: " + err.Error()
	}
	if out, err := exec.Command("sysctl", "-p", path).CombinedOutput(); err != nil {
		return false, "اعمال sysctl ناموفق: " + strings.TrimSpace(string(out))
	}
	cc, _ := runCmd(3*time.Second, "sysctl", "-n", "net.ipv4.tcp_congestion_control")
	return true, "congestion=" + strings.TrimSpace(cc)
}

// spinTempXray starts a throwaway Xray exposing a local SOCKS proxy for one
// outbound; returns the port and a stop func.
func spinTempXray(ob map[string]any) (int, func(), error) {
	if !Cfg.ProvisioningEnabled || Cfg.XrayBin == "" {
		return 0, nil, fmt.Errorf("xray در دسترس نیست")
	}
	if _, err := os.Stat(Cfg.XrayBin); err != nil {
		return 0, nil, fmt.Errorf("باینری xray یافت نشد")
	}
	port := freePort()
	obc := map[string]any{}
	for k, v := range ob {
		obc[k] = v
	}
	obc["tag"] = "proxy"
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{{
			"tag": "in", "listen": "127.0.0.1", "port": port,
			"protocol": "socks", "settings": map[string]any{"udp": false},
		}},
		"outbounds": []any{obc},
	}
	tmp, err := os.CreateTemp("", "xray-bench-*.json")
	if err != nil {
		return 0, nil, err
	}
	path := tmp.Name()
	b, _ := json.Marshal(cfg)
	_, _ = tmp.Write(b)
	tmp.Close()

	cmd := exec.Command(Cfg.XrayBin, "run", "-c", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		os.Remove(path)
		return 0, nil, err
	}
	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		os.Remove(path)
	}
	time.Sleep(1200 * time.Millisecond) // let Xray boot
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		msg := lastLine(stderr.String())
		stop()
		return 0, nil, fmt.Errorf("xray بوت نشد: %s", msg)
	}
	return port, stop, nil
}

// benchmarkOutbound measures a relay's egress throughput (Mbps) and latency by
// downloading a fixed payload through a temporary SOCKS proxy.
func benchmarkOutbound(ob map[string]any) OutboundSpeed {
	r := OutboundSpeed{}
	port, stop, err := spinTempXray(ob)
	if err != nil {
		r.Error = clip(err.Error(), 160)
		return r
	}
	defer stop()
	proxy := fmt.Sprintf("socks5h://127.0.0.1:%d", port)

	if out, ok := runCmd(8*time.Second, "curl", "-s", "--max-time", "6", "-x", proxy,
		"-o", "/dev/null", "-w", "%{time_connect}", "https://www.cloudflare.com/cdn-cgi/trace"); ok {
		if f, _ := strconv.ParseFloat(strings.TrimSpace(out), 64); f > 0 {
			r.LatencyMs = int(f * 1000)
		}
	}

	out, ok := runCmd(30*time.Second, "curl", "-s", "--max-time", "22", "-x", proxy,
		"-o", "/dev/null", "-w", "%{speed_download}", speedTestURL)
	if !ok {
		r.Error = "دانلودِ تستِ سرعت ناموفق بود (اینترنتِ رله؟)"
		return r
	}
	bps, _ := strconv.ParseFloat(strings.TrimSpace(out), 64)
	if bps <= 0 {
		r.Error = "سرعتِ صفر — رله ترافیک را عبور نداد"
		return r
	}
	r.Mbps = bps * 8 / 1e6
	r.OK = true
	if o, ok2 := runCmd(8*time.Second, "curl", "-s", "--max-time", "5", "-x", proxy, "https://api.ipify.org"); ok2 {
		if s := strings.TrimSpace(o); s != "" && len(s) < 60 {
			r.EgressIP = s
		}
	}
	return r
}

// OptimizeOutbounds tunes the host network, benchmarks every relay's real
// throughput, and returns the results ranked fastest-first. When apply is set it
// also activates only the fastest relay(s) so clients egress through them.
func OptimizeOutbounds(db *gorm.DB, apply bool) []OutboundSpeed {
	testMu.Lock() // serialize temp-Xray spawns with the connectivity tester
	defer testMu.Unlock()

	var rows []models.Outbound
	db.Order("id").Find(&rows)

	results := make([]OutboundSpeed, 0, len(rows))
	for i := range rows {
		var ob map[string]any
		if json.Unmarshal([]byte(rows[i].ConfigJSON), &ob) != nil {
			continue
		}
		res := benchmarkOutbound(ob)
		res.ID, res.Name, res.Address = rows[i].ID, rows[i].Name, rows[i].Address
		results = append(results, res)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK
		}
		return results[i].Mbps > results[j].Mbps
	})

	if apply {
		var best float64
		for _, r := range results {
			if r.OK && r.Mbps > best {
				best = r.Mbps
			}
		}
		if best > 0 {
			// keep relays within 60% of the fastest active; drop the slow ones
			keep := map[int]bool{}
			for _, r := range results {
				if r.OK && r.Mbps >= best*0.6 {
					keep[r.ID] = true
				}
			}
			for i := range rows {
				db.Model(&models.Outbound{}).Where("id = ?", rows[i].ID).
					Update("is_active", keep[rows[i].ID])
			}
			SyncXray(db)
			AuditLog(db, LogSystem, "info", "system",
				fmt.Sprintf("بهینه‌سازیِ سرعت: %d رله‌ی سریع فعال شد (بهترین %.1f Mbps)", len(keep), best))
			log.Printf("optimize: activated %d fast relays (best %.1f Mbps)", len(keep), best)
		}
	}
	return results
}
