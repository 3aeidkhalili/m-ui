package services

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// protoSettings returns p if non-nil, else the current protocol settings.
func protoSettings(db *gorm.DB, p map[string]string) map[string]string {
	if p != nil {
		return p
	}
	return ProtocolsGetAll(db)
}

// GenWireGuard builds a WireGuard client config, falling back to a stored one
// for legacy users without raw key material.
func GenWireGuard(db *gorm.DB, u *models.User, p map[string]string) string {
	p = protoSettings(db, p)
	if u.WgPrivateKey == "" {
		return u.WgConfig
	}
	return "[Interface]\n" +
		fmt.Sprintf("PrivateKey = %s\n", u.WgPrivateKey) +
		fmt.Sprintf("Address = %s/32\n", u.WgIP()) +
		fmt.Sprintf("DNS = %s\n", p["wg_dns"]) +
		fmt.Sprintf("MTU = %s\n\n", p["wg_mtu"]) +
		"[Peer]\n" +
		fmt.Sprintf("PublicKey = %s\n", p["wg_server_public_key"]) +
		fmt.Sprintf("PresharedKey = %s\n", u.WgPresharedKey) +
		fmt.Sprintf("Endpoint = %s:%s\n", p["wg_endpoint"], p["wg_port"]) +
		fmt.Sprintf("AllowedIPs = %s\n", p["wg_allowed_ips"]) +
		fmt.Sprintf("PersistentKeepalive = %s\n", p["wg_keepalive"])
}

// GenOpenVPN builds an OpenVPN client .ovpn, falling back for legacy users.
// Authentication is username/password (the same credential as L2TP/IPsec): the
// server runs with `verify-client-cert none` + `auth-user-pass-verify`, so the
// client only needs the CA, tls-crypt and the user's username/password — no
// per-user client certificate. The client prompts for the credentials
// (`auth-user-pass`); the username is u.Username and the password is the user's
// L2TP password (both shown in the panel).
func GenOpenVPN(db *gorm.DB, u *models.User, p map[string]string) string {
	p = protoSettings(db, p)
	if u.OvpnCA == "" {
		return u.OvpnConfig
	}
	return "client\n" +
		"dev tun\n" +
		fmt.Sprintf("proto %s\n", p["ovpn_proto"]) +
		fmt.Sprintf("remote %s %s\n", p["ovpn_remote"], p["ovpn_port"]) +
		"resolv-retry infinite\n" +
		"nobind\n" +
		"persist-key\n" +
		"persist-tun\n" +
		"remote-cert-tls server\n" +
		"auth-user-pass\n" +
		fmt.Sprintf("data-ciphers %s\n", p["ovpn_cipher"]) +
		fmt.Sprintf("data-ciphers-fallback %s\n", p["ovpn_cipher"]) +
		"auth SHA256\n" +
		"verb 3\n" +
		fmt.Sprintf("<ca>\n%s\n</ca>\n", strings.TrimSpace(u.OvpnCA)) +
		fmt.Sprintf("<tls-crypt>\n%s\n</tls-crypt>\n", strings.TrimSpace(u.OvpnTLSCrypt))
}

// L2TPInfo is the L2TP/IPsec connection detail returned to clients.
type L2TPInfo struct {
	Server     string `json:"server"`
	PSK        string `json:"psk"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	AssignedIP string `json:"assigned_ip"`
	Hint       string `json:"hint"`
}

// GenL2TP returns the L2TP/IPsec connection info for a user.
func GenL2TP(db *gorm.DB, u *models.User, p map[string]string) L2TPInfo {
	p = protoSettings(db, p)
	return L2TPInfo{
		Server:     p["l2tp_server"],
		PSK:        Cfg.L2tpPSK,
		Username:   u.Username,
		Password:   u.L2tpPassword,
		AssignedIP: u.L2tpIP(),
		Hint:       "L2TP/IPsec PSK — روی iOS/Android/ویندوز: Server + PSK + username/password",
	}
}

// ConfigAvailability reports which protocols have a usable config for the user.
func ConfigAvailability(u *models.User) map[string]bool {
	return map[string]bool{
		"openvpn":   u.OvpnCert != "" || u.OvpnConfig != "",
		"wireguard": u.WgPrivateKey != "" || u.WgConfig != "",
		"l2tp":      u.L2tpPassword != "",
	}
}
