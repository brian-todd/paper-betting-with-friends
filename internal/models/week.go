package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SeasonType represents the type of season (regular or postseason).
type SeasonType string

const (
	SeasonTypeRegular    SeasonType = "regular"
	SeasonTypePostseason SeasonType = "postseason"
)

// Week represents a college football week within a season.
type Week struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Season     int        `gorm:"not null;index"`
	Number     int        `gorm:"not null"`
	SeasonType SeasonType `gorm:"type:varchar(20);default:'regular'"`
	StartDate  time.Time  `gorm:"not null"`
	EndDate    time.Time  `gorm:"not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// MaxWeekSpan bounds how long a week may plausibly run.
//
// Regular-season weeks are six to ten days and the postseason block runs about
// seven, so anything longer is a bad row from the calendar feed rather than a
// real week. These are not harmless: a single week with an end date a year past
// its start contains every instant in between, so it wins any "which week
// contains now" check and hides the rest of the calendar behind it.
const MaxWeekSpan = 90 * 24 * time.Hour

// Plausible reports whether the week's dates could describe a real week.
//
// Anything reading the calendar to answer "where are we now" has to apply this
// -- the feed has shipped rows spanning a full year, and one of those silently
// pins both the games UI and the data sync to the wrong season.
func (w *Week) Plausible() bool {
	return w.EndDate.After(w.StartDate) && w.EndDate.Sub(w.StartDate) <= MaxWeekSpan
}

// BeforeCreate sets the UUID before creating a new week.
func (w *Week) BeforeCreate(tx *gorm.DB) error {
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	return nil
}
