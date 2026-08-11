package services

import (
	"log"
	"strings"
	"sync/atomic"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// Audit log categories.
const (
	LogAuth       = "auth"
	LogUser       = "user"
	LogLocation   = "location"
	LogConnection = "connection"
	LogIPLimit    = "ip_limit"
	LogTarpit     = "tarpit"
	LogBandwidth  = "bandwidth"
	LogSystem     = "system"
)

var logSeq int64

// AuditLog records a panel event (best-effort; a failure only logs to stderr and
// never blocks the caller). Level is "info" | "warn" | "critical".
// sanitizeLog strips control characters (incl. newlines) from untrusted values
// so a login username or scanned URL path cannot forge log lines or smuggle
// markup into the audit trail.
func sanitizeLog(sfield string, max int) string {
	sfield = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, sfield)
	if len(sfield) > max {
		sfield = sfield[:max]
	}
	return sfield
}

func AuditLog(db *gorm.DB, category, level, actor, message string) {
	if db == nil {
		return
	}
	actor = sanitizeLog(actor, 96)
	message = sanitizeLog(message, 400)
	if err := db.Create(&models.LogEvent{
		Category: category, Level: level, Actor: actor, Message: message,
	}).Error; err != nil {
		log.Printf("audit log: %v", err)
		return
	}
	// Keep the table bounded: every ~200 inserts, trim to the newest 5000 rows.
	if atomic.AddInt64(&logSeq, 1)%200 == 0 {
		var cutoff int
		db.Raw("SELECT id FROM logs ORDER BY id DESC LIMIT 1 OFFSET 5000").Scan(&cutoff)
		if cutoff > 0 {
			db.Where("id <= ?", cutoff).Delete(&models.LogEvent{})
		}
	}
}

// LogsList returns the newest log events, optionally filtered by category.
func LogsList(db *gorm.DB, category string, limit int) []models.LogEvent {
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	q := db.Order("id DESC").Limit(limit)
	if category != "" && category != "all" {
		q = q.Where("category = ?", category)
	}
	var out []models.LogEvent
	q.Find(&out)
	return out
}

// LogsClear deletes all log events.
func LogsClear(db *gorm.DB) {
	db.Where("1 = 1").Delete(&models.LogEvent{})
}
