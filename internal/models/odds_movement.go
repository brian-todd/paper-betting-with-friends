package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// OddsMarket is which of a game's three markets a line belongs to.
type OddsMarket string

const (
	OddsMarketMoneyLine OddsMarket = "moneyline"
	OddsMarketSpread    OddsMarket = "spread"
	OddsMarketTotal     OddsMarket = "total"
)

// OddsMovement is one change in one sportsbook's line for one market of a game.
//
// A series -- game, market, source -- gains a row only when a sync finds it at a
// value it did not hold before, so the line at any instant is the latest row at
// or before it. Read it as a step: a line moves in whole ticks, and a value
// between two rows is one no book offered. The move itself happened somewhere
// between the previous sync and RecordedAt.
//
// Each row holds the whole value rather than a delta, and only its market's
// columns are set; the rest are nil, which the table's CHECK enforces.
type OddsMovement struct {
	ID     uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID uuid.UUID  `gorm:"type:uuid;not null"`
	Source OddsSource `gorm:"type:varchar(50);not null"`
	Market OddsMarket `gorm:"type:varchar(20);not null"`

	HomeSpread *decimal.Decimal `gorm:"type:decimal(5,1)"`
	Total      *decimal.Decimal `gorm:"type:decimal(5,1)"`
	HomeOdds   *decimal.Decimal `gorm:"type:decimal(10,2)"`
	AwayOdds   *decimal.Decimal `gorm:"type:decimal(10,2)"`
	OverOdds   *decimal.Decimal `gorm:"type:decimal(10,2)"`
	UnderOdds  *decimal.Decimal `gorm:"type:decimal(10,2)"`

	// RecordedAt is the sync's clock when it saw the new value, not the
	// database's, so a replayed fixture dates its movements from the recording.
	RecordedAt time.Time `gorm:"not null"`
}

// BeforeCreate sets the UUID before creating a new odds movement.
func (m *OddsMovement) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// MoneyLineMovement is the movement a sync records for a money line it wrote.
func MoneyLineMovement(odds MoneyLineOdds, at time.Time) OddsMovement {
	return OddsMovement{
		GameID:     odds.GameID,
		Source:     odds.Source,
		Market:     OddsMarketMoneyLine,
		HomeOdds:   &odds.HomeOdds,
		AwayOdds:   &odds.AwayOdds,
		RecordedAt: at,
	}
}

// SpreadMovement is the movement a sync records for a spread it wrote. The away
// spread is left out: it is the home spread seen from the other side.
func SpreadMovement(odds SpreadOdds, at time.Time) OddsMovement {
	return OddsMovement{
		GameID:     odds.GameID,
		Source:     odds.Source,
		Market:     OddsMarketSpread,
		HomeSpread: &odds.HomeSpread,
		HomeOdds:   &odds.HomeOdds,
		AwayOdds:   &odds.AwayOdds,
		RecordedAt: at,
	}
}

// OverUnderMovement is the movement a sync records for a total it wrote.
func OverUnderMovement(odds OverUnderOdds, at time.Time) OddsMovement {
	return OddsMovement{
		GameID:     odds.GameID,
		Source:     odds.Source,
		Market:     OddsMarketTotal,
		Total:      &odds.Total,
		OverOdds:   &odds.OverOdds,
		UnderOdds:  &odds.UnderOdds,
		RecordedAt: at,
	}
}
