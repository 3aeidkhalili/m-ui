package services

import (
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// Field describes one editable setting for the admin form renderer.
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Group string `json:"group"`
}

// SettingsFields is the schema of site settings (rendered in the panel, fa-IR).
var SettingsFields = []Field{
	{"panel_title", "عنوان پنل", "text", "عمومی"},
	{"server_public_ip", "IP/دامنه‌ی عمومی سرور", "text", "عمومی"},
	{"client_dns", "DNS کلاینت‌ها", "text", "عمومی"},
	{"subscription_base_url", "آدرس پایه‌ی اشتراک (https://…)", "text", "اشتراک"},
	{"announcement", "پیام اعلان (در صفحه‌ی اشتراک)", "textarea", "اشتراک"},
	{"default_quota_gb", "حجم پیش‌فرض کاربر جدید (GB)", "number", "پیش‌فرض کاربر"},
	{"default_expires_days", "انقضای پیش‌فرض کاربر جدید (روز)", "number", "پیش‌فرض کاربر"},
	{"default_ip_limit", "حد IP هم‌زمان (۰=نامحدود؛ روی کاربرانِ بدونِ حدِ اختصاصی)", "number", "پیش‌فرض کاربر"},
	{"default_bandwidth_mbps", "محدودیت سرعت دانلود (Mbps) (۰=نامحدود؛ روی کاربرانِ بدونِ حدِ اختصاصی)", "number", "پیش‌فرض کاربر"},
}

func settingsKeys() map[string]bool {
	m := map[string]bool{}
	for _, f := range SettingsFields {
		m[f.Key] = true
	}
	return m
}

func settingsDefaults() map[string]string {
	return map[string]string{
		"panel_title":           "MultiVPN",
		"server_public_ip":      Cfg.ServerPublicIP,
		"client_dns":            "1.1.1.1",
		"subscription_base_url": "",
		"announcement":          "",
		"default_quota_gb":       "50",
		"default_expires_days":   "30",
		"default_ip_limit":       "0",
		"default_bandwidth_mbps": "0",
	}
}

// settingsRows caches the settings table for a short window so the many
// SettingsGetAll / EffectiveIPLimit / EffectiveBandwidth calls per request and
// per job tick do not each full-scan the table. A fresh map is rebuilt per call
// (callers may mutate it), but the DB read is skipped while the cache is warm.
var (
	settingsRowsMu sync.Mutex
	settingsRows   []models.Setting
	settingsRowsAt time.Time
)

const settingsCacheTTL = 3 * time.Second

func cachedSettingsRows(db *gorm.DB) []models.Setting {
	settingsRowsMu.Lock()
	defer settingsRowsMu.Unlock()
	if settingsRows != nil && time.Since(settingsRowsAt) < settingsCacheTTL {
		return settingsRows
	}
	var rows []models.Setting
	db.Find(&rows)
	settingsRows = rows
	settingsRowsAt = time.Now()
	return rows
}

func invalidateSettingsCache() {
	settingsRowsMu.Lock()
	settingsRows = nil
	settingsRowsMu.Unlock()
}

// SettingsGetAll returns default-merged site settings.
func SettingsGetAll(db *gorm.DB) map[string]string {
	keys := settingsKeys()
	values := settingsDefaults()
	for _, r := range cachedSettingsRows(db) {
		if keys[r.Key] {
			values[r.Key] = r.Value
		}
	}
	return values
}

// SettingRaw reads any settings-table key directly (not limited to the site
// settings schema), returning def when absent.
func SettingRaw(db *gorm.DB, key, def string) string {
	var row models.Setting
	if err := db.Where("key = ?", key).First(&row).Error; err == nil {
		return row.Value
	}
	return def
}

// SetSetting upserts a raw settings key (used by feature toggles like Iran
// split-routing that live outside the editable site-settings form).
func SetSetting(db *gorm.DB, key, val string) {
	upsertSetting(db, key, val)
}

const iranDirectKey = "iran_direct"

// IranDirectEnabled reports whether Iran split-routing (.ir + geoip:ir -> direct)
// is on. Default: on (the common choice for Iran-facing deployments).
func IranDirectEnabled(db *gorm.DB) bool {
	return SettingRaw(db, iranDirectKey, "on") != "off"
}

// SettingsGet returns one site setting value.
func SettingsGet(db *gorm.DB, key string) string {
	return SettingsGetAll(db)[key]
}

func sanitizeSetting(key, value string) string {
	val := value
	// Only http(s) for URLs — blocks javascript:/data: (XSS in panel href).
	if key == "subscription_base_url" && val != "" {
		low := strings.ToLower(strings.TrimSpace(val))
		if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
			return ""
		}
	}
	if len(val) > 2000 {
		val = val[:2000]
	}
	return val
}

// SettingsUpdate applies a partial update to site settings and returns all.
func SettingsUpdate(db *gorm.DB, data map[string]any) map[string]string {
	keys := settingsKeys()
	for key, raw := range data {
		if !keys[key] {
			continue
		}
		val := sanitizeSetting(key, anyToStr(raw))
		upsertSetting(db, key, val)
	}
	return SettingsGetAll(db)
}

func upsertSetting(db *gorm.DB, key, val string) {
	defer invalidateSettingsCache()
	var row models.Setting
	if err := db.First(&row, "key = ?", key).Error; err == nil {
		db.Model(&models.Setting{}).Where("key = ?", key).Update("value", val)
	} else {
		db.Create(&models.Setting{Key: key, Value: val})
	}
}

func anyToStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return numToStr(t)
	default:
		return numToStr(t)
	}
}
