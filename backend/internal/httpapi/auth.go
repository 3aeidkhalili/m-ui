package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"

	"multivpn/internal/config"
	"multivpn/internal/models"
	"multivpn/internal/security"
	"multivpn/internal/services"
)

type ctxKey string

const adminKey ctxKey = "admin"

// requireAdmin validates the bearer token and loads the admin into the context.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unauth := func() {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpError(w, http.StatusUnauthorized, "Invalid or expired token")
		}
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			unauth()
			return
		}
		claims, ok := s.auth.DecodeToken(strings.TrimPrefix(authz, "Bearer "))
		if !ok {
			unauth()
			return
		}
		var admin models.Admin
		if err := s.db.Where("username = ?", claims.Subject).First(&admin).Error; err != nil {
			unauth()
			return
		}
		// version check: a password change bumps token_version, revoking old tokens.
		if claims.Version != admin.TokenVersion {
			unauth()
			return
		}
		ctx := context.WithValue(r.Context(), adminKey, &admin)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func adminFrom(r *http.Request) *models.Admin {
	a, _ := r.Context().Value(adminKey).(*models.Admin)
	return a
}

// POST /api/auth/login
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if locked := s.loginGuard.LockedSeconds(ip); locked > 0 {
		httpError(w, http.StatusTooManyRequests, "Too many failed attempts. Try again in "+itoa(locked)+"s.")
		return
	}
	_ = r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")

	unauthorized := func() {
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpError(w, http.StatusUnauthorized, "Invalid username or password")
	}

	// input bounds: reject over-long inputs before hashing/querying/logging.
	if len(username) > 128 || len(password) > 256 {
		security.DummyVerify()
		s.loginGuard.RecordFail(ip)
		unauthorized()
		return
	}

	var admin models.Admin
	ok := false
	if err := s.db.Where("username = ?", username).First(&admin).Error; err == nil {
		ok = security.VerifyPassword(password, admin.HashedPassword)
	} else {
		security.DummyVerify() // equalise timing -> defeat timing oracle
	}

	if !ok {
		s.loginGuard.RecordFail(ip)
		log.Printf("failed login user=%q ip=%s", username, ip)
		services.AuditLog(s.db, services.LogAuth, "warn", username, "تلاش ناموفق ورود از IP "+ip)
		unauthorized()
		return
	}

	s.loginGuard.Clear(ip)
	log.Printf("login ok user=%s ip=%s", admin.Username, ip)
	services.AuditLog(s.db, services.LogAuth, "info", admin.Username, "ورود موفق ادمین از IP "+ip)
	token, err := s.auth.CreateToken(admin.Username, admin.TokenVersion)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "token error")
		return
	}
	writeJSON(w, http.StatusOK, tokenResp(token))
}

// GET /api/auth/me
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r)
	writeJSON(w, http.StatusOK, map[string]string{"username": admin.Username})
}

// POST /api/auth/change-password
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r)
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !security.VerifyPassword(body.OldPassword, admin.HashedPassword) {
		httpError(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}
	if len(body.NewPassword) < 8 || config.WeakPasswords[strings.ToLower(body.NewPassword)] {
		httpError(w, http.StatusBadRequest, "New password must be at least 8 chars and not a common password")
		return
	}
	hash, err := security.HashPassword(body.NewPassword)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "hash error")
		return
	}
	admin.HashedPassword = hash
	if admin.TokenVersion < 1 {
		admin.TokenVersion = 1
	}
	admin.TokenVersion++
	s.db.Save(admin)
	log.Printf("password changed for admin=%s (token_version=%d)", admin.Username, admin.TokenVersion)
	token, _ := s.auth.CreateToken(admin.Username, admin.TokenVersion)
	writeJSON(w, http.StatusOK, tokenResp(token))
}

func tokenResp(token string) map[string]string {
	return map[string]string{"access_token": token, "token_type": "bearer"}
}
