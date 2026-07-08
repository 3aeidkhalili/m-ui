package services

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

const (
	ipStrikeCooldown = 10 * time.Minute // min gap between two strikes for a user
	ipMaxStrikes     = 3                // strikes before the account is auto-disabled
	l2tpIPDir        = "/run/multivpn/l2tp"
)

// hostOf strips the :port from an "ip:port" endpoint.
func hostOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || addr == "(none)" {
		return ""
	}
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	if i := strings.LastIndex(addr, ":"); i > 0 && !strings.Contains(addr[i+1:], ":") {
		return addr[:i]
	}
	return addr
}

// connInfo is what a user is currently connected from: the set of distinct
// public source IPs and the number of concurrent sessions (devices).
type connInfo struct {
	ips      map[string]bool
	sessions int
}

// count is the effective connection count used against the limit: the max of
// distinct public IPs and concurrent sessions. IPs catch account-sharing across
// locations; sessions catch several devices behind one NAT — the max blocks both.
func (c *connInfo) count() int {
	if len(c.ips) > c.sessions {
		return len(c.ips)
	}
	return c.sessions
}

// currentConns returns username -> connInfo across OpenVPN, WireGuard and L2TP.
func currentConns(users []models.User) map[string]*connInfo {
	byUser := map[string]*connInfo{}
	get := func(user string) *connInfo {
		user = strings.TrimSpace(user)
		if user == "" {
			return nil
		}
		if byUser[user] == nil {
			byUser[user] = &connInfo{ips: map[string]bool{}}
		}
		return byUser[user]
	}
	// session: a live connection (counts toward both sessions and the IP set).
	session := func(user, ip string) {
		if ci := get(user); ci != nil {
			ci.sessions++
			if ip = strings.TrimSpace(ip); ip != "" {
				ci.ips[ip] = true
			}
		}
	}
	// ipOnly: contribute an IP without counting a session (dedup fallback).
	ipOnly := func(user, ip string) {
		if ci := get(user); ci != nil {
			if ip = strings.TrimSpace(ip); ip != "" {
				ci.ips[ip] = true
			}
		}
	}

	// OpenVPN: status log carries the CN (username) + the real public address.
	for _, p := range protoOpenVPN().Peers {
		session(p.Name, hostOf(p.IP))
	}

	// WireGuard: dump carries the endpoint (public) and allowed-ips (internal -> user).
	wgByIP := map[string]string{}
	for i := range users {
		wgByIP[users[i].WgIP()] = users[i].Username
	}
	if out, ok := runCmd(5*time.Second, "wg", "show", Cfg.WgInterface, "dump"); ok {
		now := time.Now().Unix()
		for i, line := range strings.Split(out, "\n") {
			if i == 0 || strings.TrimSpace(line) == "" {
				continue
			}
			c := strings.Split(line, "\t")
			if len(c) < 8 {
				continue
			}
			endpoint, allowed, handshake := c[2], c[3], c[4]
			hs, _ := strconv.ParseInt(strings.TrimSpace(handshake), 10, 64)
			if hs == 0 || int(now-hs) >= handshakeOnlineWindow {
				continue // not currently online
			}
			internal := ""
			if allowed != "" {
				internal = strings.TrimSpace(strings.Split(allowed, "/")[0])
			}
			if u := wgByIP[internal]; u != "" {
				session(u, hostOf(endpoint))
			}
		}
	}

	// L2TP: every live pppd session -> its client's public IP, mapped to the user
	// via the session's internal IP. Two independent sources are unioned:
	//   (a) the xl2tpd journal (authoritative; covers sessions that predate the
	//       ip-up hook and multiple devices of one user), and
	//   (b) the ip-up hook's /run/multivpn/l2tp/<internalIP> files (fallback when
	//       the journal has rotated).
	idxToUser := map[int]string{}
	for i := range users {
		idxToUser[users[i].Index] = users[i].Username
	}
	for _, ui := range l2tpJournalIPs(idxToUser) {
		session(ui.user, ui.ip) // each live pppd is one L2TP session
	}
	// run-files contribute IPs only (fallback) — session count comes from the
	// journal above, so a stale/duplicate file cannot inflate the device count.
	if entries, err := os.ReadDir(l2tpIPDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			internal := e.Name()
			if u := idxToUser[models.L2tpIndexForIP(internal)]; u != "" {
				if b, err := os.ReadFile(filepath.Join(l2tpIPDir, internal)); err == nil {
					ipOnly(u, hostOf(string(b)))
				}
			}
		}
	}
	return byUser
}

var (
	rePppPeer    = regexp.MustCompile(`peer (\d+\.\d+\.\d+\.\d+)`)
	reUsingIface = regexp.MustCompile(`pppd\[(\d+)\]:\s*Using interface (ppp\d+)`)
	reCallEst    = regexp.MustCompile(`Call established with (\d+\.\d+\.\d+\.\d+), PID: (\d+)`)
)

type userIP struct{ user, ip string }

// l2tpJournalIPs returns (username, publicIP) for every currently-live L2TP
// session, correlating the pppd PID across three signals:
//   xl2tpd journal: "Call established with <pubIP>, PID: <pid>"  -> pid -> pubIP
//   pppd  journal:  "pppd[<pid>]: Using interface <iface>"       -> pid -> iface
//   live kernel:    ip addr show <iface> -> "peer <internalIP>"  -> iface -> user
// pppd PID -> {iface, publicIP}, cached across ticks so the xl2tpd journal is
// parsed only when a *new* pppd appears (not every 30s tick over a 24h window).
var (
	l2tpCacheMu  sync.Mutex
	l2tpPidPub   = map[string]string{}
	l2tpPidIface = map[string]string{}
)

func l2tpJournalIPs(idxToUser map[int]string) []userIP {
	if matches, _ := filepath.Glob("/sys/class/net/ppp*"); len(matches) == 0 {
		l2tpCacheMu.Lock()
		l2tpPidPub, l2tpPidIface = map[string]string{}, map[string]string{}
		l2tpCacheMu.Unlock()
		return nil
	}
	// One batched `ip -o -4 addr show` -> iface -> username (via internal peer IP).
	ifaceUser := map[string]string{}
	if out, ok := runCmd(3*time.Second, "ip", "-o", "-4", "addr", "show"); ok {
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || !strings.HasPrefix(f[1], "ppp") {
				continue
			}
			if m := rePppPeer.FindStringSubmatch(line); m != nil {
				if u := idxToUser[models.L2tpIndexForIP(m[1])]; u != "" {
					ifaceUser[f[1]] = u
				}
			}
		}
	}
	if len(ifaceUser) == 0 {
		return nil
	}
	pidsOut, ok := runCmd(3*time.Second, "pgrep", "-x", "pppd")
	if !ok {
		return nil
	}
	alive := strings.Fields(pidsOut)

	l2tpCacheMu.Lock()
	defer l2tpCacheMu.Unlock()
	aliveSet := make(map[string]bool, len(alive))
	needParse := false
	for _, pid := range alive {
		aliveSet[pid] = true
		if _, cached := l2tpPidPub[pid]; !cached {
			needParse = true // a fresh session -> its journal line is seconds old
		}
	}
	if needParse {
		if out, ok := runCmd(6*time.Second, "journalctl", "-u", "xl2tpd", "--since", "-5min", "-o", "short", "--no-pager"); ok {
			for _, line := range strings.Split(out, "\n") {
				if m := reUsingIface.FindStringSubmatch(line); m != nil {
					l2tpPidIface[m[1]] = m[2]
				}
				if m := reCallEst.FindStringSubmatch(line); m != nil {
					l2tpPidPub[m[2]] = m[1]
				}
			}
		}
	}
	for pid := range l2tpPidPub { // prune ended sessions
		if !aliveSet[pid] {
			delete(l2tpPidPub, pid)
			delete(l2tpPidIface, pid)
		}
	}

	var res []userIP
	for _, pid := range alive {
		iface, pub := l2tpPidIface[pid], l2tpPidPub[pid]
		if iface != "" && pub != "" {
			if u := ifaceUser[iface]; u != "" {
				res = append(res, userIP{u, pub})
			}
		}
	}
	return res
}

// EnforceIPLimits records a warning (and, after ipMaxStrikes, disables the user)
// for every active user whose connection count exceeds IPLimit. The count is
// max(distinct public IPs, concurrent sessions). A strike is rate-limited to one
// per ipStrikeCooldown so a sustained violation cannot block within seconds.
func EnforceIPLimits(db *gorm.DB) {
	if !Cfg.ProvisioningEnabled {
		return
	}
	var users []models.User
	db.Find(&users)
	enforceIPLimits(db, users, currentConns(users), time.Now().UTC(), DefaultIPLimit(db))
}

// DefaultIPLimit is the global "default_ip_limit" setting (0 = unlimited),
// applied to any user without a per-user IPLimit override.
func DefaultIPLimit(db *gorm.DB) int {
	n, _ := strconv.Atoi(strings.TrimSpace(SettingsGet(db, "default_ip_limit")))
	if n < 0 {
		n = 0
	}
	return n
}

// EffectiveIPLimit is the limit that applies to a user: their per-user IPLimit
// when set (>0), otherwise the global default.
func EffectiveIPLimit(db *gorm.DB, u *models.User) int {
	if u.IPLimit > 0 {
		return u.IPLimit
	}
	return DefaultIPLimit(db)
}

// enforceIPLimits is the pure decision core (connection source + default limit
// injected) so it can be unit-tested without live tunnels.
func enforceIPLimits(db *gorm.DB, users []models.User, conns map[string]*connInfo, now time.Time, defaultLimit int) {
	for i := range users {
		u := &users[i]
		limit := u.IPLimit
		if limit <= 0 {
			limit = defaultLimit // fall back to the global default
		}
		if limit <= 0 || !u.IsActive() {
			continue
		}
		ci := conns[u.Username]
		if ci == nil || ci.count() <= limit {
			continue
		}
		// cooldown: skip if a recent ip_limit alert already exists.
		var last models.Alert
		if err := db.Where("user_id = ? AND kind = ?", u.ID, "ip_limit").
			Order("created_at DESC").First(&last).Error; err == nil &&
			now.Sub(last.CreatedAt.UTC()) < ipStrikeCooldown {
			continue
		}

		list := sortedKeys(ci.ips)
		n := ci.count()
		strike := u.Strikes + 1
		db.Model(&models.User{}).Where("id = ?", u.ID).UpdateColumn("strikes", strike)
		db.Create(&models.Alert{
			UserID:    u.ID,
			Kind:      "ip_limit",
			Message:   fmt.Sprintf("اتصال از %d دستگاه / %d IP مجزا (حد مجاز: %d) — هشدار %d از %d", ci.sessions, len(ci.ips), limit, strike, ipMaxStrikes),
			IPCount:   n,
			IPs:       strings.Join(list, ", "),
			Strike:    strike,
			CreatedAt: now,
		})
		log.Printf("ip-limit: user %s strike %d/%d (count=%d sessions=%d ips=%d: %s)", u.Username, strike, ipMaxStrikes, n, ci.sessions, len(ci.ips), strings.Join(list, ", "))
		AuditLog(db, LogIPLimit, "warn", u.Username,
			fmt.Sprintf("تخطی از حد IP — %d دستگاه/%d IP (حد %d)، هشدار %d از %d [%s]", ci.sessions, len(ci.ips), limit, strike, ipMaxStrikes, strings.Join(list, ", ")))

		if strike >= ipMaxStrikes {
			db.Model(&models.User{}).Where("id = ?", u.ID).UpdateColumn("enabled", false)
			u.Enabled = false
			db.Create(&models.Alert{
				UserID:    u.ID,
				Kind:      "blocked",
				Message:   fmt.Sprintf("اکانت به‌دلیل %d بار تخطی از حد IP مسدود شد", ipMaxStrikes),
				Strike:    strike,
				CreatedAt: now,
			})
			log.Printf("ip-limit: user %s BLOCKED after %d strikes", u.Username, strike)
			AuditLog(db, LogIPLimit, "critical", u.Username,
				fmt.Sprintf("اکانت به‌دلیل %d بار تخطی از حد IP مسدود شد", ipMaxStrikes))
		}
	}
}

// RecentAlerts returns a user's most recent alerts (newest first).
func RecentAlerts(db *gorm.DB, userID, limit int) []models.Alert {
	var out []models.Alert
	db.Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Find(&out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
