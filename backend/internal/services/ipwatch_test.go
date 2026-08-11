package services

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"

	"multivpn/internal/models"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Unique, isolated in-memory DB per test (a single kept-open connection), so
	// tests don't share state through a process-wide shared cache.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: glog.Default.LogMode(glog.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { sqlDB.Close() })
	}
	if err := db.AutoMigrate(&models.User{}, &models.Alert{}, &models.LogEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func countAlerts(db *gorm.DB, uid int, kind string) int64 {
	var n int64
	db.Model(&models.Alert{}).Where("user_id = ? AND kind = ?", uid, kind).Count(&n)
	return n
}

func reload(db *gorm.DB, id int) models.User {
	var u models.User
	db.First(&u, id)
	return u
}

func conns(m map[string]*connInfo) map[string]*connInfo { return m }

func ci(sessions int, ips ...string) *connInfo {
	set := map[string]bool{}
	for _, ip := range ips {
		set[ip] = true
	}
	return &connInfo{ips: set, sessions: sessions}
}

func TestEnforceIPLimits_StrikeCooldownBlock(t *testing.T) {
	db := testDB(t)
	u := models.User{Username: "alice", Index: 11, Enabled: true, IPLimit: 1}
	db.Create(&u)

	over := conns(map[string]*connInfo{"alice": ci(2, "1.1.1.1", "2.2.2.2")})
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 1st violation -> strike 1
	enforceIPLimits(db, []models.User{reload(db, u.ID)}, over, t0, 0)
	if got := reload(db, u.ID).Strikes; got != 1 {
		t.Fatalf("after 1st: strikes=%d want 1", got)
	}
	// same-minute repeat is within cooldown -> still 1
	enforceIPLimits(db, []models.User{reload(db, u.ID)}, over, t0.Add(1*time.Minute), 0)
	if got := reload(db, u.ID).Strikes; got != 1 {
		t.Fatalf("within cooldown: strikes=%d want 1", got)
	}
	// after cooldown -> strike 2
	enforceIPLimits(db, []models.User{reload(db, u.ID)}, over, t0.Add(11*time.Minute), 0)
	if got := reload(db, u.ID).Strikes; got != 2 {
		t.Fatalf("after cooldown: strikes=%d want 2", got)
	}
	// 3rd strike -> block
	enforceIPLimits(db, []models.User{reload(db, u.ID)}, over, t0.Add(22*time.Minute), 0)
	final := reload(db, u.ID)
	if final.Strikes != 3 {
		t.Fatalf("3rd: strikes=%d want 3", final.Strikes)
	}
	if final.Enabled {
		t.Fatalf("user should be blocked (enabled=false) after 3 strikes")
	}
	if n := countAlerts(db, u.ID, "ip_limit"); n != 3 {
		t.Fatalf("ip_limit alerts=%d want 3", n)
	}
	if n := countAlerts(db, u.ID, "blocked"); n != 1 {
		t.Fatalf("blocked alerts=%d want 1", n)
	}
}

func TestEnforceIPLimits_NoViolation(t *testing.T) {
	db := testDB(t)
	// unlimited (IPLimit=0) never strikes; within-limit never strikes.
	unlimited := models.User{Username: "bob", Index: 12, Enabled: true, IPLimit: 0}
	within := models.User{Username: "carol", Index: 13, Enabled: true, IPLimit: 2}
	db.Create(&unlimited)
	db.Create(&within)
	src := conns(map[string]*connInfo{
		"bob":   ci(3, "1.1.1.1", "2.2.2.2", "3.3.3.3"),
		"carol": ci(2, "1.1.1.1", "2.2.2.2"), // exactly at limit, not over
	})
	enforceIPLimits(db, []models.User{reload(db, unlimited.ID), reload(db, within.ID)},
		src, time.Now().UTC(), 0)
	if got := reload(db, unlimited.ID).Strikes; got != 0 {
		t.Fatalf("unlimited struck: %d", got)
	}
	if got := reload(db, within.ID).Strikes; got != 0 {
		t.Fatalf("at-limit struck: %d", got)
	}
}

// The effective count is max(sessions, distinct IPs): 3 devices behind one NAT
// (1 IP) still exceeds a limit of 2.
func TestEnforceIPLimits_SessionsExceedEvenWithOneIP(t *testing.T) {
	db := testDB(t)
	u := models.User{Username: "dave", Index: 14, Enabled: true, IPLimit: 2}
	db.Create(&u)
	// 3 concurrent sessions all from the same public IP -> count = max(1,3) = 3 > 2
	enforceIPLimits(db, []models.User{reload(db, u.ID)},
		conns(map[string]*connInfo{"dave": ci(3, "9.9.9.9")}), time.Now().UTC(), 0)
	if got := reload(db, u.ID).Strikes; got != 1 {
		t.Fatalf("sessions>limit should strike: strikes=%d want 1", got)
	}
}

// A user with no per-user limit (IPLimit=0) falls back to the global default.
func TestEnforceIPLimits_GlobalDefaultFallback(t *testing.T) {
	db := testDB(t)
	// erin has no override; global default = 1.
	erin := models.User{Username: "erin", Index: 15, Enabled: true, IPLimit: 0}
	// frank has an explicit override of 5, so the global default must not apply.
	frank := models.User{Username: "frank", Index: 16, Enabled: true, IPLimit: 5}
	db.Create(&erin)
	db.Create(&frank)
	src := conns(map[string]*connInfo{
		"erin":  ci(2, "1.1.1.1", "2.2.2.2"), // 2 > default 1 -> strike
		"frank": ci(2, "1.1.1.1", "2.2.2.2"), // 2 <= override 5 -> ok
	})
	enforceIPLimits(db, []models.User{reload(db, erin.ID), reload(db, frank.ID)},
		src, time.Now().UTC(), 1)
	if got := reload(db, erin.ID).Strikes; got != 1 {
		t.Fatalf("erin (global default 1) should strike: %d", got)
	}
	if got := reload(db, frank.ID).Strikes; got != 0 {
		t.Fatalf("frank (override 5) should not strike: %d", got)
	}
}
