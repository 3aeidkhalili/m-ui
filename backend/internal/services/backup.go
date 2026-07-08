package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// BackupDB writes a consistent single-file snapshot of the SQLite DB (VACUUM
// INTO checkpoints the WAL into one clean file) and returns its path. The caller
// serves then removes it.
func BackupDB(db *gorm.DB) (string, error) {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("multivpn-backup-%d.db", time.Now().UnixNano()))
	_ = os.Remove(tmp)
	if err := db.Exec("VACUUM INTO ?", tmp).Error; err != nil {
		return "", err
	}
	return tmp, nil
}

// ReconcileProvisioning rebuilds every derivable system-side artifact from the
// DB: L2TP chap-secrets, OpenVPN ccd + password store, and WireGuard peers. Run
// after a restore so users that were deleted-then-restored work again on the
// same server. Best-effort; errors are logged, not fatal.
func ReconcileProvisioning(db *gorm.DB) {
	if !Cfg.ProvisioningEnabled {
		return
	}
	var users []models.User
	db.Find(&users)
	if len(users) > 10000 { // blast-radius guard (validation already caps uploads)
		log.Printf("reconcile: %d users exceeds cap; skipping", len(users))
		return
	}
	log.Printf("reconcile: rebuilding system state for %d users", len(users))
	for i := range users {
		u := &users[i]
		if _, err := callScript("l2tp_add.sh", u.Username, u.L2tpPassword, u.L2tpIP()); err != nil {
			log.Printf("reconcile l2tp %s: %v", u.Username, err)
		}
		// ovpn_add.sh restores the ccd static-IP file and the ovpn-auth password
		// entry (the rebuilt cert is unused under verify-client-cert none).
		if _, err := callScript("ovpn_add.sh", u.Username, u.OvpnIP(), u.L2tpPassword); err != nil {
			log.Printf("reconcile ovpn %s: %v", u.Username, err)
		}
		reconcileWGPeer(u)
	}
	SyncXray(db)
	SyncBandwidth(db)
	AuditLog(db, LogSystem, "warn", "system", fmt.Sprintf("بازیابیِ بکاپ اعمال شد؛ وضعیتِ %d کاربر بازسازی شد", len(users)))
}

// reconcileWGPeer re-adds a user's WireGuard peer with the STORED keys (wg_add.sh
// would generate new keys and break existing client configs).
func reconcileWGPeer(u *models.User) {
	if u.WgPublicKey == "" {
		return
	}
	args := []string{"set", Cfg.WgInterface, "peer", u.WgPublicKey, "allowed-ips", u.WgIP() + "/32"}
	var pskFile string
	if u.WgPresharedKey != "" {
		f, err := os.CreateTemp("", "wgpsk")
		if err == nil {
			_, _ = f.WriteString(u.WgPresharedKey)
			_ = f.Close()
			pskFile = f.Name()
			args = append(args, "preshared-key", pskFile)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "wg", args...).Run(); err != nil {
		log.Printf("reconcile wg %s: %v", u.Username, err)
	}
	if pskFile != "" {
		_ = os.Remove(pskFile)
	}
}
