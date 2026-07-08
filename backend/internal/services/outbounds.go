package services

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"multivpn/internal/models"
	"multivpn/internal/xray"
)

// OutboundsList returns all outbounds ordered by id.
func OutboundsList(db *gorm.DB) []models.Outbound {
	var rows []models.Outbound
	db.Order("id").Find(&rows)
	return rows
}

// getActive returns the first active outbound (for display/compat), or nil.
func getActive(db *gorm.DB) *models.Outbound {
	var row models.Outbound
	if err := db.Where("is_active = ?", true).Order("id").First(&row).Error; err != nil {
		return nil
	}
	return &row
}

// ActiveGroupName returns the name of the active outbound group, or "".
func ActiveGroupName(db *gorm.DB) string {
	if r := getActive(db); r != nil {
		return r.Name
	}
	return ""
}

// GetActiveGroup returns all active (same-name) relays, ordered by id.
func GetActiveGroup(db *gorm.DB) []models.Outbound {
	var rows []models.Outbound
	db.Where("is_active = ?", true).Order("id").Find(&rows)
	return rows
}

// GetActiveRelays returns (id, parsed config) pairs for the active pool.
func GetActiveRelays(db *gorm.DB) ([]int, []xray.Relay) {
	var ids []int
	var relays []xray.Relay
	for _, row := range GetActiveGroup(db) {
		var cfg xray.Relay
		if err := json.Unmarshal([]byte(row.ConfigJSON), &cfg); err != nil {
			continue
		}
		ids = append(ids, row.ID)
		relays = append(relays, cfg)
	}
	return ids, relays
}

// ActiveIDs returns the set of active outbound ids.
func ActiveIDs(db *gorm.DB) map[int]bool {
	m := map[int]bool{}
	for _, r := range GetActiveGroup(db) {
		m[r.ID] = true
	}
	return m
}

// OutboundCreate parses text into an outbound and stores it, auto-joining the
// active group when it shares its name or when all existing outbounds are active.
func OutboundCreate(db *gorm.DB, name, text string) (*models.Outbound, error) {
	ob, err := ParseOutbound(text)
	if err != nil {
		return nil, err
	}
	proto, _ := ob["protocol"].(string)
	addr := AddressOf(ob)
	finalName := strings.TrimSpace(name)
	if finalName == "" {
		finalName = proto + "-" + addr
	}
	var existing []models.Outbound
	db.Find(&existing)
	allActive := len(existing) > 0
	for _, o := range existing {
		if !o.IsActive {
			allActive = false
			break
		}
	}
	joins := finalName == ActiveGroupName(db) || allActive
	cfgJSON, _ := json.Marshal(ob)
	row := &models.Outbound{
		Name:       finalName,
		Protocol:   proto,
		Address:    addr,
		ConfigJSON: string(cfgJSON),
		IsActive:   joins,
	}
	db.Create(row)
	return row, nil
}

// OutboundUpdate edits name and/or config; returns (row, configChanged).
func OutboundUpdate(db *gorm.DB, id int, name, config *string) (*models.Outbound, bool, error) {
	var row models.Outbound
	if err := db.First(&row, id).Error; err != nil {
		return nil, false, nil
	}
	changed := false
	if config != nil && strings.TrimSpace(*config) != "" {
		ob, err := ParseOutbound(*config)
		if err != nil {
			return nil, false, err
		}
		row.Protocol, _ = ob["protocol"].(string)
		row.Address = AddressOf(ob)
		cfgJSON, _ := json.Marshal(ob)
		row.ConfigJSON = string(cfgJSON)
		row.EgressIP = ""
		row.CountryCode = ""
		row.CountryName = ""
		changed = true
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		row.Name = strings.TrimSpace(*name)
	}
	db.Save(&row)
	return &row, changed, nil
}

// OutboundActivate activates the whole same-name group of the given outbound.
func OutboundActivate(db *gorm.DB, id int) *models.Outbound {
	var row models.Outbound
	if err := db.First(&row, id).Error; err != nil {
		return nil
	}
	db.Model(&models.Outbound{}).Where("1 = 1").Update("is_active", false)
	db.Model(&models.Outbound{}).Where("name = ?", row.Name).Update("is_active", true)
	db.First(&row, id)
	return &row
}

// OutboundActivateAll activates every outbound (one round-robin pool).
func OutboundActivateAll(db *gorm.DB) int64 {
	var n int64
	db.Model(&models.Outbound{}).Count(&n)
	db.Model(&models.Outbound{}).Where("1 = 1").Update("is_active", true)
	return n
}

// OutboundSetDirect deactivates all outbounds (freedom egress).
func OutboundSetDirect(db *gorm.DB) {
	db.Model(&models.Outbound{}).Where("1 = 1").Update("is_active", false)
}

// OutboundDelete removes an outbound; returns false if not found.
func OutboundDelete(db *gorm.DB, id int) bool {
	res := db.Delete(&models.Outbound{}, id)
	return res.RowsAffected > 0
}
