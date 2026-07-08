// Package config loads panel settings from backend/.env and the process
// environment, mirroring the former Python pydantic-settings behaviour.
package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Values that are not accepted as a secure secret key.
var weakSecrets = map[string]bool{
	"":                                  true,
	"change-me":                         true,
	"change-me-to-a-long-random-string": true,
}

// Values that are not accepted as an admin password.
var WeakPasswords = map[string]bool{
	"": true, "admin": true, "password": true, "123456": true, "changeme": true,
}

var loopbackHosts = map[string]bool{
	"127.0.0.1": true, "localhost": true, "::1": true, "": true,
}

// Config holds every runtime setting. Field names mirror the .env keys.
type Config struct {
	// panel
	PanelDomain              string
	ServerPublicIP           string
	APIHost                  string
	APIPort                  int
	SecretKey                string
	AccessTokenExpireMinutes int
	CORSOrigins              string

	// initial admin (used only by seeding)
	AdminUsername string
	AdminPassword string

	// database
	DatabaseURL string

	// Xray
	XrayBin            string
	XrayConfig         string
	XrayAPIAddr        string
	TProxyPort         int
	TrafficJobInterval int

	// per-protocol internal subnets
	OvpnSubnet     string
	WgSubnet       string
	L2tpSubnet     string
	UserIndexStart int

	// WireGuard
	WgInterface        string
	WgListenPort       int
	WgServerPrivateKey string
	WgServerPublicKey  string

	// OpenVPN
	OvpnPort     int
	OvpnProto    string
	EasyRSADir   string
	OvpnCCDDir   string
	OvpnTLSCrypt string

	// L2TP
	L2tpPSK         string
	L2tpChapSecrets string

	// provisioning
	ScriptsDir          string
	ProvisioningEnabled bool

	// behind a trusted reverse proxy (nginx)?
	TrustedProxy bool

	// directory holding fonts/geoip/world_paths.json and the built frontend
	AssetsDir string
	StaticDir string
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseEnvFile reads KEY=VALUE lines from an .env file into a map. Missing file
// is not an error (the environment may already carry the values).
func parseEnvFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// Load resolves configuration for a backend rooted at baseDir. It reads
// baseDir/.env first, then lets real environment variables win (systemd's
// EnvironmentFile puts them in the environment, matching production).
func Load(baseDir string) (*Config, error) {
	fileEnv := parseEnvFile(filepath.Join(baseDir, ".env"))

	get := func(key, def string) string {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			return v
		}
		if v, ok := fileEnv[key]; ok {
			return v
		}
		return def
	}
	getInt := func(key string, def int) int {
		s := get(key, "")
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return def
		}
		return n
	}
	getBool := func(key string, def bool) bool {
		s := strings.ToLower(get(key, ""))
		switch s {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		default:
			return def
		}
	}

	c := &Config{
		PanelDomain:              get("PANEL_DOMAIN", "localhost"),
		ServerPublicIP:           get("SERVER_PUBLIC_IP", "127.0.0.1"),
		APIHost:                  get("API_HOST", "127.0.0.1"),
		APIPort:                  getInt("API_PORT", 8000),
		SecretKey:                get("SECRET_KEY", ""),
		AccessTokenExpireMinutes: getInt("ACCESS_TOKEN_EXPIRE_MINUTES", 480),
		CORSOrigins:              get("CORS_ORIGINS", ""),

		AdminUsername: get("ADMIN_USERNAME", "admin"),
		AdminPassword: get("ADMIN_PASSWORD", ""),

		DatabaseURL: get("DATABASE_URL", "sqlite:///./multivpn.db"),

		XrayBin:            get("XRAY_BIN", "/usr/local/bin/xray"),
		XrayConfig:         get("XRAY_CONFIG", "/usr/local/etc/xray/config.json"),
		XrayAPIAddr:        get("XRAY_API_ADDR", "127.0.0.1:10085"),
		TProxyPort:         getInt("TPROXY_PORT", 12345),
		TrafficJobInterval: getInt("TRAFFIC_JOB_INTERVAL", 30),

		OvpnSubnet:     get("OVPN_SUBNET", "10.8.0.0/24"),
		WgSubnet:       get("WG_SUBNET", "10.9.0.0/24"),
		L2tpSubnet:     get("L2TP_SUBNET", "10.10.0.0/24"),
		UserIndexStart: getInt("USER_INDEX_START", 11),

		WgInterface:        get("WG_INTERFACE", "wg0"),
		WgListenPort:       getInt("WG_LISTEN_PORT", 51820),
		WgServerPrivateKey: get("WG_SERVER_PRIVATE_KEY", ""),
		WgServerPublicKey:  get("WG_SERVER_PUBLIC_KEY", ""),

		OvpnPort:     getInt("OVPN_PORT", 1194),
		OvpnProto:    get("OVPN_PROTO", "udp"),
		EasyRSADir:   get("EASYRSA_DIR", "/etc/openvpn/easy-rsa"),
		OvpnCCDDir:   get("OVPN_CCD_DIR", "/etc/openvpn/ccd"),
		OvpnTLSCrypt: get("OVPN_TLS_CRYPT", "/etc/openvpn/tls-crypt.key"),

		L2tpPSK:         get("L2TP_PSK", ""),
		L2tpChapSecrets: get("L2TP_CHAP_SECRETS", "/etc/ppp/chap-secrets"),

		ScriptsDir:          get("SCRIPTS_DIR", "/opt/multivpn/scripts"),
		ProvisioningEnabled: getBool("PROVISIONING_ENABLED", true),

		TrustedProxy: getBool("TRUSTED_PROXY", false),

		AssetsDir: get("ASSETS_DIR", filepath.Join(baseDir, "assets")),
		StaticDir: get("STATIC_DIR", filepath.Join(baseDir, "static")),
	}

	if err := c.secretGuard(); err != nil {
		return nil, err
	}
	// Ensure the scripts inherit the values they need (wg_add.sh et al. read
	// SERVER_PUBLIC_IP / WG_SERVER_PUBLIC_KEY from the environment). Under
	// systemd these are already exported; when run manually they may not be, so
	// we push the loaded .env values into the process environment here. This
	// fixes the former "provisioning fails outside systemd" foot-gun.
	c.exportForScripts(fileEnv)
	return c, nil
}

// secretGuard refuses to boot on a weak SECRET_KEY when in production
// (provisioning enabled or a public bind); on a loopback dev bind it mints an
// ephemeral per-process key instead.
func (c *Config) secretGuard() error {
	publicBind := !loopbackHosts[c.APIHost]
	weak := weakSecrets[c.SecretKey] || len(c.SecretKey) < 32
	if c.ProvisioningEnabled || publicBind {
		if weak {
			return fmt.Errorf("SECRET_KEY is weak/empty. Set a random key of at least 32 chars " +
				"in backend/.env (e.g. openssl rand -hex 32)")
		}
	} else if weak {
		c.SecretKey = randHex(32)
	}
	return nil
}

// exportForScripts pushes .env values into os.Environ so provisioning scripts
// launched via exec inherit them even when not started by systemd.
func (c *Config) exportForScripts(fileEnv map[string]string) {
	for k, v := range fileEnv {
		if _, ok := os.LookupEnv(k); !ok {
			_ = os.Setenv(k, v)
		}
	}
	// Guarantee the critical ones are present with the resolved values.
	ensure := map[string]string{
		"SERVER_PUBLIC_IP":      c.ServerPublicIP,
		"WG_INTERFACE":          c.WgInterface,
		"WG_LISTEN_PORT":        strconv.Itoa(c.WgListenPort),
		"WG_SERVER_PUBLIC_KEY":  c.WgServerPublicKey,
		"WG_SERVER_PRIVATE_KEY": c.WgServerPrivateKey,
		"EASYRSA_DIR":           c.EasyRSADir,
		"OVPN_CCD_DIR":          c.OvpnCCDDir,
		"OVPN_TLS_CRYPT":        c.OvpnTLSCrypt,
		"L2TP_CHAP_SECRETS":     c.L2tpChapSecrets,
	}
	for k, v := range ensure {
		if os.Getenv(k) == "" && v != "" {
			_ = os.Setenv(k, v)
		}
	}
}

// SQLitePath extracts the file path from a sqlite:/// DATABASE_URL, or "" if the
// URL is not sqlite.
func (c *Config) SQLitePath() string {
	const p = "sqlite:///"
	if strings.HasPrefix(c.DatabaseURL, p) {
		return strings.TrimPrefix(c.DatabaseURL, p)
	}
	return ""
}

// XrayAPIPort returns the numeric port from XRAY_API_ADDR (host:port).
func (c *Config) XrayAPIPort() int {
	i := strings.LastIndex(c.XrayAPIAddr, ":")
	if i < 0 {
		return 10085
	}
	n, err := strconv.Atoi(c.XrayAPIAddr[i+1:])
	if err != nil {
		return 10085
	}
	return n
}
