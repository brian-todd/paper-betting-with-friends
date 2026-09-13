package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// TeamAdvancedStats is a team's season efficiency, offense and defense.
//
// These are not a fourth rating. SP+ already prices all of it, and the panel's
// job here is to say why a team is rated where it is -- whether a good rating
// comes from moving the ball consistently or from a handful of long plays.
// Nothing derives a number from these or compares them with a rating.
//
// Every figure is a pointer. A team that has not run the ball has no line
// yards, and a zero would read as the worst in the country rather than as an
// absence.
type TeamAdvancedStats struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TeamID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_team_advanced_stats_team_season"`
	Season int       `gorm:"not null;uniqueIndex:idx_team_advanced_stats_team_season"`

	// FetchedAt drives the panel's "as of" line. See TeamRating.FetchedAt.
	FetchedAt time.Time `gorm:"not null"`

	OffensePlays  *int
	OffenseDrives *int
	DefensePlays  *int
	DefenseDrives *int

	OffensePPA                  *decimal.Decimal `gorm:"column:offense_ppa;type:decimal(6,3)"`
	OffenseSuccessRate          *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffenseExplosiveness        *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffenseLineYards            *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffensePointsPerOpportunity *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffenseHavoc                *decimal.Decimal `gorm:"type:decimal(6,3)"`

	DefensePPA                  *decimal.Decimal `gorm:"column:defense_ppa;type:decimal(6,3)"`
	DefenseSuccessRate          *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseExplosiveness        *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseLineYards            *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefensePointsPerOpportunity *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseHavoc                *decimal.Decimal `gorm:"type:decimal(6,3)"`

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Team *Team `gorm:"foreignKey:TeamID"`
}

// minEfficiencyPlays is how many plays a side must have taken part in before
// its rates are worth reading.
//
// Deliberately low, because the two units do not see comparable volume. Through
// week 2 of 2026 the thinnest FBS offense had run 41 plays and the thinnest
// defense had faced 37 -- both of them teams one game into a season, and
// Arkansas's defense saw 45 snaps in an opener where its offense ran 84. A
// threshold set at "roughly one game" of offensive volume therefore reads as a
// data-quality rule and acts as a rule about time of possession, quietly
// dropping the panel for a fifth of the matchups on the board.
//
// What this actually guards is a row with no football behind it at all: a
// fragment of a week-1 game, or a partial response. The sample that survives is
// still small in September, which is what the panel's footnote is for.
const minEfficiencyPlays = 30

// OffenseReliable reports whether the offensive figures rest on enough plays to
// be worth showing.
func (s *TeamAdvancedStats) OffenseReliable() bool {
	return s != nil && s.OffensePlays != nil && *s.OffensePlays >= minEfficiencyPlays
}

// DefenseReliable reports whether the defensive figures rest on enough plays to
// be worth showing.
func (s *TeamAdvancedStats) DefenseReliable() bool {
	return s != nil && s.DefensePlays != nil && *s.DefensePlays >= minEfficiencyPlays
}

// BeforeCreate sets the UUID before creating new advanced stats.
func (s *TeamAdvancedStats) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
