package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// TeamSeasonStats is a basketball team's season so far, as /stats/team/season
// reports it.
//
// One row per (team, season), overwritten in place. Each pair is the same
// measurement from both ends of the floor: Offense is what the team did and Defense
// is what its opponents did against it. Every percentage is on a 0-100 scale.
type TeamSeasonStats struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TeamID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:team_season_stats_team_id_season_key"`
	Season int       `gorm:"not null;uniqueIndex:team_season_stats_team_id_season_key"`

	// FetchedAt drives the panel's "as of" line. See TeamRating.FetchedAt.
	FetchedAt time.Time `gorm:"not null"`

	Games int              `gorm:"not null"`
	Pace  *decimal.Decimal `gorm:"type:decimal(6,3)"`

	OffenseRating *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseRating *decimal.Decimal `gorm:"type:decimal(6,3)"`

	OffenseEffectiveFGPct *decimal.Decimal `gorm:"column:offense_effective_fg_pct;type:decimal(6,3)"`
	DefenseEffectiveFGPct *decimal.Decimal `gorm:"column:defense_effective_fg_pct;type:decimal(6,3)"`
	OffenseTurnoverPct    *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseTurnoverPct    *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffenseReboundPct     *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseReboundPct     *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffenseFreeThrowRate  *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseFreeThrowRate  *decimal.Decimal `gorm:"type:decimal(6,3)"`
	OffenseThreePct       *decimal.Decimal `gorm:"type:decimal(6,3)"`
	DefenseThreePct       *decimal.Decimal `gorm:"type:decimal(6,3)"`

	// Box is the team's season totals, which the game page shows per game.
	Box TeamBox `gorm:"embedded"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName keeps GORM from pluralizing a name that is already plural.
func (TeamSeasonStats) TableName() string { return "team_season_stats" }

// HasGames reports whether there is a season to describe. It takes nil, because the
// panel asks of teams the feed has never reported.
func (s *TeamSeasonStats) HasGames() bool {
	return s != nil && s.Games > 0
}

// BeforeCreate sets the UUID before creating new season stats.
func (s *TeamSeasonStats) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
