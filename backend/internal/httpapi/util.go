package httpapi

import (
	"strconv"
	"time"

	"multivpn/internal/models"
)

func itoa(n int) string { return strconv.Itoa(n) }

// isoPtr formats a time as an RFC3339 string pointer, or nil for the zero time.
func isoPtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func isoPtrOpt(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return isoPtr(*t)
}

// userOut is the JSON shape returned for a user (mirrors the former UserOut).
type userOut struct {
	ID             int     `json:"id"`
	Username       string  `json:"username"`
	Index          int     `json:"index"`
	QuotaBytes     int64   `json:"quota_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	RemainingBytes *int64  `json:"remaining_bytes"`
	Enabled        bool    `json:"enabled"`
	IsActive       bool    `json:"is_active"`
	Status         string  `json:"status"`
	IPLimit        int     `json:"ip_limit"`
	Strikes        int     `json:"strikes"`
	BandwidthMbps  int     `json:"bandwidth_mbps"`
	ExpiresAt      *string `json:"expires_at"`
	Note           string  `json:"note"`
	CreatedAt      *string `json:"created_at"`
	OvpnIP         string  `json:"ovpn_ip"`
	WgIP           string  `json:"wg_ip"`
	L2tpIP         string  `json:"l2tp_ip"`
	SubToken       string  `json:"sub_token"`
}

func toUserOut(u *models.User) userOut {
	return userOut{
		ID:             u.ID,
		Username:       u.Username,
		Index:          u.Index,
		QuotaBytes:     u.QuotaBytes,
		UsedBytes:      u.UsedBytes,
		RemainingBytes: u.RemainingBytes(),
		Enabled:        u.Enabled,
		IsActive:       u.IsActive(),
		Status:         u.Status(),
		IPLimit:        u.IPLimit,
		Strikes:        u.Strikes,
		BandwidthMbps:  u.BandwidthMbps,
		ExpiresAt:      isoPtrOpt(u.ExpiresAt),
		Note:           u.Note,
		CreatedAt:      isoPtr(u.CreatedAt),
		OvpnIP:         u.OvpnIP(),
		WgIP:           u.WgIP(),
		L2tpIP:         u.L2tpIP(),
		SubToken:       u.SubToken,
	}
}
