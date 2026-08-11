package services

import (
	"log"
	"time"

	"gorm.io/gorm"

	"multivpn/internal/models"
)

// keepPerUser bounds how many connection IPs are retained per user.
const keepPerUser = 60

// RecordConnection registers/updates an IP that hit a user's subscription link,
// geolocating it once (offline).
func RecordConnection(db *gorm.DB, userID int, ip string) {
	if ip == "" || ip == "unknown" || ip == "127.0.0.1" || ip == "::1" {
		return
	}
	now := time.Now().UTC()
	var row models.Connection
	err := db.Where("user_id = ? AND ip = ?", userID, ip).First(&row).Error
	if err == nil {
		row.Hits++
		row.LastSeen = now
		if row.Lat == nil {
			if g := Geo.Lookup(ip); g != nil {
				row.City, row.Country, row.CountryCode = g.City, g.Country, g.CountryCode
				row.Lat, row.Lon = &g.Lat, &g.Lon
			}
		}
		db.Save(&row)
	} else {
		c := models.Connection{
			UserID: userID, IP: ip, Hits: 1, FirstSeen: now, LastSeen: now,
		}
		if g := Geo.Lookup(ip); g != nil {
			c.City, c.Country, c.CountryCode = g.City, g.Country, g.CountryCode
			c.Lat, c.Lon = &g.Lat, &g.Lon
		}
		if err := db.Create(&c).Error; err != nil {
			log.Printf("connection record failed: %v", err)
			return
		}
		// new source IP for this user -> audit event
		var uname string
		db.Model(&models.User{}).Where("id = ?", userID).Pluck("username", &uname)
		place := c.Country
		if c.City != "" {
			place = c.City + "، " + c.Country
		}
		msg := "اتصال/بازدید جدید از IP " + ip
		if place != "" {
			msg += " (" + place + ")"
		}
		AuditLog(db, LogConnection, "info", uname, msg)
	}
	pruneConnections(db, userID)
}

func pruneConnections(db *gorm.DB, userID int) {
	var ids []int
	db.Model(&models.Connection{}).
		Where("user_id = ?", userID).
		Order("last_seen desc").
		Offset(keepPerUser).
		Pluck("id", &ids)
	if len(ids) > 0 {
		db.Where("id IN ?", ids).Delete(&models.Connection{})
	}
}

// RecentConnections returns a user's most recent connections.
func RecentConnections(db *gorm.DB, userID, limit int) []models.Connection {
	var rows []models.Connection
	db.Where("user_id = ?", userID).Order("last_seen desc").Limit(limit).Find(&rows)
	return rows
}
