package services

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

const l2tpBWDir = "/run/multivpn/bw"

// DefaultBandwidth is the global "default_bandwidth_mbps" setting (0 = unlimited),
// applied to any user without a per-user BandwidthMbps override.
func DefaultBandwidth(db *gorm.DB) int {
	n, _ := strconv.Atoi(strings.TrimSpace(SettingsGet(db, "default_bandwidth_mbps")))
	if n < 0 {
		n = 0
	}
	return n
}

// EffectiveBandwidth is the download cap (Mbps) that applies to a user: their
// per-user BandwidthMbps when set (>0), otherwise the global default.
func EffectiveBandwidth(db *gorm.DB, u *models.User) int {
	if u.BandwidthMbps > 0 {
		return u.BandwidthMbps
	}
	return DefaultBandwidth(db)
}

// SyncBandwidth reconciles per-user download shaping (tc HTB) on the VPN
// interfaces from the users' effective bandwidth caps. tun0/wg0 are static;
// L2TP's ppp interfaces are dynamic, so their caps are also written to
// /run/multivpn/bw/<internalIP> for the ip-up hook to apply on connect.
func SyncBandwidth(db *gorm.DB) {
	if !Cfg.ProvisioningEnabled {
		return
	}
	var users []models.User
	db.Find(&users)
	def := DefaultBandwidth(db)

	var tun, wg []string
	l2tp := map[string]int{} // L2TP internal IP -> mbit
	for i := range users {
		u := &users[i]
		if !u.IsActive() {
			continue
		}
		mbit := u.BandwidthMbps
		if mbit <= 0 {
			mbit = def
		}
		if mbit <= 0 {
			continue // unlimited -> no class
		}
		tun = append(tun, fmt.Sprintf("%s:%d", u.OvpnIP(), mbit))
		wg = append(wg, fmt.Sprintf("%s:%d", u.WgIP(), mbit))
		l2tp[u.L2tpIP()] = mbit
	}

	if _, err := callScript("bw_apply.sh", append([]string{"tun0"}, tun...)...); err != nil {
		log.Printf("bandwidth sync tun0: %v", err)
	}
	if _, err := callScript("bw_apply.sh", append([]string{Cfg.WgInterface}, wg...)...); err != nil {
		log.Printf("bandwidth sync %s: %v", Cfg.WgInterface, err)
	}
	syncL2TPBandwidth(l2tp)
}

// syncL2TPBandwidth writes the per-IP cap files for the ip-up hook and applies
// the cap to any already-live ppp interface.
func syncL2TPBandwidth(l2tp map[string]int) {
	_ = os.MkdirAll(l2tpBWDir, 0o755)
	if entries, err := os.ReadDir(l2tpBWDir); err == nil {
		for _, e := range entries {
			_ = os.Remove(filepath.Join(l2tpBWDir, e.Name()))
		}
	}
	for ip, mbit := range l2tp {
		_ = os.WriteFile(filepath.Join(l2tpBWDir, ip), []byte(strconv.Itoa(mbit)), 0o644)
	}
	// apply to currently-live ppp interfaces
	ifaces, _ := filepath.Glob("/sys/class/net/ppp*")
	for _, p := range ifaces {
		iface := filepath.Base(p)
		out, ok := runCmd(3*time.Second, "ip", "-o", "addr", "show", "dev", iface)
		if !ok {
			continue
		}
		m := rePppPeer.FindStringSubmatch(out)
		if m == nil {
			continue
		}
		if mbit, ok := l2tp[m[1]]; ok {
			_, _ = callScript("bw_apply.sh", iface, fmt.Sprintf("%s:%d", m[1], mbit))
		} else {
			_, _ = callScript("bw_apply.sh", iface) // no cap -> unshape
		}
	}
}
