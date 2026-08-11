// Command multivpn is the panel API server. With the "seed" argument it creates
// the initial admin instead of serving (replaces the former `python -m app.seed`).
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"multivpn/internal/config"
	"multivpn/internal/httpapi"
	"multivpn/internal/models"
	"multivpn/internal/security"
	"multivpn/internal/services"
	"multivpn/internal/store"
	"multivpn/internal/xray"

	"gorm.io/gorm"
)

func main() {
	log.SetFlags(log.LstdFlags)

	if len(os.Args) > 1 && os.Args[1] == "version" {
		println("multivpn panel " + version)
		return
	}

	baseDir := os.Getenv("BASE_DIR")
	if baseDir == "" {
		baseDir = "."
	}
	cfg, err := config.Load(baseDir)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	models.Init(cfg.OvpnSubnet, cfg.WgSubnet, cfg.L2tpSubnet)

	// A restore uploaded before this boot is applied here, while no DB handle is
	// open, so the swap is safe; the system-state rebuild runs after serving.
	restored := false
	if p := cfg.SQLitePath(); p != "" {
		restored = store.ApplyStagedRestore(p)
		if restored {
			log.Printf("restore: staged backup applied to %s", p)
		}
	}

	db, err := store.Open(cfg)
	if err != nil && restored {
		// the just-restored DB failed to open -> roll back to the pre-restore copy
		if p := cfg.SQLitePath(); p != "" && store.RollbackRestore(p) {
			log.Printf("restore: opening restored DB failed (%v); rolled back to pre-restore", err)
			restored = false
			db, err = store.Open(cfg)
		}
	}
	if err != nil {
		log.Fatalf("database error: %v", err)
	}

	geo := services.NewGeoIP(cfg.AssetsDir)
	services.Init(cfg, geo, xray.New(cfg))

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "seed":
			seedAdmin(cfg, db)
			return
		case "passwd":
			var newpw string
			if len(os.Args) > 2 {
				newpw = os.Args[2]
			}
			passwdAdmin(cfg, db, newpw)
			return
		case "optimize":
			apply := len(os.Args) > 2 && (os.Args[2] == "--apply" || os.Args[2] == "apply")
			runOptimize(db, apply)
			return
		}
	}

	serve(cfg, db, geo, restored)
}

const version = "1.0.0-go"

// runOptimize is the `multivpn optimize [--apply]` tool: it tunes the host
// network for speed (BBR/FQ/fast-open/buffers), benchmarks each relay's real
// download throughput through a temporary Xray, prints a fastest-first ranking,
// and (with --apply) activates only the fastest relays so clients egress via
// them. It re-syncs Xray so the per-connection speed sockopt takes effect.
func runOptimize(db *gorm.DB, apply bool) {
	fmt.Println("──────────────────────────────────────────────")
	fmt.Println(" MultiVPN — بهینه‌سازیِ شبکهٔ اوتباند")
	fmt.Println("──────────────────────────────────────────────")
	if ok, msg := services.TuneNetwork(); ok {
		fmt.Printf(" ✓ تیونِ سیستم اعمال شد  (%s)\n", msg)
	} else {
		fmt.Printf(" ! تیونِ سیستم: %s\n", msg)
	}
	fmt.Println(" ⏱  در حال بنچمارکِ رله‌ها (هر رله ~۲۵ ثانیه)…")
	res := services.OptimizeOutbounds(db, apply)
	if len(res) == 0 {
		fmt.Println(" هیچ اوتباندی برای تست وجود ندارد.")
		return
	}
	fmt.Printf("\n %-3s %-18s %-24s %9s %7s  %s\n", "#", "name", "address", "Mbps", "ms", "egress")
	fmt.Println(" " + strings.Repeat("─", 74))
	for i, r := range res {
		if r.OK {
			fmt.Printf(" %-3d %-18s %-24s %9.1f %7d  %s\n", i+1, trunc(r.Name, 18), trunc(r.Address, 24), r.Mbps, r.LatencyMs, r.EgressIP)
		} else {
			fmt.Printf(" %-3d %-18s %-24s   ✗ %s\n", i+1, trunc(r.Name, 18), trunc(r.Address, 24), r.Error)
		}
	}
	fmt.Println()
	if apply {
		fmt.Println(" ✓ سریع‌ترین رله‌ها فعال و کانفیگ همگام شد.")
	} else {
		services.SyncXray(db) // apply the new speed sockopt even without reselecting
		fmt.Println(" ✓ تیونِ سرعت روی کانفیگ اعمال شد. برای فعال‌سازیِ خودکارِ سریع‌ترین رله‌ها:")
		fmt.Println("     multivpn optimize --apply")
	}
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func serve(cfg *config.Config, db *gorm.DB, geo *services.GeoIP, restored bool) {
	// startup sync (best-effort; a failure must not stop boot)
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("startup sync failed: %v", r)
			}
		}()
		services.BackfillTokens(db)
		services.SeedResources(db)
		services.SyncXray(db)
		services.InitActive(db)
		services.SyncBandwidth(db)
		services.AuditLog(db, services.LogSystem, "info", "system", "سرویس پنل راه‌اندازی شد")
	}()

	if restored {
		// Rebuild derivable system state from the restored DB in the background so
		// a large restore cannot delay the panel coming up (best-effort anyway).
		go func() {
			defer func() { _ = recover() }()
			services.ReconcileProvisioning(db)
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	go services.TrafficLoop(ctx, db)

	auth := security.New(cfg.SecretKey, cfg.AccessTokenExpireMinutes)
	srv := httpapi.New(cfg, db, auth, geo)

	addr := cfg.APIHost + ":" + strconv.Itoa(cfg.APIPort)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		<-sigc
		log.Printf("shutting down...")
		cancel()
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	cert := os.Getenv("PANEL_TLS_CERT")
	key := os.Getenv("PANEL_TLS_KEY")
	log.Printf("MultiVPN panel listening on %s", addr)
	if cert != "" && key != "" {
		err := httpServer.ListenAndServeTLS(cert, key)
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	} else {
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}
}

// seedAdmin creates the initial admin, generating a strong password if the
// configured one is weak/empty (and printing it once).
func seedAdmin(cfg *config.Config, db *gorm.DB) {
	var existing models.Admin
	if err := db.Where("username = ?", cfg.AdminUsername).First(&existing).Error; err == nil {
		log.Printf("Admin %q already exists.", cfg.AdminUsername)
		return
	}
	password := cfg.AdminPassword
	generated := false
	if len(password) < 8 || config.WeakPasswords[strings.ToLower(password)] {
		password = randomPassword()
		generated = true
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		log.Fatalf("hash error: %v", err)
	}
	if err := db.Create(&models.Admin{
		Username:       cfg.AdminUsername,
		HashedPassword: hash,
		TokenVersion:   1,
	}).Error; err != nil {
		log.Fatalf("create admin failed: %v", err)
	}
	log.Printf("Created admin %q.", cfg.AdminUsername)
	if generated {
		println(strings.Repeat("=", 56))
		println("  ADMIN PASSWORD (save it now): " + password)
		println(strings.Repeat("=", 56))
	}
}

// passwdAdmin resets (or creates) the admin password, bumping token_version so
// existing sessions are revoked. Prints the password when it was generated.
func passwdAdmin(cfg *config.Config, db *gorm.DB, newpw string) {
	generated := false
	if len(newpw) < 8 {
		newpw = randomPassword()
		generated = true
	}
	hash, err := security.HashPassword(newpw)
	if err != nil {
		log.Fatalf("hash error: %v", err)
	}
	var admin models.Admin
	if err := db.Where("username = ?", cfg.AdminUsername).First(&admin).Error; err != nil {
		if err := db.Create(&models.Admin{Username: cfg.AdminUsername, HashedPassword: hash, TokenVersion: 1}).Error; err != nil {
			log.Fatalf("create admin failed: %v", err)
		}
	} else {
		admin.HashedPassword = hash
		if admin.TokenVersion < 1 {
			admin.TokenVersion = 1
		}
		admin.TokenVersion++
		db.Save(&admin)
	}
	log.Printf("password updated for admin %q", cfg.AdminUsername)
	if generated {
		println(strings.Repeat("=", 56))
		println("  NEW ADMIN PASSWORD: " + newpw)
		println(strings.Repeat("=", 56))
	}
}

func randomPassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
