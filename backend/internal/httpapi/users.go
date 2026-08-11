package httpapi

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/models"
	"multivpn/internal/security"
	"multivpn/internal/services"
)

const gib = int64(1) << 30

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (s *Server) mountUsers(r chi.Router) {
	r.Route("/api/users", func(r chi.Router) {
		r.Get("/", s.listUsers)
		r.Post("/", s.createUser)
		r.Get("/{id}", s.getUser)
		r.Patch("/{id}", s.updateUser)
		r.Post("/{id}/rotate-sub", s.rotateSub)
		r.Post("/{id}/reset", s.resetTraffic)
		r.Delete("/{id}", s.deleteUser)
		r.Get("/{id}/configs", s.getConfigs)
	})
}

func (s *Server) safeSync() {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("xray sync failed (change already persisted): %v", rec)
		}
	}()
	services.SyncXray(s.db)
	services.SyncBandwidth(s.db)
}

func pathInt(r *http.Request, key string) (int, bool) {
	n, err := strconv.Atoi(chi.URLParam(r, key))
	if err != nil {
		return 0, false
	}
	return n, true
}

func (s *Server) getUserOr404(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	id, ok := pathInt(r, "id")
	if !ok {
		httpError(w, http.StatusNotFound, "User not found")
		return nil, false
	}
	var u models.User
	if err := s.db.First(&u, id).Error; err != nil {
		httpError(w, http.StatusNotFound, "User not found")
		return nil, false
	}
	return &u, true
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	var users []models.User
	s.db.Order("id").Find(&users)
	out := make([]userOut, 0, len(users))
	for i := range users {
		out = append(out, toUserOut(&users[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// nextIndex finds the first free host index in the /24, honoring an in-flight
// `taken` set to survive concurrent allocation races.
func (s *Server) nextIndex(taken map[int]bool) (int, error) {
	var idxs []int
	s.db.Model(&models.User{}).Pluck("index", &idxs)
	used := map[int]bool{}
	for _, i := range idxs {
		used[i] = true
	}
	for i := range taken {
		used[i] = true
	}
	i := s.cfg.UserIndexStart
	for used[i] {
		i++
	}
	if i > 254 {
		return 0, errors.New("no free host address in the /24 subnet (max ~244 users)")
	}
	return i, nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string  `json:"username"`
		QuotaGB     float64 `json:"quota_gb"`
		ExpiresDays *int    `json:"expires_days"`
		Note        string  `json:"note"`
		IPLimit     *int    `json:"ip_limit"`
		Bandwidth   *int    `json:"bandwidth_mbps"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// validation (mirrors the former pydantic constraints)
	if len(body.Username) < 1 || len(body.Username) > 64 || !usernameRe.MatchString(body.Username) {
		httpError(w, http.StatusBadRequest, "Invalid username")
		return
	}
	if body.IPLimit != nil && (*body.IPLimit < 0 || *body.IPLimit > 1000) {
		httpError(w, http.StatusBadRequest, "Invalid IP limit")
		return
	}
	if body.Bandwidth != nil && (*body.Bandwidth < 0 || *body.Bandwidth > 100000) {
		httpError(w, http.StatusBadRequest, "Invalid bandwidth")
		return
	}
	if body.QuotaGB < 0 || body.QuotaGB > 1_048_576 {
		httpError(w, http.StatusBadRequest, "Invalid quota")
		return
	}
	if body.ExpiresDays != nil && (*body.ExpiresDays < 0 || *body.ExpiresDays > 36_500) {
		httpError(w, http.StatusBadRequest, "Invalid expiry")
		return
	}
	if len(body.Note) > 255 {
		httpError(w, http.StatusBadRequest, "Note too long")
		return
	}

	var existing models.User
	if err := s.db.Where("username = ?", body.Username).First(&existing).Error; err == nil {
		httpError(w, http.StatusBadRequest, "Username already exists")
		return
	}

	var expiresAt *time.Time
	if body.ExpiresDays != nil && *body.ExpiresDays > 0 {
		t := time.Now().UTC().Add(time.Duration(*body.ExpiresDays) * 24 * time.Hour)
		expiresAt = &t
	}

	tried := map[int]bool{}
	var user *models.User
	for attempt := 0; attempt < 6; attempt++ {
		idx, err := s.nextIndex(tried)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		tried[idx] = true
		ipLimit := 0
		if body.IPLimit != nil {
			ipLimit = *body.IPLimit
		}
		bandwidth := 0
		if body.Bandwidth != nil {
			bandwidth = *body.Bandwidth
		}
		candidate := &models.User{
			Username:      body.Username,
			Index:         idx,
			QuotaBytes:    int64(body.QuotaGB * float64(gib)),
			UsedBytes:     0,
			Enabled:       true,
			ExpiresAt:     expiresAt,
			Note:          body.Note,
			IPLimit:       ipLimit,
			BandwidthMbps: bandwidth,
			L2tpPassword:  security.RandomToken(12),
			SubToken:      security.RandomToken(32),
		}
		if err := s.db.Create(candidate).Error; err != nil {
			// unique conflict on username or index -> maybe retry
			var dup models.User
			if s.db.Where("username = ?", body.Username).First(&dup).Error == nil {
				httpError(w, http.StatusBadRequest, "Username already exists")
				return
			}
			continue
		}
		user = candidate
		break
	}
	if user == nil {
		httpError(w, http.StatusConflict, "Could not allocate a free index; please retry")
		return
	}

	artifacts, err := services.ProvisionUser(user)
	if err != nil {
		s.db.Delete(user) // discard the DB row; ProvisionUser already rolled back system state
		httpError(w, http.StatusInternalServerError, "Provisioning failed: "+err.Error())
		return
	}
	user.OvpnCA = artifacts.Ovpn["ca"]
	user.OvpnCert = artifacts.Ovpn["cert"]
	user.OvpnKey = artifacts.Ovpn["key"]
	user.OvpnTLSCrypt = artifacts.Ovpn["tls_crypt"]
	user.WgPrivateKey = artifacts.Wg["private_key"]
	user.WgPublicKey = artifacts.Wg["public_key"]
	user.WgPresharedKey = artifacts.Wg["preshared_key"]
	s.db.Save(user)

	s.safeSync()
	services.AuditLog(s.db, services.LogUser, "info", user.Username,
		fmt.Sprintf("کاربر جدید ساخته شد (index %d)", user.Index))
	writeJSON(w, http.StatusCreated, toUserOut(user))
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.getUserOr404(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toUserOut(u))
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.getUserOr404(w, r)
	if !ok {
		return
	}
	var body struct {
		QuotaGB     *float64 `json:"quota_gb"`
		ExpiresDays *int     `json:"expires_days"`
		Enabled     *bool    `json:"enabled"`
		Note        *string  `json:"note"`
		IPLimit     *int     `json:"ip_limit"`
		Bandwidth   *int     `json:"bandwidth_mbps"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.QuotaGB != nil {
		if *body.QuotaGB < 0 || *body.QuotaGB > 1_048_576 {
			httpError(w, http.StatusBadRequest, "Invalid quota")
			return
		}
		u.QuotaBytes = int64(*body.QuotaGB * float64(gib))
	}
	if body.ExpiresDays != nil {
		if *body.ExpiresDays < 0 || *body.ExpiresDays > 36_500 {
			httpError(w, http.StatusBadRequest, "Invalid expiry")
			return
		}
		if *body.ExpiresDays > 0 {
			t := time.Now().UTC().Add(time.Duration(*body.ExpiresDays) * 24 * time.Hour)
			u.ExpiresAt = &t
		} else {
			u.ExpiresAt = nil
		}
	}
	if body.IPLimit != nil {
		if *body.IPLimit < 0 || *body.IPLimit > 1000 {
			httpError(w, http.StatusBadRequest, "Invalid IP limit")
			return
		}
		u.IPLimit = *body.IPLimit
	}
	if body.Bandwidth != nil {
		if *body.Bandwidth < 0 || *body.Bandwidth > 100000 {
			httpError(w, http.StatusBadRequest, "Invalid bandwidth")
			return
		}
		u.BandwidthMbps = *body.Bandwidth
	}
	if body.Enabled != nil {
		// Re-enabling a blocked account clears its IP-limit strikes AND its past
		// alerts, so it gets a clean slate (fresh 3-strike budget, no stale
		// warnings on the subscription page).
		if *body.Enabled && !u.Enabled {
			u.Strikes = 0
			s.db.Where("user_id = ?", u.ID).Delete(&models.Alert{})
		}
		u.Enabled = *body.Enabled
	}
	if body.Note != nil {
		if len(*body.Note) > 255 {
			httpError(w, http.StatusBadRequest, "Note too long")
			return
		}
		u.Note = *body.Note
	}
	s.db.Save(u)
	s.safeSync()
	services.AuditLog(s.db, services.LogUser, "info", u.Username, "کاربر ویرایش شد ("+u.Status()+")")
	writeJSON(w, http.StatusOK, toUserOut(u))
}

func (s *Server) rotateSub(w http.ResponseWriter, r *http.Request) {
	u, ok := s.getUserOr404(w, r)
	if !ok {
		return
	}
	u.SubToken = security.RandomToken(32)
	s.db.Save(u)
	writeJSON(w, http.StatusOK, toUserOut(u))
}

func (s *Server) resetTraffic(w http.ResponseWriter, r *http.Request) {
	u, ok := s.getUserOr404(w, r)
	if !ok {
		return
	}
	// Reset also clears the IP-limit strikes and alert history, so the admin has
	// a one-click way to wipe stale warnings (even on an already-active account).
	u.UsedBytes = 0
	u.Strikes = 0
	s.db.Save(u)
	s.db.Where("user_id = ?", u.ID).Delete(&models.Alert{})
	s.safeSync()
	services.AuditLog(s.db, services.LogUser, "info", u.Username, "ریست مصرف و پاک‌سازی هشدارها")
	writeJSON(w, http.StatusOK, toUserOut(u))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.getUserOr404(w, r)
	if !ok {
		return
	}
	uname := u.Username
	services.DeprovisionUser(u)
	s.db.Delete(u)
	s.safeSync()
	services.AuditLog(s.db, services.LogUser, "warn", uname, "کاربر حذف شد")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getConfigs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.getUserOr404(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":  u.Username,
		"openvpn":   services.GenOpenVPN(s.db, u, nil),
		"wireguard": services.GenWireGuard(s.db, u, nil),
		"l2tp":      services.GenL2TP(s.db, u, nil),
	})
}
