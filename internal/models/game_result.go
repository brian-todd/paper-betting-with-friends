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

// PeriodScore is one column of the line score: what each side scored in one
// period. A side with no entry for the period is nil rather than zero -- the
// feed reports quarters as they are played, so the fourth column of a game in
// the third quarter is unplayed, not scoreless.
type PeriodScore struct {
	Label string
	Home  *int
	Away  *int
}

// PeriodScores is the line score as columns, ready to render.
//
// sport decides both how many periods regulation has and what they are called,
// so a basketball game reports two halves rather than the four quarters it
// would otherwise be given -- with its own half scores filed under "Q1" and
// "Q2" and two empty quarters after them.
//
// Regulation is always present, filled with nils where the feed has reported
// nothing yet, so the table keeps its shape as a game is played rather than
// growing a column every period. Overtime extends it one column at a time,
// since there is no fixed number of those.
//
// It returns nil only for a nil receiver -- a game with no result row at all,
// which has not kicked off.
//
// The two sides are zipped rather than assumed to be the same length. They
// always have been, but they arrive as two independent arrays from an upstream
// nobody here controls, and indexing one by the other's length is how that
// assumption turns into a panic on a page.
func (r *GameResult) PeriodScores(sport string) []PeriodScore {
	if r == nil {
		return nil
	}

	periods := max(regulationPeriods(sport), len(r.HomeLineScores), len(r.AwayLineScores))

	scores := make([]PeriodScore, periods)
	for i := range scores {
		scores[i] = PeriodScore{Label: periodLabel(sport, i+1)}
		if i < len(r.HomeLineScores) {
			scores[i].Home = &r.HomeLineScores[i]
		}
		if i < len(r.AwayLineScores) {
			scores[i].Away = &r.AwayLineScores[i]
		}
	}
	return scores
}

// BeforeCreate sets the UUID before creating a new game result.
func (r *GameResult) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
