package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PollRank is one basketball team's position in one poll on one date.
//
// Football's TeamRanking is keyed by week and basketball has none, so a game
// finds its rank by date: the latest poll on or before tip-off. Only ranked teams
// are stored, since a row for a team merely receiving votes has no position.
type PollRank struct {
	ID       uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TeamID   uuid.UUID `gorm:"type:uuid;not null"`
	Season   int       `gorm:"not null"`
	Poll     string    `gorm:"type:varchar(64);not null"`
	PollDate time.Time `gorm:"not null"`
	Rank     int       `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// BeforeCreate sets the UUID before creating a new poll rank.
func (r *PollRank) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
