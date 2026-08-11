package httpapi

import (
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/skip2/go-qrcode"

	"multivpn/internal/models"
	"multivpn/internal/services"
)

func (s *Server) mountSubscription(r chi.Router) {
	r.Get("/api/sub/{token}", s.subscriptionJSON)
	r.Get("/api/sub/{token}/locations", s.subscriptionLocations)
	r.Post("/api/sub/{token}/location", s.subscriptionSetLocationAPI)
	r.Post("/sub/{token}/location", s.subscriptionSetLocationForm)
	r.Get("/sub/{token}/config/{proto}", s.subscriptionConfig)
	r.Get("/map/world.svg", s.worldMapSVG)
	r.Get("/map/zoom.js", s.mapZoomJS)
	r.Get("/sub/{token}", s.subscriptionPage)
}

// ---- helpers ----

func (s *Server) rateLimited(w http.ResponseWriter, r *http.Request) bool {
	if !s.subLimiter.Allow(s.clientIP(r)) {
		httpError(w, http.StatusTooManyRequests, "Too many requests")
		return true
	}
	return false
}

func (s *Server) userByToken(w http.ResponseWriter, r *http.Request, token string) (*models.User, bool) {
	// A bad token feeds the same tarpit as the HTML path so that token-guessing
	// through the JSON /api/sub surface is throttled and blocked, not just the
	// browser page.
	fail := func() {
		if rem, justBlocked := s.nfGuard.hit(s.clientIP(r), time.Now()); rem > 0 {
			if justBlocked {
				services.AuditLog(s.db, services.LogTarpit, "critical", s.clientIP(r),
					"IP به‌دلیل حدسِ مکررِ توکنِ اشتراک ۳ ساعت مسدود شد")
			}
			s.serveBlockPage(w, rem)
			return
		}
		httpError(w, http.StatusNotFound, "Not found")
	}
	if len(token) < 20 {
		fail()
		return nil, false
	}
	var u models.User
	if err := s.db.Where("sub_token = ?", token).First(&u).Error; err != nil {
		fail()
		return nil, false
	}
	return &u, true
}

func (s *Server) worldSVGString() string {
	s.worldOnce.Do(func() {
		land := strings.Join(s.geo.WorldPaths(), " ")
		if land != "" {
			s.worldSVG = fmt.Sprintf(
				`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">`+
					`<path d="%s" fill="rgba(120,140,200,0.10)" stroke="rgba(148,163,184,0.20)" stroke-width="0.4"/></svg>`,
				services.MapW, services.MapH, land)
		}
	})
	return s.worldSVG
}

func flagURL(cc string) string {
	cc = strings.ToLower(strings.TrimSpace(cc))
	return flagURLASCII(cc)
}

func fmtBytes(n int64) string {
	f := float64(n)
	if f <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", int64(f), units[i])
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}

func daysLeft(u *models.User) *int {
	if u.ExpiresAt == nil {
		return nil
	}
	d := int(time.Until(*u.ExpiresAt).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return &d
}

type subPayload struct {
	Username       string  `json:"username"`
	Status         string  `json:"status"`
	IsActive       bool    `json:"is_active"`
	UsedBytes      int64   `json:"used_bytes"`
	QuotaBytes     int64   `json:"quota_bytes"`
	RemainingBytes *int64  `json:"remaining_bytes"`
	UsedPct        float64 `json:"used_pct"`
	ExpiresAt      *string `json:"expires_at"`
	DaysLeft       *int    `json:"days_left"`
}

func payloadFor(u *models.User) subPayload {
	quota := u.QuotaBytes
	used := u.UsedBytes
	var remaining *int64
	pct := 0.0
	if quota > 0 {
		r := quota - used
		if r < 0 {
			r = 0
		}
		remaining = &r
		pct = math.Round(float64(used)/float64(quota)*1000) / 10
		if pct > 100 {
			pct = 100
		}
	}
	return subPayload{
		Username: u.Username, Status: u.Status(), IsActive: u.IsActive(),
		UsedBytes: used, QuotaBytes: quota, RemainingBytes: remaining,
		UsedPct: pct, ExpiresAt: isoPtrOpt(u.ExpiresAt), DaysLeft: daysLeft(u),
	}
}

// locations returns the active pool + the user's current selection.
func (s *Server) locations(u *models.User) map[string]any {
	rows := services.GetActiveGroup(s.db)
	activeIDs := map[int]bool{}
	for _, r := range rows {
		activeIDs[r.ID] = true
	}
	var selected *int
	if u.OutboundID != nil && activeIDs[*u.OutboundID] {
		selected = u.OutboundID
	}
	locs := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		locs = append(locs, map[string]any{
			"id": r.ID, "name": r.Name,
			"country_code": lowerASCII(r.CountryCode), "country_name": r.CountryName,
			"flag": flagURL(r.CountryCode), "address": r.Address, "egress_ip": r.EgressIP,
		})
	}
	return map[string]any{
		"balancer": len(rows) >= 2, "pool_size": len(rows),
		"selected": selected, "locations": locs,
	}
}

func (s *Server) applyLocation(u *models.User, raw any) error {
	var chosen *int
	switch v := raw.(type) {
	case nil:
		chosen = nil
	case string:
		if v == "" || v == "auto" || v == "0" {
			chosen = nil
		} else {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("Invalid location")
			}
			if !services.ActiveIDs(s.db)[n] {
				return fmt.Errorf("Location not available")
			}
			chosen = &n
		}
	case float64:
		if v == 0 {
			chosen = nil
		} else {
			n := int(v)
			if !services.ActiveIDs(s.db)[n] {
				return fmt.Errorf("Location not available")
			}
			chosen = &n
		}
	}
	u.OutboundID = chosen
	s.db.Save(u)
	locName := "خودکار (بالانسر)"
	if chosen != nil {
		var ob models.Outbound
		if s.db.First(&ob, *chosen).Error == nil {
			locName = ob.Name
			if ob.CountryName != "" {
				locName = ob.CountryName + " · " + ob.Name
			}
		}
	}
	services.AuditLog(s.db, services.LogLocation, "info", u.Username, "تغییر لوکیشن خروج به: "+locName)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("location sync failed (choice saved): %v", rec)
			}
		}()
		services.SyncXray(s.db)
	}()
	return nil
}

// ---- JSON API ----

func (s *Server) subscriptionJSON(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	u, ok := s.userByToken(w, r, chi.URLParam(r, "token"))
	if !ok {
		return
	}
	services.RecordConnection(s.db, u.ID, s.clientIP(r))
	p := payloadFor(u)
	out := map[string]any{
		"username": p.Username, "status": p.Status, "is_active": p.IsActive,
		"used_bytes": p.UsedBytes, "quota_bytes": p.QuotaBytes, "remaining_bytes": p.RemainingBytes,
		"used_pct": p.UsedPct, "expires_at": p.ExpiresAt, "days_left": p.DaysLeft,
		"configs":  services.ConfigAvailability(u),
		"location": s.locations(u),
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) subscriptionLocations(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	u, ok := s.userByToken(w, r, chi.URLParam(r, "token"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.locations(u))
}

func (s *Server) subscriptionSetLocationAPI(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	token := chi.URLParam(r, "token")
	if !s.locLimiter.Allow("loc:" + token) {
		httpError(w, http.StatusTooManyRequests, "Too many location changes; wait a bit.")
		return
	}
	u, ok := s.userByToken(w, r, token)
	if !ok {
		return
	}
	var body struct {
		OutboundID any `json:"outbound_id"`
	}
	_ = decodeJSON(r, &body)
	if err := s.applyLocation(u, body.OutboundID); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.locations(u))
}

func (s *Server) subscriptionSetLocationForm(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	token := chi.URLParam(r, "token")
	if !s.locLimiter.Allow("loc:" + token) {
		httpError(w, http.StatusTooManyRequests, "Too many location changes; wait a bit.")
		return
	}
	u, ok := s.userByToken(w, r, token)
	if !ok {
		return
	}
	_ = r.ParseForm()
	if err := s.applyLocation(u, r.FormValue("outbound_id")); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/sub/"+token, http.StatusSeeOther)
}

func (s *Server) subscriptionConfig(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	u, ok := s.userByToken(w, r, chi.URLParam(r, "token"))
	if !ok {
		return
	}
	p := services.ProtocolsGetAll(s.db)
	var body, fname string
	switch chi.URLParam(r, "proto") {
	case "openvpn":
		body, fname = services.GenOpenVPN(s.db, u, p), u.Username+".ovpn"
	case "wireguard":
		body, fname = services.GenWireGuard(s.db, u, p), u.Username+".conf"
	case "l2tp":
		d := services.GenL2TP(s.db, u, p)
		body = fmt.Sprintf("L2TP/IPsec\nServer: %s\nIPsec PSK: %s\nUsername: %s\nPassword: %s\n",
			d.Server, d.PSK, d.Username, d.Password)
		fname = u.Username + "-l2tp.txt"
	default:
		httpError(w, http.StatusNotFound, "Unknown protocol")
		return
	}
	if body == "" {
		httpError(w, http.StatusNotFound, "Config not available")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fname))
	_, _ = w.Write([]byte(body))
}

func (s *Server) worldMapSVG(w http.ResponseWriter, r *http.Request) {
	svg := s.worldSVGString()
	if svg == "" {
		httpError(w, http.StatusNotFound, "map unavailable")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write([]byte(svg))
}

func (s *Server) mapZoomJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write([]byte(zoomJS))
}

// wgQRSVG renders a WireGuard config as an inline QR SVG (self-hosted, CSP-safe).
func wgQRSVG(data string) string {
	q, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		log.Printf("qr generation failed: %v", err)
		return ""
	}
	q.DisableBorder = true
	bmp := q.Bitmap()
	n := len(bmp)
	border := 2
	total := n + border*2
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, total, total)
	b.WriteString(`<rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="`)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if bmp[y][x] {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+border, y+border)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

// ring returns the progress-ring SVG plus the centre main/sub labels.
func ring(quota, remaining int64) (string, string, string) {
	const rr = 64.0
	const size = 160
	circ := 2 * math.Pi * rr
	var ratio float64
	var main, sub, c1, c2 string
	if quota <= 0 {
		ratio, main, sub, c1, c2 = 1.0, "∞", "نامحدود", "#6366f1", "#22d3ee"
	} else {
		ratio = float64(remaining) / float64(quota)
		if ratio < 0 {
			ratio = 0
		} else if ratio > 1 {
			ratio = 1
		}
		main, sub = fmtBytes(remaining), "حجم باقی‌مانده"
		switch {
		case ratio >= 0.5:
			c1, c2 = "#34d399", "#22d3ee"
		case ratio >= 0.2:
			c1, c2 = "#fbbf24", "#f59e0b"
		default:
			c1, c2 = "#f87171", "#ef4444"
		}
	}
	offset := circ * (1 - ratio)
	svg := `<svg viewBox="0 0 ` + itoa(size) + ` ` + itoa(size) + `">` +
		`<style>@keyframes draw { from { stroke-dashoffset: ` + f2(circ) + `; } to { stroke-dashoffset: ` + f2(offset) + `; } } ` +
		`.prog { animation: draw 1.4s cubic-bezier(.2, .8, .2, 1) forwards; }</style>` +
		`<defs><linearGradient id="rg" x1="0" y1="0" x2="1" y2="1">` +
		`<stop offset="0" stop-color="` + c1 + `"/><stop offset="1" stop-color="` + c2 + `"/>` +
		`</linearGradient></defs>` +
		`<circle class="track" cx="80" cy="80" r="64"/>` +
		`<circle class="prog" cx="80" cy="80" r="64" stroke="url(#rg)" ` +
		`stroke-dasharray="` + f2(circ) + `" stroke-dashoffset="` + f2(offset) + `" transform="rotate(-90 80 80)"/></svg>`
	return svg, main, sub
}

func f2(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func esc(s string) string { return html.EscapeString(s) }
