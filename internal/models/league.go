package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// League represents a betting league that users can join.
type League struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name string    `gorm:"type:varchar(255);not null"`
	// Sport is fixed at creation. A league's bets, purses and standings are all
	// in this one sport, so a member of a football league and a basketball league
	// holds two separate balances.
	Sport           string          `gorm:"type:varchar(20);not null;default:'football'"`
	CreatedBy       uuid.UUID       `gorm:"type:uuid;not null"`
	IsPublic        *bool           `gorm:"default:true"`
	InviteCode      string          `gorm:"type:varchar(16);uniqueIndex"`
	StartingBalance decimal.Decimal `gorm:"type:decimal(12,2);default:1000"`
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Relationships.
	Creator User           `gorm:"foreignKey:CreatedBy"`
	Members []LeagueMember `gorm:"foreignKey:LeagueID"`
}

// SportLabel names the league's sport for display.
func (l League) SportLabel() string {
	if l.Sport == SportBasketball {
		return "Basketball"
	}
	return "Football"
}

// BeforeCreate sets the UUID and invite code before creating a new league.
func (l *League) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	if l.InviteCode == "" {
		l.InviteCode = generateInviteCode()
	}
	return nil
}

// generateInviteCode creates a random 8-character hex code.
func generateInviteCode() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// LeagueMember represents a user's membership in a league.
type LeagueMember struct {
	LeagueID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Role      string    `gorm:"type:varchar(50);default:'member'"`
	CreatedAt time.Time

	// Relationships.
	League League `gorm:"foreignKey:LeagueID"`
	User   User   `gorm:"foreignKey:UserID"`
}
