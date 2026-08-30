package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// BetStatus represents the current state of a bet.
type BetStatus string

const (
	BetStatusPending BetStatus = "pending"
	BetStatusWon     BetStatus = "won"
	BetStatusLost    BetStatus = "lost"
	BetStatusPush    BetStatus = "push"
	BetStatusVoid    BetStatus = "void"
)

// MoneyLinePick represents the pick for a money line bet.
type MoneyLinePick string

const (
	MoneyLinePickHome MoneyLinePick = "home"
	MoneyLinePickAway MoneyLinePick = "away"
)

// SpreadPick represents the pick for a spread bet.
type SpreadPick string

const (
	SpreadPickHome SpreadPick = "home"
	SpreadPickAway SpreadPick = "away"
)

// OverUnderPick represents the pick for an over/under bet.
type OverUnderPick string

const (
	OverUnderPickOver  OverUnderPick = "over"
	OverUnderPickUnder OverUnderPick = "under"
)

// PayoutForOdds returns the total payout (stake + profit) for a winning bet
// at the given American odds. Settlement and any display that reports winnings
// must both go through this so the numbers agree to the cent.
func PayoutForOdds(stake, odds decimal.Decimal) decimal.Decimal {
	hundred := decimal.NewFromInt(100)
	var profit decimal.Decimal

	if odds.IsPositive() {
		// Positive odds (+150): profit = stake * (odds / 100)
		profit = stake.Mul(odds).Div(hundred)
	} else {
		// Negative odds (-150): profit = stake * (100 / abs(odds))
		profit = stake.Mul(hundred).Div(odds.Abs())
	}

	return stake.Add(profit)
}

// MoneyLineBet represents a money line bet placed by a user.
type MoneyLineBet struct {
	ID              uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID          uuid.UUID       `gorm:"type:uuid;not null;index"`
	LeagueID        uuid.UUID       `gorm:"type:uuid;not null;index"`
	GameID          uuid.UUID       `gorm:"type:uuid;not null;index"`
	MoneyLineOddsID uuid.UUID       `gorm:"type:uuid;not null;index"`
	Pick            MoneyLinePick   `gorm:"type:varchar(10);not null"`
	OddsSnapshot    decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	Stake           decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	Status          BetStatus       `gorm:"type:varchar(50);default:'pending';index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Relationships.
	User          User          `gorm:"foreignKey:UserID"`
	League        League        `gorm:"foreignKey:LeagueID"`
	Game          Game          `gorm:"foreignKey:GameID"`
	MoneyLineOdds MoneyLineOdds `gorm:"foreignKey:MoneyLineOddsID"`
}

// BeforeCreate sets the UUID before creating a new money line bet.
func (b *MoneyLineBet) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// SpreadBet represents a point spread bet placed by a user.
type SpreadBet struct {
	ID             uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID         uuid.UUID       `gorm:"type:uuid;not null;index"`
	LeagueID       uuid.UUID       `gorm:"type:uuid;not null;index"`
	GameID         uuid.UUID       `gorm:"type:uuid;not null;index"`
	SpreadOddsID   uuid.UUID       `gorm:"type:uuid;not null;index"`
	Pick           SpreadPick      `gorm:"type:varchar(10);not null"`
	SpreadSnapshot decimal.Decimal `gorm:"type:decimal(5,1);not null"`
	OddsSnapshot   decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	Stake          decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	Status         BetStatus       `gorm:"type:varchar(50);default:'pending';index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// Relationships.
	User       User       `gorm:"foreignKey:UserID"`
	League     League     `gorm:"foreignKey:LeagueID"`
	Game       Game       `gorm:"foreignKey:GameID"`
	SpreadOdds SpreadOdds `gorm:"foreignKey:SpreadOddsID"`
}

// BeforeCreate sets the UUID before creating a new spread bet.
func (b *SpreadBet) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// OverUnderBet represents an over/under (total) bet placed by a user.
type OverUnderBet struct {
	ID              uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID          uuid.UUID       `gorm:"type:uuid;not null;index"`
	LeagueID        uuid.UUID       `gorm:"type:uuid;not null;index"`
	GameID          uuid.UUID       `gorm:"type:uuid;not null;index"`
	OverUnderOddsID uuid.UUID       `gorm:"type:uuid;not null;index"`
	Pick            OverUnderPick   `gorm:"type:varchar(10);not null"`
	TotalSnapshot   decimal.Decimal `gorm:"type:decimal(5,1);not null"`
	OddsSnapshot    decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	Stake           decimal.Decimal `gorm:"type:decimal(10,2);not null"`
	Status          BetStatus       `gorm:"type:varchar(50);default:'pending';index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Relationships.
	User          User          `gorm:"foreignKey:UserID"`
	League        League        `gorm:"foreignKey:LeagueID"`
	Game          Game          `gorm:"foreignKey:GameID"`
	OverUnderOdds OverUnderOdds `gorm:"foreignKey:OverUnderOddsID"`
}

// BeforeCreate sets the UUID before creating a new over/under bet.
func (b *OverUnderBet) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}
