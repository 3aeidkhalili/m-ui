package xray

import (
	"encoding/json"
	"os"

	"multivpn/internal/models"
)

// Relay is a parsed Xray outbound object (tag-less).
type Relay = map[string]any

// clone deep-copies a relay object via JSON round-trip.
func clone(r Relay) Relay {
	b, err := json.Marshal(r)
	if err != nil {
		return Relay{}
	}
	var out Relay
	_ = json.Unmarshal(b, &out)
	return out
}

// pickRelay chooses this user's egress relay:
//  1. the user's explicitly chosen outbound, if still in the active pool;
//  2. otherwise a stable round-robin slot (index % N);
//  3. otherwise nil (direct freedom egress).
func pickRelay(u *models.User, pool []Relay, byID map[int]Relay) Relay {
	if u.OutboundID != nil {
		if r, ok := byID[*u.OutboundID]; ok {
			return r
		}
	}
	if len(pool) > 0 {
		return pool[u.Index%len(pool)]
	}
	return nil
}

// userOutbound builds the per-user outbound: a copy of the chosen relay (or
// freedom), tagged user-<id>, with sockopt.mark=255 so Xray's own egress does
// not re-enter TPROXY (loop prevention). The tag stays per-user so the unified
// byte counter is unaffected regardless of which relay is shared.
func userOutbound(u *models.User, pool []Relay, byID map[int]Relay) Relay {
	relay := pickRelay(u, pool, byID)
	var ob Relay
	if relay != nil {
		ob = clone(relay)
	} else {
		ob = Relay{"protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIP"}}
	}
	ob["tag"] = u.XrayTag()
	ss, _ := ob["streamSettings"].(map[string]any)
	if ss == nil {
		ss = map[string]any{}
		ob["streamSettings"] = ss
	}
	sock, _ := ss["sockopt"].(map[string]any)
	if sock == nil {
		sock = map[string]any{}
		ss["sockopt"] = sock
	}
	applySpeedSockopt(sock)
	return ob
}

// applySpeedSockopt sets the loop-prevention mark plus the TCP tunings that make
// the relay connection faster: BBR congestion control (far better than CUBIC on
// lossy long-haul links), TCP Fast Open (skips a round-trip on connect) and
// no-delay (disables Nagle for snappier interactive traffic).
func applySpeedSockopt(sock map[string]any) {
	sock["mark"] = 255
	sock["tcpFastOpen"] = true
	sock["tcpNoDelay"] = true
	sock["tcpKeepAliveInterval"] = 15
	if _, ok := sock["tcpcongestion"]; !ok {
		sock["tcpcongestion"] = "bbr"
	}
}

// BuildConfig assembles the whole Xray config from the active users and the
// active relay pool. When iranDirect is set, Iranian domains (.ir + the
// category-ir geosite list) and Iranian IP ranges (geoip:ir) egress through the
// server's local "direct" freedom outbound instead of the foreign relay — so
// Iranian sites stay fast/domestic, work when they block foreign IPs, and are
// not counted against the user's relay quota.
func (c *Client) BuildConfig(activeUsers []models.User, pool []Relay, byID map[int]Relay, iranDirect bool) map[string]any {
	outbounds := []Relay{
		{
			"tag":      "direct",
			"protocol": "freedom",
			"settings": map[string]any{"domainStrategy": "UseIP"},
			"streamSettings": map[string]any{"sockopt": func() map[string]any {
				s := map[string]any{}
				applySpeedSockopt(s)
				return s
			}()},
		},
		{"tag": "block", "protocol": "blackhole", "settings": map[string]any{}},
	}
	rules := []map[string]any{
		{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
	}
	// Iran split-routing must precede the per-user relay rules so .ir/Iran-IP
	// traffic is matched first and sent direct. (Xray ANDs a rule's fields, so
	// domain and ip need separate rules.)
	if iranDirect {
		rules = append(rules,
			map[string]any{"type": "field", "inboundTag": []string{"tproxy-in"},
				"domain": []string{"geosite:category-ir", "regexp:\\.ir$"}, "outboundTag": "direct"},
			map[string]any{"type": "field", "inboundTag": []string{"tproxy-in"},
				"ip": []string{"geoip:ir"}, "outboundTag": "direct"},
		)
	}
	for i := range activeUsers {
		u := &activeUsers[i]
		outbounds = append(outbounds, userOutbound(u, pool, byID))
		rules = append(rules, map[string]any{
			"type":        "field",
			"inboundTag":  []string{"tproxy-in"},
			"source":      u.SourceIPs(),
			"outboundTag": u.XrayTag(),
		})
	}
	// Any VPN traffic not matching an active user -> blocked (no internet).
	rules = append(rules, map[string]any{
		"type": "field", "inboundTag": []string{"tproxy-in"}, "outboundTag": "block",
	})

	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"api": map[string]any{
			"tag":      "api",
			"services": []string{"HandlerService", "StatsService", "LoggerService"},
		},
		"stats": map[string]any{},
		"policy": map[string]any{
			"system": map[string]any{
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
		"inbounds": []map[string]any{
			{
				"tag":      "api",
				"listen":   "127.0.0.1",
				"port":     c.cfg.XrayAPIPort(),
				"protocol": "dokodemo-door",
				"settings": map[string]any{"address": "127.0.0.1"},
			},
			{
				"tag":            "tproxy-in",
				"listen":         "0.0.0.0",
				"port":           c.cfg.TProxyPort,
				"protocol":       "dokodemo-door",
				"settings":       map[string]any{"network": "tcp,udp", "followRedirect": true},
				"streamSettings": map[string]any{"sockopt": map[string]any{"tproxy": "tproxy"}},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"routeOnly":    false,
				},
			},
		},
		"outbounds": outbounds,
		"routing":   map[string]any{"domainStrategy": "AsIs", "rules": rules},
	}
}

// WriteConfig renders the config and writes it to the configured path.
func (c *Client) WriteConfig(activeUsers []models.User, pool []Relay, byID map[int]Relay, iranDirect bool) error {
	cfg := c.BuildConfig(activeUsers, pool, byID, iranDirect)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.cfg.XrayConfig, b, 0o644)
}
