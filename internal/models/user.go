package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents a registered user in the system.
type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Username     string    `gorm:"type:varchar(50);uniqueIndex;not null"`
	PasswordHash string    `gorm:"type:varchar(255);not null"`
	IsAdmin      bool      `gorm:"default:false"`

	// SessionVersion is carried in the session cookie and compared on every
	// request. Bumping it invalidates every session already issued for this
	// user, which is what makes a password reset actually revoke access.
	SessionVersion int `gorm:"not null;default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// BeforeCreate sets the UUID before creating a new user.
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}
