package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Sport type constants.
const (
	SportFootball   = "football"
	SportBasketball = "basketball"
)

// GameStatus represents the current state of a game.
type GameStatus string

const (
	GameStatusScheduled  GameStatus = "scheduled"
	GameStatusInProgress GameStatus = "in_progress"
	GameStatusFinal      GameStatus = "final"
	GameStatusPostponed  GameStatus = "postponed"
	GameStatusCancelled  GameStatus = "cancelled"
)

// Game represents a college sports game between two teams.
type Game struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ExternalID     *int64     `gorm:"uniqueIndex:idx_games_external_id_sport"`
	Sport          string     `gorm:"type:varchar(20);not null;default:'football';uniqueIndex:idx_games_external_id_sport"`
	HomeTeamID     uuid.UUID  `gorm:"type:uuid;not null;index"`
	AwayTeamID     uuid.UUID  `gorm:"type:uuid;not null;index"`
	VenueID        uuid.UUID  `gorm:"type:uuid;not null;index"`
	WeekID         *uuid.UUID `gorm:"type:uuid;index"`
	Season         int        `gorm:"not null"`
	SeasonType     string     `gorm:"type:varchar(20);default:'regular'"`
	Tournament     *string    `gorm:"type:varchar(255)"`
	HomeSeed       *int       `gorm:"type:integer"`
	AwaySeed       *int       `gorm:"type:integer"`
	ScheduledAt    time.Time  `gorm:"not null"`
	Status         GameStatus `gorm:"type:varchar(50);default:'scheduled'"`
	NeutralSite    bool       `gorm:"default:false"`
	ConferenceGame bool       `gorm:"default:false"`
	Completed      bool       `gorm:"default:false"`
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// Relationships.
	HomeTeam Team        `gorm:"foreignKey:HomeTeamID"`
	AwayTeam Team        `gorm:"foreignKey:AwayTeamID"`
	Venue    Venue       `gorm:"foreignKey:VenueID"`
	Week     *Week       `gorm:"foreignKey:WeekID"`
	Result   *GameResult `gorm:"foreignKey:GameID"`
}

// BeforeCreate sets the UUID before creating a new game.
func (g *Game) BeforeCreate(tx *gorm.DB) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	return nil
}
