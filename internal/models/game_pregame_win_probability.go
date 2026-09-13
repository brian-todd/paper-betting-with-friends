package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// GamePregameWinProbability is the provider's pre-game win probability for one
// game, with the spread it was derived from.
//
// Coverage tracks the market rather than the schedule -- the metric comes off a
// line, so it exists once somebody posts one and not before. Most games do not
// have a row until the week they are played, and a handful of marquee matchups
// have had one since August.
type GamePregameWinProbability struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`

	// FetchedAt drives the panel's "as of" line. See TeamRating.FetchedAt.
	FetchedAt time.Time `gorm:"not null"`

	// HomeWinProbability is home-relative and runs 0 to 1.
	HomeWinProbability decimal.Decimal `gorm:"type:decimal(4,3);not null"`

	// Spread is the line the probability was computed from, home-relative and
	// in the same convention as SpreadOdds.HomeSpread: negative favours the
	// home side.
	//
	// Stored even though spread_odds exists, because it is not necessarily a
	// line we hold. A probability displayed next to a spread it was not derived
	// from is worse than one displayed with no spread at all.
	Spread decimal.Decimal `gorm:"type:decimal(5,1);not null"`

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Game *Game `gorm:"foreignKey:GameID"`
}

// percentScale turns a 0-1 probability into a percentage.
var percentScale = decimal.NewFromInt(100)

// HomePercent is the home win probability as a whole-number percentage.
//
// Whole numbers because the third decimal place of a pre-game probability is
// false precision: it is derived from a spread that moves half a point at a
// time, and 76% and 76.6% are the same claim about a football game.
func (p *GamePregameWinProbability) HomePercent() int {
	if p == nil {
		return 0
	}
	return int(p.HomeWinProbability.Mul(percentScale).Round(0).IntPart())
}

// AwayPercent is the complement of HomePercent, so the two always total 100
// rather than each rounding independently to 99 or 101.
func (p *GamePregameWinProbability) AwayPercent() int {
	if p == nil {
		return 0
	}
	return 100 - p.HomePercent()
}

// BeforeCreate sets the UUID before creating a new win probability.
func (p *GamePregameWinProbability) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
