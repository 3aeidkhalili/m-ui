package services

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// collectOnce reads Xray usage deltas, accumulates them atomically, and re-syncs
// the config if the set of active users changed (quota/expiry crossings).
func collectOnce(db *gorm.DB) {
	if !Cfg.ProvisioningEnabled {
		return
	}
	traffic, err := Xray.QueryOutboundTraffic(true)
	if err != nil {
		log.Printf("traffic job error: %v", err)
		return
	}
	for tag, delta := range traffic {
		uid, ok := tagToID(tag)
		if !ok || delta == 0 {
			continue
		}
		db.Model(&models.User{}).Where("id = ?", uid).
			UpdateColumn("used_bytes", gorm.Expr("used_bytes + ?", delta))
	}

	// Enforce per-user IP limits before recomputing the active set, so an account
	// blocked this tick drops out of the Xray config on the same sync below.
	EnforceIPLimits(db)

	current := ActiveUserIDs(db)
	prev, ok := LastSyncedSnapshot()
	if !ok || !sameSet(current, prev) {
		SyncXray(db)
		SyncBandwidth(db) // add/remove shaping as users cross active/inactive
	}
}

// TrafficLoop runs collectOnce every TrafficJobInterval until ctx is cancelled.
func TrafficLoop(ctx context.Context, db *gorm.DB) {
	interval := time.Duration(Cfg.TrafficJobInterval) * time.Second
	log.Printf("traffic job started (interval=%s)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("traffic loop recovered: %v", r)
					}
				}()
				collectOnce(db)
			}()
		}
	}
}

func sameSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
