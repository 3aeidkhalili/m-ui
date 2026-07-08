// Package models defines the database schema (GORM structs) and the computed
// user properties that mirror the former SQLAlchemy model.
package models

import (
	"fmt"
	"net"
	"time"
)

// net params derived from config, set once at startup by Init.
var (
	ovpnSubnet string
	wgSubnet   string
	l2tpSubnet string
)

// Init records the per-protocol subnets used to derive each user's static IPs.
func Init(ovpn, wg, l2tp string) {
	ovpnSubnet, wgSubnet, l2tpSubnet = ovpn, wg, l2tp
}

// hostIP returns host number `index` inside a CIDR subnet
// (e.g. 10.8.0.0/24 + 11 -> 10.8.0.11).
func hostIP(subnet string, index int) string {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return ""
	}
	ip := ipnet.IP.To4()
	if ip == nil {
		return ""
	}
	v := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	v += uint32(index)
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// Setting is the admin-editable key/value store (site + protocol settings).
type Setting struct {
	Key   string `gorm:"column:key;primaryKey;size:64"`
	Value string `gorm:"column:value;type:text"`
}

func (Setting) TableName() string { return "settings" }

// Outbound is an Xray relay every user's traffic can egress through.
type Outbound struct {
	ID          int       `gorm:"column:id;primaryKey"`
	Name        string    `gorm:"column:name;size:128"`
	Protocol    string    `gorm:"column:protocol;size:32"`
	Address     string    `gorm:"column:address;size:255"`
	ConfigJSON  string    `gorm:"column:config_json;type:text"`
	IsActive    bool      `gorm:"column:is_active;not null;default:false"`
	EgressIP    string    `gorm:"column:egress_ip;size:64"`
	CountryCode string    `gorm:"column:country_code;size:4"`
	CountryName string    `gorm:"column:country_name;size:64"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (Outbound) TableName() string { return "outbounds" }

// Connection records an IP that hit a user's subscription link (offline geo).
type Connection struct {
	ID          int       `gorm:"column:id;primaryKey"`
	UserID      int       `gorm:"column:user_id;index;not null"`
	IP          string    `gorm:"column:ip;index;not null;size:64"`
	City        string    `gorm:"column:city;size:80"`
	Country     string    `gorm:"column:country;size:80"`
	CountryCode string    `gorm:"column:country_code;size:4"`
	Lat         *float64  `gorm:"column:lat"`
	Lon         *float64  `gorm:"column:lon"`
	Hits        int       `gorm:"column:hits;default:1"`
	FirstSeen   time.Time `gorm:"column:first_seen;autoCreateTime"`
	LastSeen    time.Time `gorm:"column:last_seen;autoCreateTime"`
}

func (Connection) TableName() string { return "connections" }

// Alert is a security event recorded for a user (shown on the subscription page).
// Kind "ip_limit" is a too-many-IPs warning (Strike = the strike number it
// carried); kind "blocked" is the auto-disable after the 3rd strike.
type Alert struct {
	ID        int       `gorm:"column:id;primaryKey"`
	UserID    int       `gorm:"column:user_id;index;not null"`
	Kind      string    `gorm:"column:kind;size:24;not null"`
	Message   string    `gorm:"column:message;size:255"`
	IPCount   int       `gorm:"column:ip_count"`
	IPs       string    `gorm:"column:ips;type:text"`
	Strike    int       `gorm:"column:strike"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index"`
}

func (Alert) TableName() string { return "alerts" }

// LogEvent is one entry in the panel-wide activity/audit log. Category groups
// the source (auth, user, location, connection, ip_limit, tarpit, bandwidth,
// system); Level is info | warn | critical.
type LogEvent struct {
	ID        int       `gorm:"column:id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index"`
	Category  string    `gorm:"column:category;size:24;index"`
	Level     string    `gorm:"column:level;size:12"`
	Actor     string    `gorm:"column:actor;size:96"`
	Message   string    `gorm:"column:message;size:400"`
}

func (LogEvent) TableName() string { return "logs" }

// L2tpIndexForIP reverses an L2TP internal IP (10.10.0.N) back to the host index
// N, or -1 if the IP is not inside the L2TP subnet. Used to map a captured
// public IP (keyed by the session's internal IP) back to a user.
func L2tpIndexForIP(ip string) int { return indexForIP(l2tpSubnet, ip) }

func indexForIP(subnet, ip string) int {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return -1
	}
	base := ipnet.IP.To4()
	target := net.ParseIP(ip).To4()
	if base == nil || target == nil || !ipnet.Contains(target) {
		return -1
	}
	b := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	t := uint32(target[0])<<24 | uint32(target[1])<<16 | uint32(target[2])<<8 | uint32(target[3])
	return int(t - b)
}

// Resource is a "guides & downloads" item shown on the subscription page.
type Resource struct {
	ID          int       `gorm:"column:id;primaryKey"`
	Kind        string    `gorm:"column:kind;size:16;default:download"`
	Title       string    `gorm:"column:title;size:120"`
	Description string    `gorm:"column:description;size:500"`
	URL         string    `gorm:"column:url;size:500"`
	Icon        string    `gorm:"column:icon;size:16"`
	Platform    string    `gorm:"column:platform;size:40"`
	SortOrder   int       `gorm:"column:sort_order;default:0"`
	Enabled     bool      `gorm:"column:enabled;not null;default:true"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (Resource) TableName() string { return "resources" }

// Admin is a panel administrator.
type Admin struct {
	ID             int    `gorm:"column:id;primaryKey"`
	Username       string `gorm:"column:username;uniqueIndex;not null;size:64"`
	HashedPassword string `gorm:"column:hashed_password;not null;size:255"`
	// bumped on every password change -> revokes previously-issued tokens.
	TokenVersion int `gorm:"column:token_version;not null;default:1"`
}

func (Admin) TableName() string { return "admins" }

// User is a VPN account present on all three protocols with one unified quota.
type User struct {
	ID       int    `gorm:"column:id;primaryKey"`
	Username string `gorm:"column:username;uniqueIndex;not null;size:64"`
	// unique index that derives the static IP on each protocol.
	Index int `gorm:"column:index;uniqueIndex;not null"`

	QuotaBytes int64      `gorm:"column:quota_bytes;default:0"` // 0 = unlimited
	UsedBytes  int64      `gorm:"column:used_bytes;default:0"`
	Enabled    bool       `gorm:"column:enabled;not null;default:true"`
	// IPLimit is the max number of distinct simultaneous source IPs allowed
	// across all protocols. 0 = unlimited (no enforcement). Strikes counts the
	// IP-limit violations recorded so far; at 3 the account is auto-disabled.
	IPLimit int `gorm:"column:ip_limit;not null;default:0"`
	Strikes int `gorm:"column:strikes;not null;default:0"`
	// BandwidthMbps is the per-user download speed cap in Mbit/s (traffic shaped
	// via tc HTB on the VPN interfaces). 0 = unlimited.
	BandwidthMbps int `gorm:"column:bandwidth_mbps;not null;default:0"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
	Note       string     `gorm:"column:note;size:255"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime"`
	SubToken   string     `gorm:"column:sub_token;uniqueIndex;size:64"`
	// chosen egress location; nil = automatic (round-robin over the pool).
	OutboundID *int `gorm:"column:outbound_id"`

	// raw credentials / material used to regenerate client configs on the fly.
	OvpnConfig     string `gorm:"column:ovpn_config;type:text"`
	OvpnCA         string `gorm:"column:ovpn_ca;type:text"`
	OvpnCert       string `gorm:"column:ovpn_cert;type:text"`
	OvpnKey        string `gorm:"column:ovpn_key;type:text"`
	OvpnTLSCrypt   string `gorm:"column:ovpn_tls_crypt;type:text"`
	WgPrivateKey   string `gorm:"column:wg_private_key;size:64"`
	WgPublicKey    string `gorm:"column:wg_public_key;size:64"`
	WgPresharedKey string `gorm:"column:wg_preshared_key;size:64"`
	WgConfig       string `gorm:"column:wg_config;type:text"`
	L2tpPassword   string `gorm:"column:l2tp_password;size:64"`
}

func (User) TableName() string { return "users" }

// ---- derived addresses ----

func (u *User) OvpnIP() string { return hostIP(ovpnSubnet, u.Index) }
func (u *User) WgIP() string   { return hostIP(wgSubnet, u.Index) }
func (u *User) L2tpIP() string { return hostIP(l2tpSubnet, u.Index) }

func (u *User) SourceIPs() []string {
	return []string{u.OvpnIP(), u.WgIP(), u.L2tpIP()}
}

func (u *User) XrayTag() string { return fmt.Sprintf("user-%d", u.ID) }

// ---- status ----

func (u *User) IsExpired() bool {
	if u.ExpiresAt == nil {
		return false
	}
	return !u.ExpiresAt.After(time.Now().UTC())
}

func (u *User) IsOverQuota() bool {
	return u.QuotaBytes > 0 && u.UsedBytes >= u.QuotaBytes
}

// RemainingBytes returns the remaining quota, or nil for unlimited.
func (u *User) RemainingBytes() *int64 {
	if u.QuotaBytes <= 0 {
		return nil
	}
	r := u.QuotaBytes - u.UsedBytes
	if r < 0 {
		r = 0
	}
	return &r
}

// IsActive reports whether the user should be present in the Xray config.
func (u *User) IsActive() bool {
	return u.Enabled && !u.IsExpired() && !u.IsOverQuota()
}

func (u *User) Status() string {
	switch {
	case !u.Enabled:
		return "disabled"
	case u.IsExpired():
		return "expired"
	case u.IsOverQuota():
		return "limited"
	default:
		return "active"
	}
}
