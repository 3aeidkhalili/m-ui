// Package xray bridges to Xray-core via the `xray api` subcommand and to the
// systemd service, and generates the Xray configuration.
package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"multivpn/internal/config"
)

// Client talks to a running Xray instance and its systemd unit.
type Client struct {
	cfg *config.Config
}

// New returns an Xray client bound to the given config.
func New(cfg *config.Config) *Client { return &Client{cfg: cfg} }

type statResp struct {
	Stat []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	} `json:"stat"`
}

// QueryOutboundTraffic returns per-outbound usage {tag: bytes} (up+down). With
// reset=true the counters are zeroed after reading, so each call yields the
// delta since the previous read.
func (c *Client) QueryOutboundTraffic(reset bool) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The pattern is a positional argument (not a -pattern flag) and must come
	// after all flags, because Go's flag parser stops at the first non-flag arg.
	cmd := exec.CommandContext(ctx, c.cfg.XrayBin, "api", "statsquery",
		"--server="+c.cfg.XrayAPIAddr,
		"-reset="+strconv.FormatBool(reset),
		"outbound>>>",
	)
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		if msg == "" {
			msg = "xray statsquery failed"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var data statResp
	if len(out) == 0 {
		return map[string]int64{}, nil
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	result := map[string]int64{}
	for _, s := range data.Stat {
		parts := strings.Split(s.Name, ">>>")
		// outbound>>>user-5>>>traffic>>>uplink
		if len(parts) == 4 && parts[0] == "outbound" && parts[2] == "traffic" {
			result[parts[1]] += toInt64(s.Value)
		}
	}
	return result, nil
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

// Reload restarts the Xray service so a freshly written config takes effect.
func (c *Client) Reload() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "restart", "xray").Run(); err != nil {
		log.Printf("failed to restart xray: %v", err)
	}
}
