package services

import (
	"strconv"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

const protoPrefix = "proto__"

// ProtocolFields is the schema of per-protocol connection params (fa-IR labels).
var ProtocolFields = []Field{
	// WireGuard
	{"wg_endpoint", "هاست/دامنه‌ی Endpoint", "text", "WireGuard"},
	{"wg_port", "پورت", "number", "WireGuard"},
	{"wg_server_public_key", "کلید عمومی سرور", "text", "WireGuard"},
	{"wg_dns", "DNS", "text", "WireGuard"},
	{"wg_allowed_ips", "AllowedIPs", "text", "WireGuard"},
	{"wg_mtu", "MTU", "number", "WireGuard"},
	{"wg_keepalive", "PersistentKeepalive", "number", "WireGuard"},
	// OpenVPN
	{"ovpn_remote", "هاست/دامنه", "text", "OpenVPN"},
	{"ovpn_port", "پورت", "number", "OpenVPN"},
	{"ovpn_proto", "پروتکل (udp/tcp)", "text", "OpenVPN"},
	{"ovpn_cipher", "Cipher", "text", "OpenVPN"},
	// L2TP
	{"l2tp_server", "هاست/دامنه‌ی سرور", "text", "L2TP"},
}

func protocolKeys() map[string]bool {
	m := map[string]bool{}
	for _, f := range ProtocolFields {
		m[f.Key] = true
	}
	return m
}

func protocolDefaults() map[string]string {
	ip := Cfg.ServerPublicIP
	return map[string]string{
		"wg_endpoint":          ip,
		"wg_port":              strconv.Itoa(Cfg.WgListenPort),
		"wg_server_public_key": Cfg.WgServerPublicKey,
		"wg_dns":               "1.1.1.1",
		"wg_allowed_ips":       "0.0.0.0/0, ::/0",
		"wg_mtu":               "1420",
		"wg_keepalive":         "25",
		"ovpn_remote":          ip,
		"ovpn_port":            strconv.Itoa(Cfg.OvpnPort),
		"ovpn_proto":           Cfg.OvpnProto,
		"ovpn_cipher":          "AES-256-GCM",
		"l2tp_server":          ip,
	}
}

// ProtocolsGetAll returns default-merged protocol connection settings.
func ProtocolsGetAll(db *gorm.DB) map[string]string {
	keys := protocolKeys()
	values := protocolDefaults()
	var rows []models.Setting
	db.Where("key LIKE ?", protoPrefix+"%").Find(&rows)
	for _, r := range rows {
		key := r.Key[len(protoPrefix):]
		if keys[key] {
			values[key] = r.Value
		}
	}
	return values
}

// ProtocolsUpdate applies a partial update and returns all protocol settings.
func ProtocolsUpdate(db *gorm.DB, data map[string]any) map[string]string {
	keys := protocolKeys()
	for key, raw := range data {
		if !keys[key] {
			continue
		}
		val := anyToStr(raw)
		if len(val) > 512 {
			val = val[:512]
		}
		upsertSetting(db, protoPrefix+key, val)
	}
	return ProtocolsGetAll(db)
}
