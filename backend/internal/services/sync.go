package services

import (
	"log"
	"sync"

	"gorm.io/gorm"

	"multivpn/internal/models"
	"multivpn/internal/xray"
)

// syncState guards the Xray config regeneration and the record of which active
// users were last written (single source of truth), across the request threads
// and the background traffic goroutine.
var (
	syncMu        sync.Mutex
	lastSynced    map[int]bool
	lastSyncedSet bool
)

// ActiveUsers returns users that should be present in the Xray config.
func ActiveUsers(db *gorm.DB) []models.User {
	var all []models.User
	db.Order("id").Find(&all)
	out := make([]models.User, 0, len(all))
	for _, u := range all {
		if u.IsActive() {
			out = append(out, u)
		}
	}
	return out
}

// ActiveUserIDs returns the set of active user ids.
func ActiveUserIDs(db *gorm.DB) map[int]bool {
	m := map[int]bool{}
	for _, u := range ActiveUsers(db) {
		m[u.ID] = true
	}
	return m
}

// InitActive seeds the last-synced set from the current DB state at startup.
func InitActive(db *gorm.DB) {
	syncMu.Lock()
	lastSynced = ActiveUserIDs(db)
	lastSyncedSet = true
	syncMu.Unlock()
}

// LastSyncedSnapshot returns a copy of the last-synced set and whether it is set.
func LastSyncedSnapshot() (map[int]bool, bool) {
	syncMu.Lock()
	defer syncMu.Unlock()
	if !lastSyncedSet {
		return nil, false
	}
	cp := make(map[int]bool, len(lastSynced))
	for k, v := range lastSynced {
		cp[k] = v
	}
	return cp, true
}

// flushTraffic drains Xray's live counters into used_bytes before a restart
// zeroes them. Uses atomic SQL increments so it cannot lose a concurrent update.
func flushTraffic(db *gorm.DB) {
	traffic, err := Xray.QueryOutboundTraffic(true)
	if err != nil {
		log.Printf("pre-reload traffic flush failed: %v", err)
		return
	}
	if len(traffic) == 0 {
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
}

// SyncXray rebuilds the Xray config from active users and reloads the service.
func SyncXray(db *gorm.DB) {
	if !Cfg.ProvisioningEnabled {
		log.Printf("provisioning disabled; skipping xray sync")
		return
	}
	syncMu.Lock()
	defer syncMu.Unlock()

	flushTraffic(db) // accumulate usage before the restart zeroes counters
	users := ActiveUsers(db)
	ids, relays := GetActiveRelays(db)
	byID := map[int]xray.Relay{}
	for i, id := range ids {
		byID[id] = relays[i]
	}
	if err := Xray.WriteConfig(users, relays, byID, IranDirectEnabled(db)); err != nil {
		log.Printf("failed to write xray config: %v", err)
		return
	}
	Xray.Reload()

	lastSynced = make(map[int]bool, len(users))
	for _, u := range users {
		lastSynced[u.ID] = true
	}
	lastSyncedSet = true
	log.Printf("xray synced with %d active users", len(users))
}

func tagToID(tag string) (int, bool) {
	const p = "user-"
	if len(tag) <= len(p) || tag[:len(p)] != p {
		return 0, false
	}
	n := 0
	for _, c := range tag[len(p):] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
