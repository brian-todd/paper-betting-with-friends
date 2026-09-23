package repository

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"gorm.io/gorm"
)

// A Saturday in the middle of the 2026 season.
var midSeason = time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)

func insertWeek(t *testing.T, db *gorm.DB, season int, start, end time.Time) {
	t.Helper()

	week := &models.Week{Season: season, Number: 7, SeasonType: models.SeasonTypeRegular, StartDate: start, EndDate: end}
	if err := db.Create(week).Error; err != nil {
		t.Fatalf("inserting week: %v", err)
	}
}

// FindSeasonContainingDate decides which season the background sync fetches,
// so a wrong answer here is a sync that pulls a year holding none of the games
// being watched and logs success. See AGENTS.md, The Week Calendar.
func TestWeekRepositoryFindSeasonContainingDate(t *testing.T) {
	t.Run("the season whose week contains the instant", func(t *testing.T) {
		db := testdb.Open(t)
		insertWeek(t, db, 2026, midSeason.AddDate(0, 0, -3), midSeason.AddDate(0, 0, 3))

		season, err := NewWeekRepository(db).FindSeasonContainingDate(midSeason)
		if err != nil {
			t.Fatalf("FindSeasonContainingDate: %v", err)
		}
		if season != 2026 {
			t.Errorf("season = %d, want 2026", season)
		}
	})

	t.Run("an implausibly long week does not hide the real one", func(t *testing.T) {
		// A row with a year-long span contains every instant in between, and
		// with a later season it wins the ORDER BY season DESC -- which is how
		// one bad calendar row sent the sync after the wrong year.
		db := testdb.Open(t)
		insertWeek(t, db, 2026, midSeason.AddDate(0, 0, -3), midSeason.AddDate(0, 0, 3))
		insertWeek(t, db, 2027, midSeason.AddDate(0, -6, 0), midSeason.AddDate(0, 6, 0))

		season, err := NewWeekRepository(db).FindSeasonContainingDate(midSeason)
		if err != nil {
			t.Fatalf("FindSeasonContainingDate: %v", err)
		}
		if season != 2026 {
			t.Errorf("season = %d, want 2026: the year-long 2027 row should be ignored", season)
		}
	})

	t.Run("no week containing the instant reads as zero", func(t *testing.T) {
		// GetCurrentSeasonYear treats zero as "none" and falls back to the
		// calendar year, so zero with no error is the contract, not a miss.
		db := testdb.Open(t)
		insertWeek(t, db, 2026, midSeason.AddDate(0, 0, 1), midSeason.AddDate(0, 0, 7))

		season, err := NewWeekRepository(db).FindSeasonContainingDate(midSeason)
		if err != nil {
			t.Fatalf("FindSeasonContainingDate: %v", err)
		}
		if season != 0 {
			t.Errorf("season = %d, want 0", season)
		}
	})
}

// The SQL filter and models.Week.Plausible are two statements of one rule, one
// for each consumer of the calendar. If they disagree at the edge, the week
// page and the sync answer "which week is it" differently. So each span is
// judged by both, and the database has to agree with the Go.
func TestWeekRepositorySpanFilterAgreesWithPlausible(t *testing.T) {
	for _, tc := range []struct {
		name string
		span time.Duration
	}{
		{"a normal week", 6 * 24 * time.Hour},
		{"exactly the maximum span", models.MaxWeekSpan},
		{"a second past the maximum", models.MaxWeekSpan + time.Second},
		{"a year", 365 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testdb.Open(t)
			start := midSeason.Add(-tc.span / 2)
			week := models.Week{StartDate: start, EndDate: start.Add(tc.span)}
			insertWeek(t, db, 2026, week.StartDate, week.EndDate)

			season, err := NewWeekRepository(db).FindSeasonContainingDate(midSeason)
			if err != nil {
				t.Fatalf("FindSeasonContainingDate: %v", err)
			}
			if found, plausible := season == 2026, week.Plausible(); found != plausible {
				t.Errorf("SQL found the week = %v, Plausible() = %v", found, plausible)
			}
		})
	}
}
