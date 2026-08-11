package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/models"
	"multivpn/internal/services"
)

func (s *Server) mountOutbounds(r chi.Router) {
	r.Route("/api/outbounds", func(r chi.Router) {
		r.Get("/", s.listOutbounds)
		r.Post("/", s.addOutbound)
		r.Post("/parse", s.parseOutbound)
		r.Post("/activate-all", s.activateAllOutbounds)
		r.Post("/direct", s.useDirect)
		r.Get("/iran-direct", s.getIranDirect)
		r.Post("/iran-direct", s.setIranDirect)
		r.Patch("/{id}", s.updateOutbound)
		r.Post("/{id}/test", s.testOutbound)
		r.Post("/{id}/activate", s.activateOutbound)
		r.Delete("/{id}", s.deleteOutbound)
	})
}

func (s *Server) getIranDirect(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"enabled": services.IranDirectEnabled(s.db)})
}

// setIranDirect toggles Iran split-routing (.ir + geoip:ir -> direct) and re-syncs Xray.
func (s *Server) setIranDirect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	val := "off"
	if body.Enabled {
		val = "on"
	}
	services.SetSetting(s.db, "iran_direct", val)
	s.safeSyncOB()
	state := "خاموش"
	if body.Enabled {
		state = "روشن"
	}
	services.AuditLog(s.db, services.LogSystem, "info", "ادمین", "مسیریابیِ مستقیمِ ایران "+state+" شد")
	writeJSON(w, http.StatusOK, map[string]any{"enabled": body.Enabled})
}

func (s *Server) safeSyncOB() {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("xray sync failed after outbound change: %v", rec)
		}
	}()
	services.SyncXray(s.db)
}

func outboundOut(row *models.Outbound) map[string]any {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(row.ConfigJSON), &cfg); err != nil {
		cfg = map[string]any{}
	}
	cc := lowerASCII(row.CountryCode)
	pretty, _ := json.MarshalIndent(cfg, "", "  ")
	return map[string]any{
		"id":           row.ID,
		"name":         row.Name,
		"protocol":     row.Protocol,
		"address":      row.Address,
		"is_active":    row.IsActive,
		"egress_ip":    row.EgressIP,
		"country_code": cc,
		"country_name": row.CountryName,
		"flag":         flagURLASCII(cc),
		"config":       string(pretty),
	}
}

func (s *Server) listOutbounds(w http.ResponseWriter, r *http.Request) {
	rows := services.OutboundsList(s.db)
	activeCount := 0
	nameCounts := map[string]int{}
	for i := range rows {
		if rows[i].IsActive {
			activeCount++
		}
		nameCounts[rows[i].Name]++
	}
	balanced := activeCount >= 2
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		d := outboundOut(&rows[i])
		d["name_count"] = nameCounts[rows[i].Name]
		d["balanced"] = rows[i].IsActive && balanced
		if rows[i].IsActive {
			d["group_size"] = activeCount
		} else {
			d["group_size"] = 0
		}
		items = append(items, d)
	}
	strategy := ""
	if balanced {
		strategy = "round-robin"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"direct":     activeCount == 0,
		"balanced":   balanced,
		"group_size": activeCount,
		"group_name": services.ActiveGroupName(s.db),
		"strategy":   strategy,
	})
}

func (s *Server) parseOutbound(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config string `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ob, err := services.ParseOutbound(body.Config)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	proto, _ := ob["protocol"].(string)
	pretty, _ := json.MarshalIndent(ob, "", "  ")
	writeJSON(w, http.StatusOK, map[string]any{"protocol": proto, "config": string(pretty)})
}

func (s *Server) addOutbound(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Config string `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := services.OutboundCreate(s.db, body.Name, body.Config)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if row.IsActive {
		s.safeSyncOB()
	}
	writeJSON(w, http.StatusCreated, outboundOut(row))
}

func (s *Server) updateOutbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	var body struct {
		Name   *string `json:"name"`
		Config *string `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, changed, err := services.OutboundUpdate(s.db, id, body.Name, body.Config)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if row == nil {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	if row.IsActive && changed {
		s.safeSyncOB()
	}
	writeJSON(w, http.StatusOK, outboundOut(row))
}

func (s *Server) testOutbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	var row models.Outbound
	if err := s.db.First(&row, id).Error; err != nil {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	var ob map[string]any
	if err := json.Unmarshal([]byte(row.ConfigJSON), &ob); err != nil {
		httpError(w, http.StatusBadRequest, "Outbound config is invalid JSON")
		return
	}
	result := services.TestOutbound(ob)
	if proxy, okp := result["proxy"].(map[string]any); okp {
		if egress, _ := proxy["egress_ip"].(string); egress != "" {
			row.EgressIP = clipStr(egress, 64)
			if cc, _ := proxy["country_code"].(string); cc != "" {
				row.CountryCode = clipStr(cc, 4)
				cn, _ := proxy["country_name"].(string)
				row.CountryName = clipStr(cn, 64)
			}
			s.db.Save(&row)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) activateOutbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	row := services.OutboundActivate(s.db, id)
	if row == nil {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	s.safeSyncOB()
	writeJSON(w, http.StatusOK, outboundOut(row))
}

func (s *Server) activateAllOutbounds(w http.ResponseWriter, r *http.Request) {
	n := services.OutboundActivateAll(s.db)
	s.safeSyncOB()
	writeJSON(w, http.StatusOK, map[string]any{"activated": n, "balanced": n >= 2})
}

func (s *Server) useDirect(w http.ResponseWriter, r *http.Request) {
	services.OutboundSetDirect(s.db)
	s.safeSyncOB()
	writeJSON(w, http.StatusOK, map[string]any{"direct": true})
}

func (s *Server) deleteOutbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	var row models.Outbound
	wasActive := false
	if err := s.db.First(&row, id).Error; err == nil {
		wasActive = row.IsActive
	}
	if !services.OutboundDelete(s.db, id) {
		httpError(w, http.StatusNotFound, "Outbound not found")
		return
	}
	if wasActive {
		s.safeSyncOB()
	}
	w.WriteHeader(http.StatusNoContent)
}

// small local helpers (avoid importing services' unexported ones)
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func flagURLASCII(cc string) string {
	if len(cc) == 2 {
		alpha := true
		for _, c := range cc {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				alpha = false
				break
			}
		}
		if alpha {
			return "/flags/" + cc + ".svg"
		}
	}
	return "/flags/xx.svg"
}

func clipStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
