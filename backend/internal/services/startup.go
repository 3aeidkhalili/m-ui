package services

import (
	"log"

	"gorm.io/gorm"

	"multivpn/internal/models"
	"multivpn/internal/security"
)

// BackfillTokens generates a subscription token for any user missing one
// (upgrades installs created before sub_token existed).
func BackfillTokens(db *gorm.DB) {
	var users []models.User
	db.Where("sub_token IS NULL OR sub_token = ''").Find(&users)
	for i := range users {
		users[i].SubToken = security.RandomToken(32)
		db.Save(&users[i])
	}
	if len(users) > 0 {
		log.Printf("backfilled sub_token for %d users", len(users))
	}
}
