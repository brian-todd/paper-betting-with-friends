package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// OddsSource represents where the odds came from.
type OddsSource string

const (
	OddsSourceDraftKings OddsSource = "draftkings"
	OddsSourceFanDuel    OddsSource = "fanduel"
	OddsSourceBetMGM     OddsSource = "betmgm"
	OddsSourceCaesars    OddsSource = "caesars"
	OddsSourceESPN       OddsSource = "espn"
	OddsSourceBovada     OddsSource = "bovada"
	OddsSourceCustom     OddsSource = "custom"
)

// MoneyLineOdds represents money line odds for a game.
type MoneyLineOdds struct {
	ID        uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID    uuid.UUID       `gorm:"type:uuid;not null;index"`
	Source    OddsSource      `gorm:"type:varchar(50);not null"`
	HomeOdds  decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	AwayOdds  decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Game Game `gorm:"foreignKey:GameID"`
}

// BeforeCreate sets the UUID before creating new money line odds.
func (m *MoneyLineOdds) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// SpreadOdds represents point spread odds for a game.
type SpreadOdds struct {
	ID         uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID     uuid.UUID       `gorm:"type:uuid;not null;index"`
	Source     OddsSource      `gorm:"type:varchar(50);not null"`
	HomeSpread decimal.Decimal `gorm:"type:decimal(5,1);not null"`
	AwaySpread decimal.Decimal `gorm:"type:decimal(5,1);not null"`
	HomeOdds   decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	AwayOdds   decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// Relationships.
	Game Game `gorm:"foreignKey:GameID"`
}

// BeforeCreate sets the UUID before creating new spread odds.
func (s *SpreadOdds) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// OverUnderOdds represents over/under (total) odds for a game.
type OverUnderOdds struct {
	ID        uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID    uuid.UUID       `gorm:"type:uuid;not null;index"`
	Source    OddsSource      `gorm:"type:varchar(50);not null"`
	Total     decimal.Decimal `gorm:"type:decimal(5,1);not null"`
	OverOdds  decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	UnderOdds decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Game Game `gorm:"foreignKey:GameID"`
}

// BeforeCreate sets the UUID before creating new over/under odds.
func (o *OverUnderOdds) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}
