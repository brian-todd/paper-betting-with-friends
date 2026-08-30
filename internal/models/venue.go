package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Venue represents a stadium or arena that can host games.
type Venue struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ExternalID *int64    `gorm:"uniqueIndex:idx_venues_external_id_sport"`
	Sport      string    `gorm:"type:varchar(20);not null;default:'football';uniqueIndex:idx_venues_external_id_sport"`
	Name       string    `gorm:"type:varchar(255);not null"`
	City       string    `gorm:"type:varchar(100);not null"`
	State      string    `gorm:"type:varchar(50);not null"`
	Capacity   int       `gorm:"not null"`
	Timezone   *string   `gorm:"type:varchar(50)"`
	Dome       bool      `gorm:"default:false"`
	Grass      bool      `gorm:"default:true"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// BeforeCreate sets the UUID before creating a new venue.
func (v *Venue) BeforeCreate(tx *gorm.DB) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	return nil
}
