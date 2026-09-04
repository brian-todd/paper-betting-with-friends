package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Poll names, matching the strings the CFB Data API's /rankings endpoint
// returns verbatim. RankingRepository.EffectiveRanks matches against these.
const (
	PollCFP     = "Playoff Committee Rankings"
	PollAP      = "AP Top 25"
	PollCoaches = "Coaches Poll"
)

// TeamRanking is one team's position in one poll for one week.
type TeamRanking struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	WeekID          uuid.UUID `gorm:"type:uuid;not null;index:idx_team_rankings_week_poll"`
	TeamID          uuid.UUID `gorm:"type:uuid;not null"`
	Poll            string    `gorm:"type:varchar(64);not null;index:idx_team_rankings_week_poll"`
	Rank            int       `gorm:"not null"`
	FirstPlaceVotes *int
	Points          *int
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Relationships.
	Week *Week `gorm:"foreignKey:WeekID"`
	Team *Team `gorm:"foreignKey:TeamID"`
}

// BeforeCreate sets the UUID before creating a new team ranking.
func (r *TeamRanking) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
