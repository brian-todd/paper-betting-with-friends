package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// RatingSource names the model a rating came from.
//
// The three are not interchangeable and are never averaged: SP+ and FPI are
// points above an average team, CORE is opponent-relative efficiency on its own
// scale. The page shows them side by side and lets the reader compare.
type RatingSource string

const (
	RatingSourceSP   RatingSource = "sp+"
	RatingSourceFPI  RatingSource = "fpi"
	RatingSourceCORE RatingSource = "core"
)

// TeamRating is one model's current rating of one team for one season.
//
// There is one row per (team, season, source) and it is overwritten in place.
// These numbers are decision support for choosing a bet, so only the current
// value matters; nothing reconstructs what a rating was before a game that has
// since been played.
type TeamRating struct {
	ID     uuid.UUID    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TeamID uuid.UUID    `gorm:"type:uuid;not null;uniqueIndex:idx_team_ratings_team_season_source"`
	Season int          `gorm:"not null;uniqueIndex:idx_team_ratings_team_season_source"`
	Source RatingSource `gorm:"type:varchar(16);not null;uniqueIndex:idx_team_ratings_team_season_source"`

	// FetchedAt drives the panel's "as of" line and nothing else. It is not a
	// version key and no query selects on it.
	FetchedAt time.Time `gorm:"not null"`

	// Overall is the headline rating. Offense, Defense and SpecialTeams are the
	// unit ratings.
	//
	// All four are on the *source's own scale* and none of them is comparable
	// across sources -- SP+ offense is points scored against an average defense
	// (40.0 is elite) and its defense is points allowed (10.1 is elite, so lower
	// is better), CORE is opponent-relative efficiency either side of zero, and
	// FPI reports percentiles where higher is better for both units. Anything
	// that renders these has to label the source, and nothing may difference
	// two of them.
	Overall      decimal.Decimal  `gorm:"type:decimal(6,3);not null"`
	Offense      *decimal.Decimal `gorm:"type:decimal(6,3)"`
	Defense      *decimal.Decimal `gorm:"type:decimal(6,3)"`
	SpecialTeams *decimal.Decimal `gorm:"type:decimal(6,3)"`

	OverallRank *int
	OffenseRank *int
	DefenseRank *int

	// StrengthOfSchedule and StrengthOfRecord are FPI's resume ranks. They are
	// ranks, not ratings, and no other source supplies them.
	StrengthOfSchedule *int
	StrengthOfRecord   *int

	// ThroughWeek and ModelVersion are only ever set for CORE, which is the one
	// source that says what it has seen. Left nil elsewhere rather than derived:
	// a guessed week would be indistinguishable from a reported one.
	ThroughWeek  *int
	ModelVersion *string

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Team *Team `gorm:"foreignKey:TeamID"`
}

// BeforeCreate sets the UUID before creating a new team rating.
func (r *TeamRating) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}

// OverallText renders the headline rating for display.
//
// The column is decimal(6,3) because FPI publishes three places, but a round
// trip through it brings back "14.200" where the provider said "14.2". One
// decimal place is what a reader wants beside a point spread; the extra
// precision exists so nothing is lost on the way in, not to be shown.
func (r TeamRating) OverallText() string {
	return r.Overall.Round(1).String()
}

// Label is the source's display name for the comparison panel.
func (s RatingSource) Label() string {
	switch s {
	case RatingSourceSP:
		return "SP+"
	case RatingSourceFPI:
		return "FPI"
	case RatingSourceCORE:
		return "CORE"
	default:
		return string(s)
	}
}
