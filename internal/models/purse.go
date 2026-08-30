package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Purse represents a user's balance within a specific league.
type Purse struct {
	UserID    uuid.UUID       `gorm:"type:uuid;primaryKey"`
	LeagueID  uuid.UUID       `gorm:"type:uuid;primaryKey"`
	Balance   decimal.Decimal `gorm:"type:decimal(12,2);not null"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	User   User   `gorm:"foreignKey:UserID"`
	League League `gorm:"foreignKey:LeagueID"`
}
