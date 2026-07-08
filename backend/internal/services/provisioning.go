package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"multivpn/internal/models"
)

// ProvisionArtifacts holds the credential material returned by the add scripts.
type ProvisionArtifacts struct {
	Ovpn map[string]string
	Wg   map[string]string
	L2tp map[string]string
}

// callScript runs bash <scripts_dir>/<name> <args...> and parses its JSON stdout.
// Returns an empty map (no error) when provisioning is disabled.
func callScript(name string, args ...string) (map[string]string, error) {
	if !Cfg.ProvisioningEnabled {
		log.Printf("provisioning disabled; skipping %s", name)
		return map[string]string{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	path := Cfg.ScriptsDir + "/" + name
	cmd := exec.CommandContext(ctx, "bash", append([]string{path}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr == "" {
			stderr = strings.TrimSpace(string(out))
		}
		return nil, fmt.Errorf("%s failed: %s", name, stderr)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return map[string]string{}, nil
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		limit := trimmed
		if len(limit) > 200 {
			limit = limit[:200]
		}
		return nil, fmt.Errorf("%s returned invalid JSON: %s", name, limit)
	}
	return parsed, nil
}

// ProvisionUser creates credentials on OpenVPN + WireGuard + L2TP. On a partial
// failure it rolls back any credentials already created (fixes the former
// orphaned-credential leak) and returns the error.
func ProvisionUser(u *models.User) (*ProvisionArtifacts, error) {
	ovpn, err := callScript("ovpn_add.sh", u.Username, u.OvpnIP(), u.L2tpPassword)
	if err != nil {
		return nil, err
	}
	wg, err := callScript("wg_add.sh", u.Username, u.WgIP())
	if err != nil {
		_, _ = callScript("ovpn_del.sh", u.Username)
		return nil, err
	}
	l2tp, err := callScript("l2tp_add.sh", u.Username, u.L2tpPassword, u.L2tpIP())
	if err != nil {
		_, _ = callScript("ovpn_del.sh", u.Username)
		_, _ = callScript("wg_del.sh", u.Username, wg["public_key"])
		return nil, err
	}
	return &ProvisionArtifacts{Ovpn: ovpn, Wg: wg, L2tp: l2tp}, nil
}

// DeprovisionUser removes credentials from all three protocols (errors logged).
func DeprovisionUser(u *models.User) {
	steps := []struct {
		name string
		args []string
	}{
		{"ovpn_del.sh", []string{u.Username}},
		{"wg_del.sh", []string{u.Username, u.WgPublicKey}},
		{"l2tp_del.sh", []string{u.Username}},
	}
	for _, s := range steps {
		if _, err := callScript(s.name, s.args...); err != nil {
			log.Printf("deprovision %s: %v", s.name, err)
		}
	}
}
