package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

func makeDB(t *testing.T, path, username string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT)")
	db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)")
	db.Exec("INSERT INTO users (username) VALUES (?)", username)
	if sqlDB, e := db.DB(); e == nil {
		sqlDB.Close()
	}
}

func TestValidateSQLite(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.db")
	makeDB(t, good, "alice_01")
	if err := ValidateSQLite(good); err != nil {
		t.Fatalf("valid backup rejected: %v", err)
	}

	// hostile username (path traversal) must be rejected before promotion
	bad := filepath.Join(dir, "bad.db")
	makeDB(t, bad, "../../../../etc/cron.d/pwn")
	if err := ValidateSQLite(bad); err == nil {
		t.Fatal("backup with traversal username was accepted")
	}

	// newline-injection username must be rejected
	nl := filepath.Join(dir, "nl.db")
	makeDB(t, nl, "bob\nroot * hax 10.10.0.9")
	if err := ValidateSQLite(nl); err == nil {
		t.Fatal("backup with newline username was accepted")
	}

	// non-sqlite junk must be rejected
	junk := filepath.Join(dir, "junk.db")
	_ = os.WriteFile(junk, []byte("this is not a sqlite database at all"), 0o644)
	if err := ValidateSQLite(junk); err == nil {
		t.Fatal("non-sqlite file was accepted")
	}

	// missing panel tables must be rejected
	empty := filepath.Join(dir, "empty.db")
	edb, _ := gorm.Open(sqlite.Open(empty), &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	edb.Exec("CREATE TABLE foo (x INTEGER)")
	if s, e := edb.DB(); e == nil {
		s.Close()
	}
	if err := ValidateSQLite(empty); err == nil {
		t.Fatal("DB without users/settings was accepted")
	}
}
