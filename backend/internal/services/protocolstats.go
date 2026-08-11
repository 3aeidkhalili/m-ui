package services

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

const handshakeOnlineWindow = 180 // seconds

var openvpnStatusCandidates = []string{
	"/var/log/openvpn-status.log",
	"/var/log/openvpn/status.log",
	"/run/openvpn/server.status",
	"/etc/openvpn/openvpn-status.log",
}

// ProtocolPeer is one connected peer within a protocol.
type ProtocolPeer struct {
	Name          string `json:"name"`
	IP            string `json:"ip"`
	Online        bool   `json:"online"`
	RxBytes       int64  `json:"rx_bytes"`
	TxBytes       int64  `json:"tx_bytes"`
	LastHandshake *int   `json:"last_handshake"`
}

// ProtocolStat is the live status of one protocol.
type ProtocolStat struct {
	Key        string         `json:"key"`
	Service    string         `json:"service"`
	Label      string         `json:"label"`
	Status     string         `json:"status"`
	Online     int            `json:"online"`
	RxBytes    int64          `json:"rx_bytes"`
	TxBytes    int64          `json:"tx_bytes"`
	TotalBytes int64          `json:"total_bytes"`
	Detail     string         `json:"detail"`
	Peers      []ProtocolPeer `json:"peers"`
	// Egress is set only on the Xray entry: Xray is the transit layer, so its
	// card shows the outbound/egress relays that all VPN traffic exits through,
	// not per-protocol usage.
	Egress *EgressInfo `json:"egress,omitempty"`
}

// EgressLocation describes one relay in the active egress pool.
type EgressLocation struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	CountryCode string `json:"country_code"`
	CountryName string `json:"country_name"`
	Flag        string `json:"flag"`
	EgressIP    string `json:"egress_ip"`
	Address     string `json:"address"`
}

// EgressInfo summarizes how Xray routes traffic out (direct / one relay / a
// round-robin balancer over several relays).
type EgressInfo struct {
	Mode      string           `json:"mode"` // "direct" | "single" | "balancer"
	PoolSize  int              `json:"pool_size"`
	Total     int              `json:"total"` // total outbounds configured
	Locations []EgressLocation `json:"locations"`
}

// ProtocolStats is the aggregate response of the protocol monitor.
type ProtocolStats struct {
	Protocols   []ProtocolStat `json:"protocols"`
	TotalOnline int            `json:"total_online"`
	ActiveUsers int            `json:"active_users"`
	TotalBytes  int64          `json:"total_bytes"`
	Demo        bool           `json:"demo"`
}

func runCmd(timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func emptyStat(key, service, label, status string) ProtocolStat {
	return ProtocolStat{Key: key, Service: service, Label: label, Status: status, Peers: []ProtocolPeer{}}
}

func protoWireGuard(usersByIP map[string]string) ProtocolStat {
	d := emptyStat("wireguard", "wg-quick@"+Cfg.WgInterface, "WireGuard", "unknown")
	out, ok := runCmd(5*time.Second, "wg", "show", Cfg.WgInterface, "dump")
	if !ok {
		d.Status = "inactive"
		return d
	}
	d.Status = "active"
	now := time.Now().Unix()
	for i, line := range strings.Split(out, "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) < 8 {
			continue
		}
		allowedIPs, handshake, rx, tx := c[3], c[4], c[5], c[6]
		rxI, _ := strconv.ParseInt(rx, 10, 64)
		txI, _ := strconv.ParseInt(tx, 10, 64)
		hs, _ := strconv.ParseInt(handshake, 10, 64)
		var ago *int
		online := false
		if hs != 0 {
			a := int(now - hs)
			ago = &a
			online = a < handshakeOnlineWindow
		}
		ip := ""
		if allowedIPs != "" {
			ip = strings.TrimSpace(strings.Split(allowedIPs, "/")[0])
		}
		d.RxBytes += rxI
		d.TxBytes += txI
		if online {
			d.Online++
		}
		name := usersByIP[ip]
		if name == "" {
			if ip != "" {
				name = ip
			} else {
				name = "peer"
			}
		}
		d.Peers = append(d.Peers, ProtocolPeer{Name: name, IP: ip, Online: online, RxBytes: rxI, TxBytes: txI, LastHandshake: ago})
	}
	d.TotalBytes = d.RxBytes + d.TxBytes
	sortPeers(d.Peers, true)
	return d
}

func protoOpenVPN() ProtocolStat {
	d := emptyStat("openvpn", "openvpn@server", "OpenVPN", "unknown")
	var text string
	found := false
	for _, path := range openvpnStatusCandidates {
		if b, err := os.ReadFile(path); err == nil {
			text = string(b)
			found = true
			break
		}
	}
	if !found {
		d.Status = "inactive"
		return d
	}
	d.Status = "active"
	inClients := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Common Name,") {
			inClients = true
			continue
		}
		if strings.HasPrefix(line, "ROUTING TABLE") || strings.HasPrefix(line, "GLOBAL STATS") || strings.HasPrefix(line, "END") {
			inClients = false
			continue
		}
		if !inClients {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 5 {
			continue
		}
		cn, addr := parts[0], parts[1]
		rxI, err1 := strconv.ParseInt(parts[2], 10, 64)
		txI, err2 := strconv.ParseInt(parts[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		ip := addr
		if strings.Contains(addr, ":") {
			ip = addr[:strings.LastIndex(addr, ":")]
		}
		d.Online++
		d.RxBytes += rxI
		d.TxBytes += txI
		zero := 0
		d.Peers = append(d.Peers, ProtocolPeer{Name: cn, IP: ip, Online: true, RxBytes: rxI, TxBytes: txI, LastHandshake: &zero})
	}
	d.TotalBytes = d.RxBytes + d.TxBytes
	sortPeers(d.Peers, false)
	return d
}

func protoL2TP() ProtocolStat {
	d := emptyStat("l2tp", "xl2tpd", "L2TP/IPsec", "unknown")
	matches, _ := filepath.Glob("/sys/class/net/ppp*")
	found := false
	for _, p := range matches {
		rxB, err1 := os.ReadFile(filepath.Join(p, "statistics", "rx_bytes"))
		txB, err2 := os.ReadFile(filepath.Join(p, "statistics", "tx_bytes"))
		if err1 != nil || err2 != nil {
			continue
		}
		rxI, _ := strconv.ParseInt(strings.TrimSpace(string(rxB)), 10, 64)
		txI, _ := strconv.ParseInt(strings.TrimSpace(string(txB)), 10, 64)
		found = true
		d.Online++
		d.RxBytes += rxI
		d.TxBytes += txI
		zero := 0
		d.Peers = append(d.Peers, ProtocolPeer{Name: filepath.Base(p), IP: "", Online: true, RxBytes: rxI, TxBytes: txI, LastHandshake: &zero})
	}
	if st, ok := runCmd(5*time.Second, "systemctl", "is-active", "xl2tpd"); ok && strings.TrimSpace(st) != "" {
		d.Status = strings.TrimSpace(st)
	} else if found {
		d.Status = "active"
	} else {
		d.Status = "inactive"
	}
	d.TotalBytes = d.RxBytes + d.TxBytes
	sortPeers(d.Peers, false)
	return d
}

func protoStrongswan() ProtocolStat {
	d := emptyStat("strongswan", "strongswan", "IPsec (strongSwan)", "unknown")
	out, ok := runCmd(5*time.Second, "ipsec", "status")
	if !ok {
		out, ok = runCmd(5*time.Second, "swanctl", "--list-sas")
	}
	if !ok {
		d.Status = "inactive"
		d.Detail = "لایه‌ی رمزنگاری L2TP"
		return d
	}
	d.Status = "active"
	d.Online = strings.Count(out, "ESTABLISHED") + strings.Count(out, "INSTALLED")
	d.Detail = "تونل‌های IPsec برقرار (لایه‌ی رمزنگاری L2TP)"
	return d
}

func protoXray(users []models.User) ProtocolStat {
	d := emptyStat("xray", "xray", "Xray Core", "unknown")
	if _, ok := runCmd(5*time.Second, "systemctl", "is-active", "xray"); ok {
		d.Status = "active"
	} else {
		d.Status = "inactive"
	}
	online := 0
	var total int64
	for i := range users {
		if users[i].IsActive() {
			online++
		}
		total += users[i].UsedBytes
	}
	d.Online = online
	d.TotalBytes = total
	d.Detail = "شمارنده‌ی حجمِ واحدِ همه‌ی پروتکل‌ها"
	return d
}

func sortPeers(peers []ProtocolPeer, byOnline bool) {
	sort.SliceStable(peers, func(i, j int) bool {
		if byOnline && peers[i].Online != peers[j].Online {
			return peers[i].Online // online first
		}
		return (peers[i].RxBytes + peers[i].TxBytes) > (peers[j].RxBytes + peers[j].TxBytes)
	})
}

// ---- demo data (provisioning disabled) ----
//
// A stateful, monotonic simulator: counters only ever increase and the online
// count / peer set stay stable. The previous stateless "wobble" produced totals
// that decreased between polls, so the UI-derived live rate (delta of bytes)
// went negative and the online count jumped around — exactly the wrong data the
// monitor displayed. Here each poll advances per-peer accumulators by rate*dt,
// and the Xray aggregate is the exact sum of the three protocols.

type demoPeerAcc struct{ rx, tx int64 }

type demoSpec struct {
	key, service, label, octet string
	onlineFrac, rateBps        float64
}

func demoSpecs() []demoSpec {
	return []demoSpec{
		{"wireguard", "wg-quick@" + Cfg.WgInterface, "WireGuard", "9", 0.5, 900_000},
		{"openvpn", "openvpn@server", "OpenVPN", "8", 0.35, 650_000},
		{"l2tp", "xl2tpd", "L2TP/IPsec", "10", 0.2, 380_000},
	}
}

var demoState = struct {
	mu      sync.Mutex
	lastT   time.Time
	started bool
	peers   map[string]map[string]*demoPeerAcc // protoKey -> username -> acc
}{peers: map[string]map[string]*demoPeerAcc{}}

func demoStats(users []models.User) []ProtocolStat {
	active := make([]models.User, 0, len(users))
	for i := range users {
		if users[i].IsActive() {
			active = append(active, users[i])
		}
	}
	if len(active) == 0 {
		active = users
	}
	n := len(active)
	if n < 4 {
		n = 4
	}

	demoState.mu.Lock()
	now := time.Now()
	if !demoState.started {
		demoState.lastT = now.Add(-time.Second)
		demoState.started = true
	}
	dt := now.Sub(demoState.lastT).Seconds()
	if dt <= 0 || dt > 30 {
		dt = 1 // clamp against clock jumps / long idle
	}
	demoState.lastT = now
	t := float64(now.Unix())

	for si, spec := range demoSpecs() {
		online := int(math.Round(float64(n) * spec.onlineFrac))
		if online < 1 {
			online = 1
		}
		accs := demoState.peers[spec.key]
		if accs == nil {
			accs = map[string]*demoPeerAcc{}
			demoState.peers[spec.key] = accs
		}
		keep := map[string]bool{}
		for i := 0; i < online; i++ {
			uname := "user" + strconv.Itoa(i+1)
			if len(active) > 0 {
				uname = active[i%len(active)].Username
			}
			keep[uname] = true
			acc := accs[uname]
			if acc == nil {
				// seed with an established-looking base so it does not start at 0
				base := int64(spec.rateBps * float64(300+si*150))
				acc = &demoPeerAcc{rx: base * 28 / 100, tx: base * 72 / 100}
				accs[uname] = acc
			}
			// gentle strictly-positive modulation for a live-looking rate
			mod := 0.55 + 0.35*math.Sin(t/9.0+float64(i)+float64(si))
			inc := int64(spec.rateBps / float64(online) * mod * dt)
			acc.rx += inc * 28 / 100
			acc.tx += inc * 72 / 100
		}
		for name := range accs {
			if !keep[name] {
				delete(accs, name)
			}
		}
	}

	// snapshot the accumulators under the lock
	type snapPeer struct {
		name   string
		rx, tx int64
	}
	snap := map[string][]snapPeer{}
	for key, accs := range demoState.peers {
		for name, a := range accs {
			snap[key] = append(snap[key], snapPeer{name, a.rx, a.tx})
		}
	}
	demoState.mu.Unlock()

	protoStats := []ProtocolStat{}
	var aggRx, aggTx int64
	for _, spec := range demoSpecs() {
		peers := snap[spec.key]
		sort.Slice(peers, func(i, j int) bool { return peers[i].name < peers[j].name })
		d := emptyStat(spec.key, spec.service, spec.label, "active")
		for i, p := range peers {
			hs := 12 + (i*9)%50
			d.Peers = append(d.Peers, ProtocolPeer{
				Name: p.name, IP: "10." + spec.octet + ".0." + strconv.Itoa(11+i),
				Online: true, RxBytes: p.rx, TxBytes: p.tx, LastHandshake: &hs,
			})
			d.RxBytes += p.rx
			d.TxBytes += p.tx
		}
		d.Online = len(peers)
		d.TotalBytes = d.RxBytes + d.TxBytes
		sortPeers(d.Peers, false)
		aggRx += d.RxBytes
		aggTx += d.TxBytes
		protoStats = append(protoStats, d)
	}

	out := []ProtocolStat{}
	xr := emptyStat("xray", "xray", "Xray Core", "active")
	xr.Online = len(active)
	xr.RxBytes = aggRx
	xr.TxBytes = aggTx
	xr.TotalBytes = aggRx + aggTx
	xr.Detail = "شمارنده‌ی حجمِ واحدِ همه‌ی پروتکل‌ها (نمایشی)"
	out = append(out, xr)
	out = append(out, protoStats...)

	ss := emptyStat("strongswan", "strongswan", "IPsec (strongSwan)", "inactive")
	ss.Detail = "لایه‌ی رمزنگاری L2TP (نمایشی)"
	out = append(out, ss)
	return out
}

// ---- cache + public API ----

var (
	statsCacheMu   sync.Mutex
	statsCacheAt   time.Time
	statsCacheData *ProtocolStats
)

const statsCacheTTL = 2500 * time.Millisecond

// buildEgressInfo summarizes the active outbound pool that Xray routes all VPN
// traffic through (direct freedom, a single relay, or a round-robin balancer).
func buildEgressInfo(db *gorm.DB) *EgressInfo {
	all := OutboundsList(db)
	active := GetActiveGroup(db)
	info := &EgressInfo{Total: len(all), PoolSize: len(active), Locations: []EgressLocation{}}
	switch {
	case len(active) == 0:
		info.Mode = "direct"
	case len(active) == 1:
		info.Mode = "single"
	default:
		info.Mode = "balancer"
	}
	for i := range active {
		o := &active[i]
		info.Locations = append(info.Locations, EgressLocation{
			ID:          o.ID,
			Name:        o.Name,
			Protocol:    o.Protocol,
			CountryCode: lower(o.CountryCode),
			CountryName: o.CountryName,
			Flag:        flagURL(lower(o.CountryCode)),
			EgressIP:    o.EgressIP,
			Address:     o.Address,
		})
	}
	return info
}

// CollectProtocolStats returns per-protocol live stats, cached briefly to bound
// the subprocess spawn rate independent of request rate.
func CollectProtocolStats(db *gorm.DB) ProtocolStats {
	statsCacheMu.Lock()
	if statsCacheData != nil && time.Since(statsCacheAt) < statsCacheTTL {
		d := *statsCacheData
		statsCacheMu.Unlock()
		return d
	}
	statsCacheMu.Unlock()

	var users []models.User
	db.Find(&users)

	var protocols []ProtocolStat
	demo := false
	if !Cfg.ProvisioningEnabled {
		protocols = demoStats(users)
		demo = true
	} else {
		usersByIP := map[string]string{}
		for i := range users {
			usersByIP[users[i].WgIP()] = users[i].Username
		}
		protocols = []ProtocolStat{
			protoXray(users),
			protoWireGuard(usersByIP),
			protoOpenVPN(),
			protoL2TP(),
			protoStrongswan(),
		}
	}

	// Attach the egress/outbound summary to the Xray card (transit layer).
	egress := buildEgressInfo(db)
	for i := range protocols {
		if protocols[i].Key == "xray" {
			protocols[i].Egress = egress
		}
	}

	totalOnline := 0
	var xrayOnline int
	var xrayTotal int64
	for _, p := range protocols {
		if p.Key == "xray" {
			xrayOnline = p.Online
			xrayTotal = p.TotalBytes
		} else {
			totalOnline += p.Online
		}
	}
	result := ProtocolStats{
		Protocols:   protocols,
		TotalOnline: totalOnline,
		ActiveUsers: xrayOnline,
		TotalBytes:  xrayTotal,
		Demo:        demo,
	}
	statsCacheMu.Lock()
	statsCacheAt = time.Now()
	statsCacheData = &result
	statsCacheMu.Unlock()
	return result
}
