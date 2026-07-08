package store

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

// validUsername must match the create-user constraint; a restored DB is an
// unguarded second ingress, and usernames flow into root provisioning scripts
// (ccd file paths, chap-secrets lines) at reconcile — so reject anything with
// path/traversal/control characters here, before the DB is ever promoted.
var validUsername = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,64}$`)

const maxRestoreUsers = 10000

// ValidateSQLite checks that a file is a real SQLite database that looks like a
// MultiVPN panel backup (has the users + settings tables). Used before staging
// an uploaded restore so a bad/hostile file cannot replace the live DB.
func ValidateSQLite(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	hdr := make([]byte, 16)
	_, rerr := io.ReadFull(f, hdr)
	f.Close()
	if rerr != nil {
		return fmt.Errorf("فایل خیلی کوچک/خراب است")
	}
	if string(hdr[:15]) != "SQLite format 3" {
		return fmt.Errorf("فایلِ SQLite معتبر نیست")
	}
	db, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro&immutable=1"),
		&gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	if err != nil {
		return fmt.Errorf("بازکردن دیتابیس ناموفق بود")
	}
	defer func() {
		if sqlDB, e := db.DB(); e == nil {
			sqlDB.Close()
		}
	}()

	// integrity: a header-valid but internally-corrupt DB would brick boot.
	var ic string
	if err := db.Raw("PRAGMA integrity_check").Scan(&ic).Error; err != nil || ic != "ok" {
		return fmt.Errorf("دیتابیسِ بکاپ سالم نیست (integrity_check)")
	}
	m := db.Migrator()
	if !m.HasTable("users") || !m.HasTable("settings") {
		return fmt.Errorf("این فایل بکاپِ این پنل نیست (جدول‌های users/settings یافت نشد)")
	}

	// bound the reconcile blast radius + reject hostile usernames that would
	// escape the provisioning scripts (path traversal / newline injection).
	var count int64
	db.Table("users").Count(&count)
	if count > maxRestoreUsers {
		return fmt.Errorf("تعداد کاربرانِ بکاپ بیش از حد مجاز است (%d)", count)
	}
	var names []string
	db.Table("users").Pluck("username", &names)
	for _, n := range names {
		if !validUsername.MatchString(n) {
			return fmt.Errorf("بکاپ شاملِ نام‌کاربریِ نامعتبر/خطرناک است و رد شد")
		}
	}
	return nil
}

// ApplyStagedRestore, called at boot BEFORE the DB is opened: if a staged
// restore file (<db>.restore) exists, it replaces the live DB (dropping stale
// WAL/SHM) after keeping a one-slot safety copy (<db>.pre-restore). Returns true
// when a restore was applied.
func ApplyStagedRestore(dbPath string) bool {
	staged := dbPath + ".restore"
	if _, err := os.Stat(staged); err != nil {
		return false
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath + ".pre-restore")
	if _, err := os.Stat(dbPath); err == nil {
		_ = os.Rename(dbPath, dbPath+".pre-restore")
	}
	if err := os.Rename(staged, dbPath); err != nil {
		return false
	}
	return true
}

// RollbackRestore restores the pre-restore safety copy over the live DB. Called
// when opening a just-restored DB fails, so a bad backup cannot brick the panel.
func RollbackRestore(dbPath string) bool {
	pre := dbPath + ".pre-restore"
	if _, err := os.Stat(pre); err != nil {
		return false
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath)
	return os.Rename(pre, dbPath) == nil
}
