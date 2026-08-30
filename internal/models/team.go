package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Team represents a college sports team.
type Team struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ExternalID     *int64    `gorm:"uniqueIndex:idx_teams_external_id_sport"`
	Sport          string    `gorm:"type:varchar(20);not null;default:'football';uniqueIndex:idx_teams_external_id_sport"`
	Name           string    `gorm:"type:varchar(255);not null"`
	Abbreviation   string    `gorm:"type:varchar(10);not null;uniqueIndex:idx_teams_abbreviation_sport"`
	Mascot         *string   `gorm:"type:varchar(100)"`
	Conference     string    `gorm:"type:varchar(100);not null"`
	Classification *string   `gorm:"type:varchar(20)"` // e.g., fbs, fcs.
	HomeVenueID    uuid.UUID `gorm:"type:uuid;not null;index"`
	LogoURL        *string   `gorm:"type:text"`
	PrimaryColor   *string   `gorm:"type:varchar(7)"` // Hex color code.
	SecondaryColor *string   `gorm:"type:varchar(7)"` // Hex color code.
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// Relationships.
	HomeVenue Venue `gorm:"foreignKey:HomeVenueID"`
}

// BeforeCreate sets the UUID before creating a new team.
func (t *Team) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}
