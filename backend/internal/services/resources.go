package services

import (
	"strings"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

var allowedKinds = map[string]bool{"download": true, "guide": true}

func cleanURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	low := strings.ToLower(url)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return ""
	}
	if len(url) > 500 {
		url = url[:500]
	}
	return url
}

// ResourcesList returns resources ordered by sort_order then id.
func ResourcesList(db *gorm.DB, enabledOnly bool) []models.Resource {
	var rows []models.Resource
	q := db.Order("sort_order, id")
	if enabledOnly {
		q = q.Where("enabled = ?", true)
	}
	q.Find(&rows)
	return rows
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ResourceCreate inserts a resource from a loosely-typed payload.
func ResourceCreate(db *gorm.DB, data map[string]any) *models.Resource {
	kind := anyToStr(data["kind"])
	if !allowedKinds[kind] {
		kind = "download"
	}
	icon := anyToStr(data["icon"])
	if icon == "" {
		icon = "📦"
	}
	row := &models.Resource{
		Kind:        kind,
		Title:       clip(anyToStr(data["title"]), 120),
		Description: clip(anyToStr(data["description"]), 500),
		URL:         cleanURL(anyToStr(data["url"])),
		Icon:        clipRunes(icon, 16),
		Platform:    clip(anyToStr(data["platform"]), 40),
		SortOrder:   anyToInt(data["sort_order"]),
		Enabled:     anyToBool(data["enabled"], true),
	}
	db.Create(row)
	return row
}

// ResourceUpdate applies a partial update; returns nil if not found.
func ResourceUpdate(db *gorm.DB, id int, data map[string]any) *models.Resource {
	var row models.Resource
	if err := db.First(&row, id).Error; err != nil {
		return nil
	}
	if v, ok := data["kind"]; ok {
		if k := anyToStr(v); allowedKinds[k] {
			row.Kind = k
		}
	}
	if v, ok := data["title"]; ok {
		row.Title = clip(anyToStr(v), 120)
	}
	if v, ok := data["description"]; ok {
		row.Description = clip(anyToStr(v), 500)
	}
	if v, ok := data["url"]; ok {
		row.URL = cleanURL(anyToStr(v))
	}
	if v, ok := data["icon"]; ok {
		icon := anyToStr(v)
		if icon == "" {
			icon = "📦"
		}
		row.Icon = clipRunes(icon, 16)
	}
	if v, ok := data["platform"]; ok {
		row.Platform = clip(anyToStr(v), 40)
	}
	if v, ok := data["sort_order"]; ok {
		row.SortOrder = anyToInt(v)
	}
	if v, ok := data["enabled"]; ok {
		row.Enabled = anyToBool(v, true)
	}
	db.Save(&row)
	return &row
}

// ResourceDelete removes a resource; returns false if not found.
func ResourceDelete(db *gorm.DB, id int) bool {
	res := db.Delete(&models.Resource{}, id)
	return res.RowsAffected > 0
}

// SeedResources adds a few sample items on first run (empty table).
func SeedResources(db *gorm.DB) {
	var count int64
	db.Model(&models.Resource{}).Count(&count)
	if count > 0 {
		return
	}
	defaults := []map[string]any{
		{"kind": "download", "icon": "🤖", "title": "v2rayNG", "platform": "Android",
			"description": "کلاینت WireGuard/V2Ray برای اندروید", "url": "", "sort_order": 1},
		{"kind": "download", "icon": "🍏", "title": "WireGuard", "platform": "iOS",
			"description": "اپ رسمی WireGuard برای آیفون", "url": "", "sort_order": 2},
		{"kind": "download", "icon": "🪟", "title": "OpenVPN Connect", "platform": "Windows",
			"description": "کلاینت OpenVPN برای ویندوز", "url": "", "sort_order": 3},
		{"kind": "guide", "icon": "📘", "title": "آموزش اتصال با WireGuard", "platform": "",
			"description": "کانفیگ را دانلود کنید و در اپ import کنید؛ سپس اتصال را روشن کنید.", "url": "", "sort_order": 10},
	}
	for _, d := range defaults {
		ResourceCreate(db, d)
	}
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func anyToInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n := 0
		for _, c := range t {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int(c-'0')
		}
		return n
	default:
		return 0
	}
}

func anyToBool(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(t)
		return s == "true" || s == "1" || s == "yes" || s == "on"
	case float64:
		return t != 0
	case nil:
		return def
	default:
		return def
	}
}
