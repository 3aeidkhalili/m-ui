// Package store opens the database and runs schema migrations.
package store

import (
	"fmt"
	"log"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"

	"multivpn/internal/config"
	"multivpn/internal/models"
)

// allModels is the full set of tables, in dependency-free order.
func allModels() []any {
	return []any{
		&models.Setting{},
		&models.Outbound{},
		&models.Connection{},
		&models.Resource{},
		&models.Admin{},
		&models.User{},
		&models.Alert{},
		&models.LogEvent{},
	}
}

// Open connects to the database, enabling WAL + a busy timeout for SQLite so
// the background traffic writer and request handlers do not deadlock under
// concurrency (fixes the former "database is locked" fragility), then applies
// an additive-only migration.
func Open(cfg *config.Config) (*gorm.DB, error) {
	path := cfg.SQLitePath()
	if path == "" {
		return nil, fmt.Errorf("only sqlite:/// DATABASE_URL is supported, got %q", cfg.DatabaseURL)
	}
	// WAL + 5s busy timeout + foreign keys off, via modernc pragma DSN params.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(0)", path)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: glog.Default.LogMode(glog.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		return nil, err
	}
	log.Printf("database ready: %s", path)
	return db, nil
}

// migrate creates missing tables and adds missing columns/indexes only. It never
// rebuilds or drops an existing table, so it upgrades a database created by the
// former Python backend without the destructive table-rebuild AutoMigrate would
// otherwise attempt over a differing column default.
func migrate(db *gorm.DB) error {
	m := db.Migrator()
	for _, model := range allModels() {
		if !m.HasTable(model) {
			if err := m.CreateTable(model); err != nil {
				return err
			}
			continue
		}
		// Existing table: add only the columns it is missing.
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" || field.IgnoreMigration {
				continue
			}
			if !m.HasColumn(model, field.DBName) {
				if err := m.AddColumn(model, field.Name); err != nil {
					return fmt.Errorf("add column %s.%s: %w", stmt.Schema.Table, field.DBName, err)
				}
				log.Printf("migration: added %s.%s", stmt.Schema.Table, field.DBName)
			}
		}
		// Ensure unique indexes exist (e.g. sub_token on legacy rows).
		for name, idx := range stmt.Schema.ParseIndexes() {
			if idx.Class == "UNIQUE" && !m.HasIndex(model, name) {
				if err := m.CreateIndex(model, name); err != nil {
					// A duplicate-value conflict here is non-fatal; log and continue.
					log.Printf("migration: skip index %s: %v", name, err)
				}
			}
		}
	}
	return nil
}
