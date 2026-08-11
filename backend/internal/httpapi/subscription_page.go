package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/models"
	"multivpn/internal/services"
)

func ff1(x float64) string { return strconv.FormatFloat(x, 'f', 1, 64) }

// userEgressGeo geolocates the user's effective egress (chosen or round-robin).
func (s *Server) userEgressGeo(u *models.User) map[string]any {
	rows := services.GetActiveGroup(s.db)
	if len(rows) == 0 {
		return nil
	}
	byID := map[int]*models.Outbound{}
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	var chosen *models.Outbound
	if u.OutboundID != nil {
		chosen = byID[*u.OutboundID]
	}
	if chosen == nil {
		chosen = &rows[u.Index%len(rows)]
	}
	if chosen.EgressIP == "" {
		return nil
	}
	g := s.geo.Lookup(chosen.EgressIP)
	if g == nil {
		return nil
	}
	label := chosen.CountryName
	if label == "" {
		label = chosen.Name
	}
	return map[string]any{
		"city": g.City, "country": g.Country, "flag": g.Flag,
		"lat": g.Lat, "lon": g.Lon, "label": label,
	}
}

func (s *Server) mapConnsHTML(u *models.User) string {
	conns := services.RecentConnections(s.db, u.ID, 30)
	if len(conns) == 0 && !s.geo.Available() {
		return ""
	}
	egress := s.userEgressGeo(u)
	var geoConns []models.Connection
	for _, c := range conns {
		if c.Lat != nil && c.Lon != nil {
			geoConns = append(geoConns, c)
		}
	}

	var beams, markers strings.Builder
	var ex, ey float64
	haveEx := false
	if egress != nil {
		ex, ey = services.ToXY(egress["lat"].(float64), egress["lon"].(float64))
		haveEx = true
	}
	for i, c := range geoConns {
		if i >= 15 {
			break
		}
		x1, y1 := services.ToXY(*c.Lat, *c.Lon)
		if haveEx && i < 10 {
			cx := (x1 + ex) / 2
			cy := min2(y1, ey) - abs2(x1-ex)*0.28 - 24
			d := fmt.Sprintf("M%s,%s Q%s,%s %s,%s", ff1(x1), ff1(y1), ff1(cx), ff1(cy), ff1(ex), ff1(ey))
			dur := 2.4 + float64(i%5)*0.4
			beams.WriteString(fmt.Sprintf(
				`<path class="beam" d="%s"/><circle class="bdot" r="2.6"><animateMotion dur="%ss" repeatCount="indefinite" path="%s"/></circle>`,
				d, ff1(dur), d))
		}
		lx := x1 / services.MapW * 100
		ly := y1 / services.MapH * 100
		place := joinNonEmpty(", ", c.City, c.Country)
		if place == "" {
			place = "نامشخص"
		}
		markers.WriteString(fmt.Sprintf(
			`<button type="button" class="marker" style="left:%s%%;top:%s%%" data-x="%s" data-y="%s" data-ip="%s" data-city="%s" data-flag="%s" data-hits="%d" aria-label="%s"><span class="mk-dot"></span></button>`,
			ff2(lx), ff2(ly), ff1(x1), ff1(y1), esc(c.IP), esc(place), flagURL(c.CountryCode), c.Hits, esc(c.IP)))
	}
	egressDot := ""
	if haveEx {
		egressDot = fmt.Sprintf(`<circle class="edot-glow" cx="%s" cy="%s" r="10"/><circle class="edot" cx="%s" cy="%s" r="4.5"/>`,
			ff1(ex), ff1(ey), ff1(ex), ff1(ey))
	}
	hasMap := s.worldSVGString() != ""
	var svg string
	if hasMap {
		overlay := fmt.Sprintf(`<svg class="wmap-ov" viewBox="0 0 %d %d" preserveAspectRatio="xMidYMid meet" aria-hidden="true">%s%s</svg>`,
			services.MapW, services.MapH, beams.String(), egressDot)
		svg = `<div class="map-inner"><img class="wmap-bg" src="/map/world.svg" alt="نقشه‌ی اتصال‌ها" loading="lazy">` +
			overlay + `<div class="markers">` + markers.String() + `</div></div>`
	} else {
		svg = `<div class="map-empty muted">نقشه‌ی آفلاین در دسترس نیست.</div>`
	}

	var rowsHTML strings.Builder
	for i, c := range conns {
		if i >= 15 {
			break
		}
		place := joinNonEmpty(", ", esc(c.City), esc(c.Country))
		if place == "" {
			place = "نامشخص"
		}
		rowsHTML.WriteString(fmt.Sprintf(
			`<div class="conn-row"><img class="conn-flag" src="%s" alt="" loading="lazy"><span class="conn-ip">%s</span><span class="conn-place">%s</span><span class="conn-hits">%d×</span></div>`,
			flagURL(c.CountryCode), esc(c.IP), place, c.Hits))
	}
	rowsStr := rowsHTML.String()
	if rowsStr == "" {
		rowsStr = `<div class="muted sm" style="padding:10px 0">هنوز اتصالی ثبت نشده.</div>`
	}

	egressBadge := ""
	if egress != nil {
		city, _ := egress["city"].(string)
		label, _ := egress["label"].(string)
		country, _ := egress["country"].(string)
		head := city
		if head == "" {
			head = label
		}
		suffix := ""
		if country != "" {
			suffix = "، " + esc(country)
		}
		egressBadge = fmt.Sprintf(`<div class="map-egress"><img class="conn-flag" src="%s" alt="">خروج از: <b>%s%s</b></div>`,
			esc(egress["flag"].(string)), esc(head), suffix)
	}

	interactive := hasMap && markers.Len() > 0
	controls, script := "", ""
	if interactive {
		controls = `<button type="button" class="map-reset">⨯ کوچک‌نمایی</button>` +
			`<div class="map-hint">🔍 روی هر نقطه بزنید تا آن منطقه بزرگ شود</div>` +
			`<div class="map-panel"></div>`
		script = `<script src="/map/zoom.js"></script>`
	}
	return `<div class="glass"><div class="map-head"><h3>🛰️ نقشه‌ی اتصال‌ها (زنده)</h3>` + egressBadge + `</div>` +
		`<div class="map-wrap">` + svg + controls + `</div>` +
		`<div class="conn-list">` + rowsStr + `</div></div>` + script
}

func (s *Server) resourcesHTML() string {
	items := services.ResourcesList(s.db, true)
	if len(items) == 0 {
		return ""
	}
	card := func(r *models.Resource) string {
		icon := esc(orDefaultStr(r.Icon, "📦"))
		title := esc(r.Title)
		plat := ""
		if r.Platform != "" {
			plat = `<span class="res-plat">` + esc(r.Platform) + `</span>`
		}
		desc := ""
		if r.Description != "" {
			desc = `<small>` + esc(r.Description) + `</small>`
		}
		inner := `<span class="res-ico">` + icon + `</span><span class="res-body"><b>` + title + plat + `</b>` + desc + `</span>`
		if r.URL != "" {
			return `<a class="res-item" href="` + esc(r.URL) + `" target="_blank" rel="noreferrer noopener">` + inner + `<span class="res-dl">⬇</span></a>`
		}
		return `<div class="res-item">` + inner + `</div>`
	}
	var downloads, guides strings.Builder
	for i := range items {
		if items[i].Kind == "download" {
			downloads.WriteString(card(&items[i]))
		} else if items[i].Kind == "guide" {
			guides.WriteString(card(&items[i]))
		}
	}
	out := `<div class="glass"><h3>📚 آموزش و دانلود نرم‌افزار</h3>`
	if downloads.Len() > 0 {
		out += `<div class="res-grid">` + downloads.String() + `</div>`
	}
	if guides.Len() > 0 {
		out += `<div class="res-guides">` + guides.String() + `</div>`
	}
	return out + `</div>`
}

func (s *Server) subscriptionPage(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	// Browser-facing: a bad/guessed token gets the friendly wrong-address page
	// (and feeds the tarpit) instead of a raw JSON 404.
	token := chi.URLParam(r, "token")
	var uModel models.User
	if len(token) < 20 || s.db.Where("sub_token = ?", token).First(&uModel).Error != nil {
		s.notFound(w, r)
		return
	}
	u := &uModel
	services.RecordConnection(s.db, u.ID, s.clientIP(r))
	st := services.SettingsGetAll(s.db)
	d := payloadFor(u)

	title := esc(orDefaultStr(st["panel_title"], "MultiVPN"))
	total := "نامحدود"
	if d.QuotaBytes > 0 {
		total = fmtBytes(d.QuotaBytes)
	}
	used := fmtBytes(d.UsedBytes)
	stLabel, stColor := statusPill(d.Status)
	expTxt := "بدون انقضا"
	if d.DaysLeft != nil {
		expTxt = itoa(*d.DaysLeft) + " روز"
	}
	announcement := esc(st["announcement"])
	remaining := int64(0)
	if d.RemainingBytes != nil {
		remaining = *d.RemainingBytes
	}
	ringSVG, ringMain, ringSub := ring(d.QuotaBytes, remaining)

	base := "/sub/" + chi.URLParam(r, "token") + "/config"
	avail := services.ConfigAvailability(u)

	dl := func(proto, label, subLabel string, available bool) string {
		if available {
			return `<a class="dl" href="` + base + `/` + proto + `" download><span class="dl-i">⬇</span><span><b>` +
				label + `</b><small>` + subLabel + `</small></span></a>`
		}
		return `<span class="dl off"><span class="dl-i">–</span><span><b>` + label + `</b><small>نامشخص</small></span></span>`
	}

	annHTML := ""
	if announcement != "" {
		annHTML = `<div class="ann">📢 ` + announcement + `</div>`
	}
	// Credentials card (username/password) — the same login used by OpenVPN and
	// L2TP/IPsec. Shown in place of the WireGuard QR. `user-select:all` lets a
	// single tap select the whole value for copying (CSP forbids inline JS).
	credsHTML := ""
	if u.L2tpPassword != "" {
		credsHTML = `<div class="creds"><div class="creds-t">🔑 یوزرنیم و رمز — OpenVPN / L2TP</div>` +
			`<div class="cred-row"><span class="cred-k">یوزرنیم</span><span class="cred-v" dir="ltr">` + esc(u.Username) + `</span></div>` +
			`<div class="cred-row pw"><span class="cred-k">رمز عبور</span><span class="cred-v" dir="ltr">` + esc(u.L2tpPassword) + `</span></div>` +
			`<div class="cred-hint">هنگام اتصال OpenVPN یا L2TP همین یوزرنیم و رمز را وارد کنید · برای کپی، روی مقدار لمس کنید</div></div>`
	}

	// egress location picker (form-based, no JS -> CSP-safe)
	loc := s.locations(u)
	locHTML := ""
	locs := loc["locations"].([]map[string]any)
	if len(locs) > 0 {
		token := esc(chi.URLParam(r, "token"))
		selected, _ := loc["selected"].(*int)
		locBtn := func(val string, active bool, flagHTML, ttl, subtitle string) string {
			cls := "loc"
			chk := ""
			if active {
				cls = "loc active"
				chk = `<span class="chk">✓</span>`
			}
			return `<button type="submit" name="outbound_id" value="` + val + `" class="` + cls + `">` +
				flagHTML + `<span><b>` + esc(ttl) + `</b><small>` + esc(subtitle) + `</small></span>` + chk + `</button>`
		}
		var btns strings.Builder
		btns.WriteString(locBtn("auto", selected == nil,
			`<span class="loc-flag auto">🎯</span>`, "خودکار",
			fmt.Sprintf("بالانسر بین %d لوکیشن", loc["pool_size"])))
		for _, L := range locs {
			id := L["id"].(int)
			active := selected != nil && *selected == id
			flag := `<img class="loc-flag" src="` + esc(L["flag"].(string)) + `" alt="" loading="lazy">`
			ttl := orDefaultStr(L["country_name"].(string), L["name"].(string))
			subtitle := L["name"].(string)
			if L["country_name"].(string) == "" {
				subtitle = orDefaultStr(L["egress_ip"].(string), L["address"].(string))
			}
			btns.WriteString(locBtn(itoa(id), active, flag, ttl, subtitle))
		}
		locHTML = `<div class="glass"><h3>🌍 لوکیشن خروج (هر سه پروتکل)</h3>` +
			`<form method="post" action="/sub/` + token + `/location" class="locs">` + btns.String() + `</form>` +
			`<div class="loc-hint">با انتخاب لوکیشن، ترافیک OpenVPN/WireGuard/L2TP از همان‌جا خارج می‌شود (اعمالِ تغییر چند ثانیه طول می‌کشد).</div></div>`
	}

	mapHTML := safeHTML(func() string { return s.mapConnsHTML(u) }, "map render")
	resHTML := safeHTML(func() string { return s.resourcesHTML() }, "resources render")

	// Security card: IP limit + download-speed cap + alerts (shown when any of
	// them applies).
	alertsHTML := ""
	alerts := services.RecentAlerts(s.db, u.ID, 12)
	effLimit := services.EffectiveIPLimit(s.db, u)
	effBw := services.EffectiveBandwidth(s.db, u)
	if effLimit > 0 || effBw > 0 || len(alerts) > 0 {
		var rows strings.Builder
		if len(alerts) == 0 {
			rows.WriteString(`<div class="al-empty">✓ تاکنون هشداری ثبت نشده است</div>`)
		}
		for _, a := range alerts {
			cls, ico := "al-warn", "⚠️"
			if a.Kind == "blocked" {
				cls, ico = "al-block", "⛔"
			}
			ips := ""
			if a.IPs != "" {
				ips = `<div class="al-ips" dir="ltr">` + esc(a.IPs) + `</div>`
			}
			rows.WriteString(`<div class="al-row ` + cls + `"><span class="al-ico">` + ico + `</span>` +
				`<div class="al-body"><div class="al-msg">` + esc(a.Message) + `</div>` + ips +
				`<div class="al-time" dir="ltr">` + esc(a.CreatedAt.Local().Format("2006-01-02 15:04")) + `</div></div></div>`)
		}
		limTxt := "نامحدود"
		if effLimit > 0 {
			limTxt = itoa(effLimit) + " دستگاه/IP"
		}
		blocked := ""
		if !u.Enabled && u.Strikes >= 3 {
			blocked = `<div class="al-blocked">⛔ این اکانت به‌دلیل ۳ بار تخطی از حد IP مسدود شده است. برای رفع مسدودی با پشتیبانی تماس بگیرید.</div>`
		}

		// Download-speed cap: numeric + an animated "data flow" meter (light beams
		// stream across a track; the stream runs faster the higher the cap).
		bwHTML := `<div class="bw-row"><div class="bw-info"><span class="bw-ico">⚡</span>` +
			`<div><div class="bw-label">محدودیت سرعت دانلود</div><div class="bw-sub">سقفِ این اکانت</div></div></div>`
		if effBw > 0 {
			dur := 2300 - effBw*12
			if dur < 650 {
				dur = 650
			}
			if dur > 2400 {
				dur = 2400
			}
			bwHTML += `<div class="bw-num"><b>` + itoa(effBw) + `</b><small>Mbps</small></div></div>` +
				fmt.Sprintf(`<div class="bw-track" style="--bwdur:%dms"><span class="bw-beam"></span><span class="bw-beam b2"></span><span class="bw-beam b3"></span><span class="bw-beam b4"></span></div>`, dur)
		} else {
			bwHTML += `<div class="bw-num unlim">♾️ نامحدود</div></div>` +
				`<div class="bw-track unlim"><span class="bw-beam"></span><span class="bw-beam b2"></span><span class="bw-beam b3"></span></div>`
		}

		alertsHTML = `<div class="glass"><h3>🛡️ امنیت و محدودیت</h3>` +
			`<div class="al-head"><span>حد مجاز هم‌زمان: <b>` + limTxt + `</b></span>` +
			`<span class="al-strikes">هشدارها: <b>` + itoa(u.Strikes) + `/3</b></span></div>` +
			`<div class="bw-box">` + bwHTML + `</div>` +
			blocked + `<div class="al-list">` + rows.String() + `</div></div>`
	}

	avatarCh := "?"
	if uname := strings.TrimSpace(u.Username); uname != "" {
		avatarCh = esc(strings.ToUpper(string([]rune(uname)[0])))
	}
	ubarHTML := ""
	if d.QuotaBytes > 0 {
		pct := d.UsedPct
		ubarHTML = fmt.Sprintf(`<div class="ubar"><i style="width:%s%%"></i></div><div class="ubar-tx"><span>%s٪ مصرف‌شده</span><span>%s / %s</span></div>`,
			ff2(minf(100, pct)), fmtPct(pct), used, total)
	} else {
		ubarHTML = `<div class="ubar-tx" style="justify-content:center"><span>♾️ حجم نامحدود</span></div>`
	}

	downloadsHTML := `<div class="glass"><h3>⬇️ دانلود کانفیگ‌ها</h3><div class="dls">` +
		dl("wireguard", "WireGuard", "سریع‌ترین، مناسب موبایل", avail["wireguard"]) +
		dl("openvpn", "OpenVPN", "سازگاری بالا", avail["openvpn"]) +
		dl("l2tp", "L2TP/IPsec", "بدون نصب اپ", avail["l2tp"]) +
		`</div>` + credsHTML + `</div>`

	css := strings.ReplaceAll(subCSS, "__STC__", stColor)
	var p strings.Builder
	p.WriteString(`<!doctype html>` + "\n")
	p.WriteString(`<html lang="fa" dir="rtl"><head>` + "\n")
	p.WriteString(`<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	p.WriteString(`<meta http-equiv="refresh" content="120">` + "\n")
	p.WriteString(`<title>` + title + ` — ` + esc(u.Username) + `</title>` + "\n")
	p.WriteString(`<style>` + "\n" + css + "\n</style></head>\n")
	heroHTML := `<section class="glass hero">` +
		`<div class="hero-ring"><div class="ring">` + ringSVG + `<div class="ring-label"><div class="m">` + ringMain + `</div><div class="s">` + ringSub + `</div></div></div></div>` +
		`<div class="hero-info"><div class="hero-user"><span class="avatar">` + avatarCh + `</span>` +
		`<div><div class="uname">` + esc(u.Username) + `</div><div class="uname-sub">حساب اشتراک شما</div></div></div>` +
		`<div class="tiles">` +
		`<div class="tile"><div class="k">📊 مصرف‌شده</div><div class="v">` + used + `</div></div>` +
		`<div class="tile"><div class="k">📦 کل حجم</div><div class="v">` + total + `</div></div>` +
		`<div class="tile"><div class="k">⏳ اعتبار</div><div class="v">` + expTxt + `</div></div></div>` +
		ubarHTML + `</div></section>`

	p.WriteString(`<body><div class="wrap">` + "\n")
	p.WriteString(`<header class="topbar"><div class="brand-row"><span class="brand-mark">🛡️</span><span class="brand">` + title + `</span></div>` +
		`<span class="status-pill">` + stLabel + `</span></header>` + "\n")
	p.WriteString(annHTML)
	// Horizontal, single-viewport dashboard: three columns fill the height; the
	// page itself never scrolls (columns scroll internally only if they overflow).
	p.WriteString(`<main class="dash">` + "\n")
	p.WriteString(`<div class="dcol">` + heroHTML + alertsHTML + `</div>` + "\n")
	p.WriteString(`<div class="dcol">` + mapHTML + locHTML + `</div>` + "\n")
	p.WriteString(`<div class="dcol">` + downloadsHTML + resHTML + `</div>` + "\n")
	p.WriteString(`</main>` + "\n")
	p.WriteString(`<footer class="foot">🔄 به‌روزرسانی خودکار هر ۲ دقیقه · 🔒 این لینک را محرمانه نگه دارید</footer>`)
	p.WriteString(`</div></body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(p.String()))
}

func statusPill(status string) (string, string) {
	switch status {
	case "active":
		return "فعال", "#34d399"
	case "disabled":
		return "غیرفعال", "#949db6"
	case "expired":
		return "منقضی", "#fbbf24"
	case "limited":
		return "اتمام حجم", "#f87171"
	default:
		return status, "#949db6"
	}
}

// safeHTML runs fn, returning "" (and logging) on panic so an optional card
// never turns the whole page into a 500.
func safeHTML(fn func() string, what string) (out string) {
	defer func() {
		if rec := recover(); rec != nil {
			out = ""
		}
	}()
	return fn()
}

func fmtPct(p float64) string {
	if p == float64(int64(p)) {
		return strconv.FormatInt(int64(p), 10)
	}
	return strconv.FormatFloat(p, 'f', 1, 64)
}

func ff2(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func min2(a, b float64) float64 { return minf(a, b) }
func abs2(a float64) float64 {
	if a < 0 {
		return -a
	}
	return a
}

func orDefaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
