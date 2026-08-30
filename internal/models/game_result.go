package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// IntSlice is a custom type for storing []int as JSONB in PostgreSQL.
type IntSlice []int

// Value implements the driver.Valuer interface for database storage.
func (s IntSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface for database retrieval.
func (s *IntSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan IntSlice: expected []byte")
	}

	return json.Unmarshal(bytes, s)
}

// GameResult stores the score of a game. A row exists as soon as the provider
// reports a score, which need not wait for the game to end, so the presence of
// a GameResult does not mean the game is over -- see FinalizedAt.
type GameResult struct {
	ID              uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID          uuid.UUID        `gorm:"type:uuid;not null;uniqueIndex"`
	HomeScore       int              `gorm:"not null"`
	AwayScore       int              `gorm:"not null"`
	HomeLineScores  IntSlice         `gorm:"type:jsonb"` // Quarter-by-quarter scores.
	AwayLineScores  IntSlice         `gorm:"type:jsonb"` // Quarter-by-quarter scores.
	ExcitementIndex *decimal.Decimal `gorm:"type:decimal(5,2)"`
	// FinalizedAt is nil while the score is still moving and set once the
	// provider reports the game complete. Bet settlement keys off it, so
	// nothing may set it for a game that is still in progress.
	FinalizedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Relationships.
	Game Game `gorm:"foreignKey:GameID"`
}

// IsFinal reports whether the score is settled rather than still in play.
func (r *GameResult) IsFinal() bool {
	return r != nil && r.FinalizedAt != nil
}

// BeforeCreate sets the UUID before creating a new game result.
func (r *GameResult) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
