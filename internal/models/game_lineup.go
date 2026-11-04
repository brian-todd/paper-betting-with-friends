package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LineupAthlete is one player in a five-man unit.
type LineupAthlete struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// LineupAthletes is stored as a JSON array.
type LineupAthletes []LineupAthlete

// Value implements the driver.Valuer interface for database storage.
func (a LineupAthletes) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

// Scan implements the sql.Scanner interface for database retrieval.
func (a *LineupAthletes) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*a = nil
		return nil
	case []byte:
		return json.Unmarshal(v, a)
	case string:
		return json.Unmarshal([]byte(v), a)
	default:
		return errors.New("failed to scan LineupAthletes: expected JSON")
	}
}

// LineupTotals are a unit's numbers over one game or many. They are sums because
// a rating over several games has to be worked out from the totals: an average of
// each game's rating would weigh a one-minute stint like a twenty-minute one.
type LineupTotals struct {
	Athletes           LineupAthletes `gorm:"type:jsonb"`
	Seconds            int
	PointsFor          int
	PossessionsFor     int
	PointsAgainst      int
	PossessionsAgainst int
	For                LineupBox `gorm:"embedded;embeddedPrefix:for_"`
	Against            LineupBox `gorm:"embedded;embeddedPrefix:against_"`
}

// LineupBox is what one side did while a unit was on the floor. They are counts,
// so games can be summed and rates worked out from the sums.
type LineupBox struct {
	FGMade         int `gorm:"column:fg_made;not null"`
	FGAttempted    int `gorm:"column:fg_attempted;not null"`
	ThreeMade      int `gorm:"not null"`
	ThreeAttempted int `gorm:"not null"`
	FTMade         int `gorm:"column:ft_made;not null"`
	FTAttempted    int `gorm:"column:ft_attempted;not null"`
	Turnovers      int `gorm:"not null"`
	OffRebounds    int `gorm:"not null"`
	DefRebounds    int `gorm:"not null"`
}

// EffectiveFGPct is shooting with a three counted as one and a half makes, as a
// percentage. It matches the feed's own on all 356 sides of the captured games. The
// second return is false when there are no attempts to divide by.
func (b LineupBox) EffectiveFGPct() (float64, bool) {
	if b.FGAttempted <= 0 {
		return 0, false
	}
	return (float64(b.FGMade) + 0.5*float64(b.ThreeMade)) * 100 / float64(b.FGAttempted), true
}

// ThreeRate is the share of field goal attempts that were threes, as a percentage.
func (b LineupBox) ThreeRate() (float64, bool) {
	if b.FGAttempted <= 0 {
		return 0, false
	}
	return float64(b.ThreeAttempted) * 100 / float64(b.FGAttempted), true
}

// TurnoverPct is turnovers per 100 of the given possessions.
func (b LineupBox) TurnoverPct(possessions int) (float64, bool) {
	if possessions <= 0 {
		return 0, false
	}
	return float64(b.Turnovers) * 100 / float64(possessions), true
}

// OffenseRating is points scored per 100 possessions.
func (t LineupTotals) OffenseRating() (float64, bool) {
	if t.PossessionsFor <= 0 {
		return 0, false
	}
	return float64(t.PointsFor) * 100 / float64(t.PossessionsFor), true
}

// DefenseRating is points allowed per 100 possessions.
func (t LineupTotals) DefenseRating() (float64, bool) {
	if t.PossessionsAgainst <= 0 {
		return 0, false
	}
	return float64(t.PointsAgainst) * 100 / float64(t.PossessionsAgainst), true
}

// NetRating is points scored minus points allowed per 100 possessions. The second
// return is false when either side has no possessions to divide by.
func (t LineupTotals) NetRating() (float64, bool) {
	offense, ok := t.OffenseRating()
	if !ok {
		return 0, false
	}
	defense, ok := t.DefenseRating()
	if !ok {
		return 0, false
	}
	return offense - defense, true
}

// Players names the unit's five players.
func (t LineupTotals) Players() string {
	names := make([]string, len(t.Athletes))
	for i, athlete := range t.Athletes {
		names[i] = athlete.Name
	}
	return strings.Join(names, ", ")
}

// GameLineup is one five-man unit's totals in one game.
//
// This is what a unit did in a game already played, derived from play-by-play. It
// says nothing about who starts or who is available tonight, and the feed's coverage
// of a game can be partial.
type GameLineup struct {
	ID       uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID   uuid.UUID      `gorm:"type:uuid;not null"`
	TeamID   uuid.UUID      `gorm:"type:uuid;not null"`
	IDHash   string         `gorm:"column:id_hash;type:varchar(128);not null"`
	Athletes LineupAthletes `gorm:"type:jsonb;not null"`

	Seconds            int `gorm:"not null"`
	PointsFor          int `gorm:"not null"`
	PossessionsFor     int `gorm:"not null"`
	PointsAgainst      int `gorm:"not null"`
	PossessionsAgainst int `gorm:"not null"`

	For     LineupBox `gorm:"embedded;embeddedPrefix:for_"`
	Against LineupBox `gorm:"embedded;embeddedPrefix:against_"`

	CreatedAt time.Time
}

// BeforeCreate sets the UUID before creating a new game lineup.
func (l *GameLineup) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// GameLineupFetch records the asking for one game's lineups, whatever came back.
// FoundAt stays nil until a response holds any.
type GameLineupFetch struct {
	GameID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Attempts      int       `gorm:"not null"`
	LastAttemptAt time.Time `gorm:"not null"`
	FoundAt       *time.Time
}
