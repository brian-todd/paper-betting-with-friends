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
}

// NetRating is points scored minus points allowed per 100 possessions. The second
// return is false when either side has no possessions to divide by.
func (t LineupTotals) NetRating() (float64, bool) {
	if t.PossessionsFor <= 0 || t.PossessionsAgainst <= 0 {
		return 0, false
	}
	offense := float64(t.PointsFor) * 100 / float64(t.PossessionsFor)
	defense := float64(t.PointsAgainst) * 100 / float64(t.PossessionsAgainst)
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
