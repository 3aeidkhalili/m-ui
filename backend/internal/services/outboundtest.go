package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// only one outbound test may run at a time (avoid spawning many temp Xrays).
var testMu sync.Mutex

func splitHostPort(address string) (string, int, bool) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", 0, false
	}
	var host, portStr string
	if strings.HasPrefix(address, "[") && strings.Contains(address, "]") {
		end := strings.Index(address, "]")
		host = address[1:end]
		portStr = strings.TrimPrefix(address[end+1:], ":")
	} else {
		i := strings.LastIndex(address, ":")
		if i < 0 {
			return "", 0, false
		}
		host, portStr = address[:i], address[i+1:]
	}
	if host == "" || portStr == "" {
		return "", 0, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, false
	}
	return host, port, true
}

func tcpCheck(host string, port int) map[string]any {
	t0 := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 6*time.Second)
	if err != nil {
		return map[string]any{"ok": false, "latency_ms": nil, "error": clip(err.Error(), 200)}
	}
	conn.Close()
	return map[string]any{"ok": true, "latency_ms": int(time.Since(t0).Milliseconds()), "error": ""}
}

func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func proxyCheck(ob map[string]any) map[string]any {
	res := map[string]any{"ran": false, "ok": false, "latency_ms": nil, "egress_ip": "",
		"country_code": "", "country_name": "", "error": ""}
	if !Cfg.ProvisioningEnabled {
		res["error"] = "در حالت توسعه اجرا نمی‌شود (فقط روی سرور)"
		return res
	}
	if Cfg.XrayBin == "" {
		res["error"] = "باینری Xray یافت نشد"
		return res
	}
	if _, err := os.Stat(Cfg.XrayBin); err != nil {
		res["error"] = "باینری Xray یافت نشد"
		return res
	}

	port := freePort()
	obCopy := map[string]any{}
	for k, v := range ob {
		obCopy[k] = v
	}
	obCopy["tag"] = "proxy"
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{{
			"tag": "in", "listen": "127.0.0.1", "port": port,
			"protocol": "socks", "settings": map[string]any{"udp": false},
		}},
		"outbounds": []any{obCopy},
	}

	tmp, err := os.CreateTemp("", "xray-test-*.json")
	if err != nil {
		res["error"] = clip(err.Error(), 200)
		res["ran"] = true
		return res
	}
	path := tmp.Name()
	defer os.Remove(path)
	b, _ := json.Marshal(cfg)
	tmp.Write(b)
	tmp.Close()

	var stderr bytes.Buffer
	cmd := exec.Command(Cfg.XrayBin, "run", "-c", path)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		res["error"] = clip(err.Error(), 200)
		res["ran"] = true
		return res
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	time.Sleep(1 * time.Second) // give Xray a moment to boot
	select {
	case <-exited:
		last := lastLine(stderr.String())
		if last == "" {
			last = "Xray بوت نشد"
		}
		res["error"] = clip(last, 200)
		res["ran"] = true
		return res
	default:
	}

	proxy := fmt.Sprintf("socks5h://127.0.0.1:%d", port)
	t0 := time.Now()
	egress, cc, cn := "", "", ""

	if out, ok := runCmd(11*time.Second, "curl", "-s", "--max-time", "8", "-x", proxy,
		"http://ip-api.com/json/?fields=status,query,countryCode,country"); ok && out != "" {
		var j map[string]any
		if json.Unmarshal([]byte(out), &j) == nil {
			if s, _ := j["status"].(string); s == "success" {
				egress = strings.TrimSpace(anyToStr(j["query"]))
				cc = strings.ToLower(strings.TrimSpace(anyToStr(j["countryCode"])))
				cn = strings.TrimSpace(anyToStr(j["country"]))
			}
		}
	}
	if egress == "" {
		if out, ok := runCmd(8*time.Second, "curl", "-s", "--max-time", "5", "-x", proxy, "https://api.ipify.org"); ok {
			o := strings.TrimSpace(out)
			if o != "" && len(o) < 60 {
				egress = o
			}
		}
	}

	res["latency_ms"] = int(time.Since(t0).Milliseconds())
	res["ran"] = true
	if egress != "" {
		res["ok"] = true
		res["egress_ip"] = egress
		res["country_code"] = cc
		res["country_name"] = cn
	} else {
		res["error"] = "عبور ترافیک از رله ناموفق بود (احراز/TLS/اینترنتِ رله؟)"
	}
	return res
}

// TestOutbound runs the full connectivity test (TCP + real egress) for a relay.
func TestOutbound(ob map[string]any) map[string]any {
	if !testMu.TryLock() {
		return map[string]any{"ok": false, "busy": true, "address": "",
			"message": "یک تست دیگر در حال اجراست؛ چند لحظه بعد دوباره امتحان کنید.",
			"tcp":     map[string]any{}, "proxy": map[string]any{}}
	}
	defer testMu.Unlock()

	address := AddressOf(ob)
	host, port, ok := splitHostPort(address)
	if !ok {
		return map[string]any{"ok": false, "address": address,
			"message": "آدرس رله در این اوتباند نامعتبر است.", "tcp": map[string]any{}, "proxy": map[string]any{}}
	}
	tcp := tcpCheck(host, port)
	if b, _ := tcp["ok"].(bool); !b {
		return map[string]any{"ok": false, "address": address, "tcp": tcp,
			"proxy":   map[string]any{"ran": false},
			"message": fmt.Sprintf("سرور در دسترس نیست: %v — میزبان/پورت/فایروالِ رله را بررسی کنید.", tcp["error"])}
	}

	proxy := proxyCheck(ob)
	if ran, _ := proxy["ran"].(bool); ran {
		if pok, _ := proxy["ok"].(bool); pok {
			msg := fmt.Sprintf("✅ اتصال کامل موفق — ترافیک از رله عبور کرد (IP خروجی: %v، %vms).",
				proxy["egress_ip"], proxy["latency_ms"])
			return map[string]any{"ok": true, "address": address, "tcp": tcp, "proxy": proxy, "message": msg}
		}
		msg := fmt.Sprintf("پورت باز است (%vms) ولی عبورِ ترافیک از رله ناموفق بود: %v",
			tcp["latency_ms"], proxy["error"])
		return map[string]any{"ok": false, "address": address, "tcp": tcp, "proxy": proxy, "message": msg}
	}

	note, _ := proxy["error"].(string)
	extra := ""
	if note != "" {
		extra = "تستِ کاملِ عبور: " + note
	}
	return map[string]any{"ok": true, "address": address, "tcp": tcp, "proxy": proxy,
		"message": strings.TrimSpace(fmt.Sprintf("پورت سرور باز است (%vms). %s", tcp["latency_ms"], extra))}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}
