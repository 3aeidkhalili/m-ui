// Package httpapi wires the HTTP router, middleware and all endpoint handlers.
package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"multivpn/internal/config"
	"multivpn/internal/ratelimit"
	"multivpn/internal/security"
	"multivpn/internal/services"
)

// Server holds shared dependencies for the HTTP handlers.
type Server struct {
	cfg        *config.Config
	db         *gorm.DB
	auth       *security.Auth
	loginGuard *ratelimit.LoginGuard
	subLimiter *ratelimit.RateLimiter
	locLimiter *ratelimit.RateLimiter
	geo        *services.GeoIP
	nfGuard    *notFoundGuard

	worldOnce sync.Once
	worldSVG  string
}

// New builds a Server.
func New(cfg *config.Config, db *gorm.DB, auth *security.Auth, geo *services.GeoIP) *Server {
	return &Server{
		cfg:        cfg,
		db:         db,
		auth:       auth,
		loginGuard: ratelimit.NewLoginGuard(5, 300*time.Second, 900*time.Second),
		subLimiter: ratelimit.NewRateLimiter(60, 60*time.Second),
		locLimiter: ratelimit.NewRateLimiter(5, 60*time.Second),
		geo:        geo,
		nfGuard:    newNotFoundGuard(),
	}
}

// Handler builds the chi router with all routes and middleware.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.securityHeaders)
	r.Use(s.tarpit) // blocked IPs (repeated wrong addresses) -> 3h countdown page

	// public health
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// auth (public login + authed me/change-password)
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", s.login)
		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Get("/me", s.me)
			r.Post("/change-password", s.changePassword)
		})
	})

	// admin-only API groups
	r.Group(func(r chi.Router) {
		r.Use(s.requireAdmin)
		s.mountUsers(r)
		s.mountSystem(r)
		s.mountSettings(r)
		s.mountProtocols(r)
		s.mountOutbounds(r)
		s.mountResources(r)
		s.mountLogs(r)
		s.mountApiNotes(r)
		s.mountBackup(r)
	})

	// public, token-gated subscription surface
	s.mountSubscription(r)

	// fonts + built frontend (with SPA fallback)
	s.mountStatic(r)

	return r
}

// ---- response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// httpError writes {"detail": msg} to match the former FastAPI error shape the
// frontend expects (api.js reads data.detail).
func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

func decodeJSON(r *http.Request, v any) error {
	// Cap request bodies at 1 MiB so a client cannot force huge allocations via a
	// giant/deeply-nested JSON document (fields typed `any`/`map` accept anything).
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(v)
}

func (s *Server) clientIP(r *http.Request) string {
	return ratelimit.ClientIP(r, s.cfg.TrustedProxy)
}

// ---- security headers middleware ----

var securityHeaderPairs = map[string]string{
	"X-Content-Type-Options":     "nosniff",
	"X-Frame-Options":            "DENY",
	"Referrer-Policy":            "no-referrer",
	"Cross-Origin-Opener-Policy": "same-origin",
	"Permissions-Policy":         "geolocation=(), microphone=(), camera=(), interest-cohort=()",
}

const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'none'; form-action 'self'; object-src 'none'"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		for k, v := range securityHeaderPairs {
			h.Set(k, v)
		}
		h.Set("Content-Security-Policy", csp)

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if s.cfg.TrustedProxy {
			if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
				scheme = xfp
			}
		}
		if scheme == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}

		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/sub/") {
			h.Set("Cache-Control", "no-store")
		}

		// optional CORS (only when explicit origins configured; never wildcard)
		if origins := s.corsOrigins(); len(origins) > 0 {
			if origin := r.Header.Get("Origin"); origin != "" && origins[origin] {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Vary", "Origin")
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", "*")
					h.Set("Access-Control-Allow-Headers", "*")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsOrigins() map[string]bool {
	out := map[string]bool{}
	for _, o := range strings.Split(s.cfg.CORSOrigins, ",") {
		o = strings.TrimSpace(o)
		if o != "" && o != "*" {
			out[o] = true
		}
	}
	if _, ok := out["*"]; ok {
		log.Printf("CORS_ORIGINS='*' is insecure for an admin panel; ignoring")
	}
	return out
}
