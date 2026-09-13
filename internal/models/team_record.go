package models

import (
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// WinLoss is one won-lost-tied split of a season.
type WinLoss struct {
	Games  int
	Wins   int
	Losses int
	Ties   int
}

// String renders the split as "8-1", or "8-1-1" when there are ties. Ties are
// vanishingly rare in college football but the feed still reports them, and a
// record that silently dropped one would not add up.
func (w WinLoss) String() string {
	s := strconv.Itoa(w.Wins) + "-" + strconv.Itoa(w.Losses)
	if w.Ties > 0 {
		s += "-" + strconv.Itoa(w.Ties)
	}
	return s
}

// Played reports whether the split covers any games at all. A team with none
// renders nothing rather than "0-0", which reads as a result.
func (w WinLoss) Played() bool { return w.Games > 0 }

// TeamRecord is a team's current season record.
//
// One row per (team, season), overwritten in place. /records also returns
// conference, neutral-site, regular-season and postseason splits; only the
// three with somewhere to go on the page are stored.
type TeamRecord struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TeamID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_team_records_team_season"`
	Season int       `gorm:"not null;uniqueIndex:idx_team_records_team_season"`

	// FetchedAt drives the panel's "as of" line. See TeamRating.FetchedAt.
	FetchedAt time.Time `gorm:"not null"`

	Games  int `gorm:"not null;default:0"`
	Wins   int `gorm:"not null;default:0"`
	Losses int `gorm:"not null;default:0"`
	Ties   int `gorm:"not null;default:0"`

	HomeGames  int `gorm:"not null;default:0"`
	HomeWins   int `gorm:"not null;default:0"`
	HomeLosses int `gorm:"not null;default:0"`
	HomeTies   int `gorm:"not null;default:0"`

	AwayGames  int `gorm:"not null;default:0"`
	AwayWins   int `gorm:"not null;default:0"`
	AwayLosses int `gorm:"not null;default:0"`
	AwayTies   int `gorm:"not null;default:0"`

	ExpectedWins *decimal.Decimal `gorm:"type:decimal(4,2)"`

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Team *Team `gorm:"foreignKey:TeamID"`
}

// Overall is the full-season split.
func (r *TeamRecord) Overall() WinLoss {
	if r == nil {
		return WinLoss{}
	}
	return WinLoss{Games: r.Games, Wins: r.Wins, Losses: r.Losses, Ties: r.Ties}
}

// Home is the record in home games.
func (r *TeamRecord) Home() WinLoss {
	if r == nil {
		return WinLoss{}
	}
	return WinLoss{Games: r.HomeGames, Wins: r.HomeWins, Losses: r.HomeLosses, Ties: r.HomeTies}
}

// Away is the record in away games.
func (r *TeamRecord) Away() WinLoss {
	if r == nil {
		return WinLoss{}
	}
	return WinLoss{Games: r.AwayGames, Wins: r.AwayWins, Losses: r.AwayLosses, Ties: r.AwayTies}
}

// BeforeCreate sets the UUID before creating a new team record.
func (r *TeamRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
