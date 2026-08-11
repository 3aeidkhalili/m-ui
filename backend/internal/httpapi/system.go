package httpapi

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/models"
	"multivpn/internal/services"
)

func (s *Server) mountSystem(r chi.Router) {
	r.Get("/api/system/status", s.systemStatus)
	r.Get("/api/system/protocols", s.systemProtocols)
	r.Get("/api/system/resources", s.systemResources)
}

func (s *Server) systemResources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, services.CollectResourceStats())
}

func (s *Server) serviceNames() []string {
	return []string{
		"xray",
		"openvpn@server",
		"wg-quick@" + s.cfg.WgInterface,
		"xl2tpd",
		"strongswan-starter",
	}
}

// svcStatuses queries all units in ONE `systemctl is-active a b c …` call
// (one line per unit, in order) instead of one subprocess per service.
func svcStatuses(names []string) map[string]string {
	out := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// is-active exits non-zero when any unit is inactive but still prints one
	// line per unit on stdout, so parse the output regardless of exit code.
	raw, _ := exec.CommandContext(ctx, "systemctl", append([]string{"is-active"}, names...)...).Output()
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, name := range names {
		st := "unknown"
		if i < len(lines) {
			if v := strings.TrimSpace(lines[i]); v != "" {
				st = v
			}
		}
		out[name] = st
	}
	return out
}

func (s *Server) systemStatus(w http.ResponseWriter, r *http.Request) {
	var users []models.User
	s.db.Find(&users)
	active := 0
	var totalUsed int64
	for i := range users {
		if users[i].IsActive() {
			active++
		}
		totalUsed += users[i].UsedBytes
	}
	services_ := map[string]string{}
	if s.cfg.ProvisioningEnabled {
		services_ = svcStatuses(s.serviceNames())
	} else {
		for _, name := range s.serviceNames() {
			services_[name] = "n/a"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users_total":          len(users),
		"users_active":         active,
		"total_used_bytes":     totalUsed,
		"services":             services_,
		"domain":               s.cfg.PanelDomain,
		"server_ip":            s.cfg.ServerPublicIP,
		"provisioning_enabled": s.cfg.ProvisioningEnabled,
	})
}

func (s *Server) systemProtocols(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, services.CollectProtocolStats(s.db))
}
