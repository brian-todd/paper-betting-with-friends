package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Sport type constants.
const (
	SportFootball   = "football"
	SportBasketball = "basketball"
)

// ParseSport reads a sport from a form. Absent means football, which is what a
// league was before leagues had a sport; the second return is false for anything
// else that is not a sport.
func ParseSport(value string) (string, bool) {
	switch value {
	case "", SportFootball:
		return SportFootball, true
	case SportBasketball:
		return SportBasketball, true
	default:
		return "", false
	}
}

// BasketballSeasonLabel names a basketball season for display. The feed labels a
// season by the year it ends in, so season 2027 is "2026-27".
func BasketballSeasonLabel(season int) string {
	return fmt.Sprintf("%d-%02d", season-1, season%100)
}

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

	// HomePregameElo and AwayPregameElo are the /games feed's Elo for each side
	// going into this game. They are reported for unplayed games too, which is
	// what makes them the one rating on this page that costs no extra request.
	// Nil for basketball, and for any game synced before they were stored.
	HomePregameElo *int
	AwayPregameElo *int

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	HomeTeam Team        `gorm:"foreignKey:HomeTeamID"`
	AwayTeam Team        `gorm:"foreignKey:AwayTeamID"`
	Venue    Venue       `gorm:"foreignKey:VenueID"`
	Week     *Week       `gorm:"foreignKey:WeekID"`
	Result   *GameResult `gorm:"foreignKey:GameID"`
	// LiveState is the scoreboard feed's view of the game -- clock, situation,
	// broadcast, weather. Nil for any game the scoreboard has not covered,
	// which is every basketball game and every football game outside the
	// classifications and week the scoreboard sync asks for.
	LiveState *GameLiveState `gorm:"foreignKey:GameID"`
}

// Path is the URL of the game's page. A game with no sport set, which only a
// hand-built struct can be, gets the lookup route, which finds the right one.
func (g Game) Path() string {
	if g.Sport == "" {
		return "/games/" + g.ID.String()
	}
	return "/games/" + g.Sport + "/" + g.ID.String()
}

// BeforeCreate sets the UUID before creating a new game.
func (g *Game) BeforeCreate(tx *gorm.DB) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	return nil
}
