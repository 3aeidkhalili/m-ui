package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseError marks a failure to parse an outbound link/JSON.
type ParseError struct{ msg string }

func (e *ParseError) Error() string { return e.msg }

func parseErr(format string, a ...any) *ParseError {
	return &ParseError{msg: fmt.Sprintf(format, a...)}
}

// b64 decodes a possibly-unpadded url-safe or standard base64 string.
func b64(s string) (string, error) {
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(strings.TrimSpace(s))
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", parseErr("base64 decode failed")
}

// streamSettings builds an Xray streamSettings object from a network/security
// pair and query parameters.
func streamSettings(net, security string, q map[string]string, hostHdr string) map[string]any {
	network := strings.ToLower(orDefault(net, "tcp"))
	if network == "h2" {
		network = "http"
	}
	ss := map[string]any{"network": network}
	security = strings.ToLower(orDefault(security, "none"))

	sni := firstNonEmpty(q["sni"], q["peer"], hostHdr, q["host"])
	fp := q["fp"]
	switch security {
	case "reality":
		ss["security"] = "reality"
		rs := map[string]any{}
		if sni != "" {
			rs["serverName"] = sni
		}
		if fp != "" {
			rs["fingerprint"] = fp
		}
		if q["pbk"] != "" {
			rs["publicKey"] = q["pbk"]
		}
		if q["sid"] != "" {
			rs["shortId"] = q["sid"]
		}
		if q["spx"] != "" {
			rs["spiderX"] = q["spx"]
		}
		ss["realitySettings"] = rs
	case "tls", "xtls":
		ss["security"] = "tls"
		ts := map[string]any{}
		if sni != "" {
			ts["serverName"] = sni
		}
		if fp != "" {
			ts["fingerprint"] = fp
		}
		if q["alpn"] != "" {
			ts["alpn"] = strings.Split(q["alpn"], ",")
		}
		if q["allowInsecure"] == "1" || q["allowInsecure"] == "true" {
			ts["allowInsecure"] = true
		}
		ss["tlsSettings"] = ts
	}

	switch network {
	case "ws":
		w := map[string]any{"path": orDefault(q["path"], "/")}
		if h := firstNonEmpty(q["host"], hostHdr); h != "" {
			w["headers"] = map[string]any{"Host": h}
		}
		ss["wsSettings"] = w
	case "grpc":
		ss["grpcSettings"] = map[string]any{
			"serviceName": firstNonEmpty(q["serviceName"], q["path"]),
			"multiMode":   q["mode"] == "multi",
		}
	case "http":
		hs := map[string]any{"path": orDefault(q["path"], "/")}
		if h := firstNonEmpty(q["host"], hostHdr); h != "" {
			hs["host"] = strings.Split(h, ",")
		}
		ss["httpSettings"] = hs
	case "tcp":
		if q["headerType"] == "http" {
			req := map[string]any{"path": []string{orDefault(q["path"], "/")}}
			if h := firstNonEmpty(q["host"], hostHdr); h != "" {
				req["headers"] = map[string]any{"Host": []string{h}}
			}
			ss["tcpSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": req}}
		}
	}
	return ss
}

func queryMap(u *url.URL) map[string]string {
	out := map[string]string{}
	for k, v := range u.Query() {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func parseVless(link string) (map[string]any, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Hostname() == "" || u.Port() == "" {
		return nil, parseErr("incomplete vless link")
	}
	port, _ := strconv.Atoi(u.Port())
	q := queryMap(u)
	user := map[string]any{"id": u.User.Username(), "encryption": orDefault(q["encryption"], "none")}
	if q["flow"] != "" {
		user["flow"] = q["flow"]
	}
	return map[string]any{
		"protocol": "vless",
		"settings": map[string]any{"vnext": []any{map[string]any{
			"address": u.Hostname(), "port": port, "users": []any{user},
		}}},
		"streamSettings": streamSettings(orDefault(q["type"], "tcp"), orDefault(q["security"], "none"), q, ""),
	}, nil
}

func parseVmess(link string) (map[string]any, error) {
	raw, err := b64(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		return nil, err
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, parseErr("invalid vmess payload")
	}
	getS := func(k string) string { s, _ := d[k].(string); return s }
	getIntStr := func(k string, def int) int {
		switch v := d[k].(type) {
		case float64:
			return int(v)
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	}
	network := orDefault(getS("net"), "tcp")
	tls := getS("tls")
	q := map[string]string{
		"host": getS("host"), "path": orDefault(getS("path"), "/"), "sni": getS("sni"),
		"alpn": getS("alpn"), "headerType": getS("type"), "serviceName": getS("path"), "fp": getS("fp"),
	}
	security := "none"
	if tls == "tls" || tls == "reality" {
		security = "tls"
	}
	user := map[string]any{
		"id":       getS("id"),
		"alterId":  getIntStr("aid", 0),
		"security": orDefault(getS("scy"), "auto"),
	}
	return map[string]any{
		"protocol": "vmess",
		"settings": map[string]any{"vnext": []any{map[string]any{
			"address": getS("add"), "port": getIntStr("port", 443), "users": []any{user},
		}}},
		"streamSettings": streamSettings(network, security, q, getS("host")),
	}, nil
}

func parseTrojan(link string) (map[string]any, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Hostname() == "" || u.Port() == "" {
		return nil, parseErr("incomplete trojan link")
	}
	port, _ := strconv.Atoi(u.Port())
	q := queryMap(u)
	return map[string]any{
		"protocol": "trojan",
		"settings": map[string]any{"servers": []any{map[string]any{
			"address": u.Hostname(), "port": port, "password": u.User.Username(),
		}}},
		"streamSettings": streamSettings(orDefault(q["type"], "tcp"), orDefault(q["security"], "tls"), q, ""),
	}, nil
}

func parseSS(link string) (map[string]any, error) {
	body := strings.TrimPrefix(link, "ss://")
	if i := strings.Index(body, "#"); i >= 0 {
		body = body[:i]
	}
	var method, pw, host, portStr string
	if strings.Contains(body, "@") {
		at := strings.LastIndex(body, "@")
		userinfo, hostpart := body[:at], body[at+1:]
		if dec, err := b64(userinfo); err == nil {
			userinfo = dec
		}
		mp := strings.SplitN(userinfo, ":", 2)
		if len(mp) != 2 {
			return nil, parseErr("invalid shadowsocks userinfo")
		}
		method, pw = mp[0], mp[1]
		hp := strings.SplitN(hostpart, ":", 2)
		if len(hp) != 2 {
			return nil, parseErr("invalid shadowsocks host")
		}
		host = hp[0]
		portStr = strings.SplitN(strings.SplitN(hp[1], "/", 2)[0], "?", 2)[0]
	} else {
		dec, err := b64(body)
		if err != nil {
			return nil, err
		}
		at := strings.LastIndex(dec, "@")
		if at < 0 {
			return nil, parseErr("invalid shadowsocks link")
		}
		methodpw, hostport := dec[:at], dec[at+1:]
		mp := strings.SplitN(methodpw, ":", 2)
		if len(mp) != 2 {
			return nil, parseErr("invalid shadowsocks userinfo")
		}
		method, pw = mp[0], mp[1]
		hp := strings.SplitN(hostport, ":", 2)
		if len(hp) != 2 {
			return nil, parseErr("invalid shadowsocks host")
		}
		host, portStr = hp[0], hp[1]
	}
	port, _ := strconv.Atoi(portStr)
	return map[string]any{
		"protocol": "shadowsocks",
		"settings": map[string]any{"servers": []any{map[string]any{
			"address": host, "port": port, "method": method, "password": pw,
		}}},
	}, nil
}

// ParseOutbound converts a subscription link or raw JSON into a normalized
// (tag-less) Xray outbound object.
func ParseOutbound(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, parseErr("empty input")
	}
	if strings.HasPrefix(text, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return nil, parseErr("invalid JSON: %v", err)
		}
		var ob map[string]any
		if arr, ok := obj["outbounds"].([]any); ok && len(arr) > 0 {
			ob, _ = arr[0].(map[string]any)
		} else if _, ok := obj["protocol"]; ok {
			ob = obj
		}
		if ob == nil {
			return nil, parseErr(`JSON must be an outbound or {"outbounds":[...]}`)
		}
		delete(ob, "tag")
		return ob, nil
	}

	low := strings.ToLower(text)
	switch {
	case strings.HasPrefix(low, "vless://"):
		return parseVless(text)
	case strings.HasPrefix(low, "vmess://"):
		return parseVmess(text)
	case strings.HasPrefix(low, "trojan://"):
		return parseTrojan(text)
	case strings.HasPrefix(low, "ss://"):
		return parseSS(text)
	}
	return nil, parseErr("unsupported: provide a vless/vmess/trojan/ss link or an Xray outbound JSON")
}

// AddressOf returns "host:port" for the first server in an outbound.
func AddressOf(ob map[string]any) string {
	s, _ := ob["settings"].(map[string]any)
	if s == nil {
		return ""
	}
	if vnext, ok := s["vnext"].([]any); ok && len(vnext) > 0 {
		if v, ok := vnext[0].(map[string]any); ok {
			return fmt.Sprintf("%v:%v", v["address"], numToStr(v["port"]))
		}
	}
	if servers, ok := s["servers"].([]any); ok && len(servers) > 0 {
		if sv, ok := servers[0].(map[string]any); ok {
			return fmt.Sprintf("%v:%v", sv["address"], numToStr(sv["port"]))
		}
	}
	return ""
}

func numToStr(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.Itoa(int(t))
	case int:
		return strconv.Itoa(t)
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
