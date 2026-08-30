package games

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// week builds a week from RFC3339 start/end strings, keeping the table below
// readable.
func week(t *testing.T, season, number int, seasonType models.SeasonType, start, end string) models.Week {
	t.Helper()
	return models.Week{
		Season:     season,
		Number:     number,
		SeasonType: seasonType,
		StartDate:  mustParse(t, start),
		EndDate:    mustParse(t, end),
	}
}

func mustParse(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return parsed
}

// The real 2026 calendar, which is what exposed the bug: the regular season had
// not started yet, so every visit to /games landed on the bowl week in January.
func seededWeeks(t *testing.T) []models.Week {
	t.Helper()
	return []models.Week{
		week(t, 2026, 1, models.SeasonTypeRegular, "2026-08-29T07:00:00Z", "2026-09-08T06:59:00Z"),
		week(t, 2026, 2, models.SeasonTypeRegular, "2026-09-08T07:00:00Z", "2026-09-14T06:59:00Z"),
		week(t, 2026, 3, models.SeasonTypeRegular, "2026-09-14T07:00:00Z", "2026-09-21T06:59:00Z"),
		week(t, 2026, 1, models.SeasonTypePostseason, "2026-12-12T08:00:00Z", "2027-01-28T07:59:00Z"),
	}
}

func TestPickCurrentWeek(t *testing.T) {
	tests := []struct {
		name           string
		now            string
		wantNumber     int
		wantSeasonType models.SeasonType
	}{
		{
			name:           "inside a week picks that week",
			now:            "2026-09-10T18:00:00Z",
			wantNumber:     2,
			wantSeasonType: models.SeasonTypeRegular,
		},
		{
			// The reported bug. Week 1 starts in a few hours; the old code
			// jumped to the postseason five months out.
			name:           "night before the season opens picks week 1",
			now:            "2026-08-29T00:37:00Z",
			wantNumber:     1,
			wantSeasonType: models.SeasonTypeRegular,
		},
		{
			// Weeks do not tile the calendar: week 1 ends at 06:59 and week 2
			// starts at 07:00. Landing in that seam must not skip to January.
			name:           "in the seam between two weeks picks the next one",
			now:            "2026-09-08T06:59:30Z",
			wantNumber:     2,
			wantSeasonType: models.SeasonTypeRegular,
		},
		{
			name:           "between the regular season and bowls picks the postseason",
			now:            "2026-12-10T12:00:00Z",
			wantNumber:     1,
			wantSeasonType: models.SeasonTypePostseason,
		},
		{
			// Nothing is upcoming any more, so the most recently ended week is
			// the only sensible answer.
			name:           "after every week ends picks the last one",
			now:            "2027-06-01T12:00:00Z",
			wantNumber:     1,
			wantSeasonType: models.SeasonTypePostseason,
		},
		{
			name:           "exactly at a start instant picks that week, not the previous one",
			now:            "2026-09-08T07:00:00Z",
			wantNumber:     2,
			wantSeasonType: models.SeasonTypeRegular,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pickCurrentWeek(mustParse(t, tt.now), seededWeeks(t))
			if !ok {
				t.Fatal("pickCurrentWeek found no week")
			}
			if got.Number != tt.wantNumber || got.SeasonType != tt.wantSeasonType {
				t.Errorf("picked %s week %d, want %s week %d",
					got.SeasonType, got.Number, tt.wantSeasonType, tt.wantNumber)
			}
		})
	}
}

func TestPickCurrentWeekWithNoWeeks(t *testing.T) {
	if _, ok := pickCurrentWeek(time.Now(), nil); ok {
		t.Error("pickCurrentWeek reported a week for an empty season list")
	}
}

// The repository returns rows in whatever order Postgres hands back, so the
// answer must not depend on it.
func TestPickCurrentWeekIsOrderIndependent(t *testing.T) {
	now := mustParse(t, "2026-08-29T00:37:00Z")
	weeks := seededWeeks(t)

	reversed := make([]models.Week, len(weeks))
	for i, w := range weeks {
		reversed[len(weeks)-1-i] = w
	}

	forward, ok := pickCurrentWeek(now, weeks)
	if !ok {
		t.Fatal("pickCurrentWeek found no week in forward order")
	}
	backward, ok := pickCurrentWeek(now, reversed)
	if !ok {
		t.Fatal("pickCurrentWeek found no week in reverse order")
	}

	if forward.Number != backward.Number || forward.SeasonType != backward.SeasonType {
		t.Errorf("order changed the answer: %s week %d vs %s week %d",
			forward.SeasonType, forward.Number, backward.SeasonType, backward.Number)
	}
}

func TestPlausibleWeeks(t *testing.T) {
	tests := []struct {
		name   string
		week   models.Week
		usable bool
	}{
		{
			name:   "an ordinary week",
			week:   week(t, 2026, 2, models.SeasonTypeRegular, "2026-09-08T07:00:00Z", "2026-09-14T06:59:00Z"),
			usable: true,
		},
		{
			// Week 1 covers the long opening stretch, and the bowl block runs
			// about seven weeks. Neither may be filtered out.
			name:   "the postseason block",
			week:   week(t, 2026, 1, models.SeasonTypePostseason, "2026-12-12T08:00:00Z", "2027-01-28T07:59:00Z"),
			usable: true,
		},
		{
			// The row actually in the database: 2025 week 16, ending in
			// December 2026. It contains every instant in the 2026 season, so
			// it won the containing-week check on every request.
			name:   "a week ending a year after it starts",
			week:   week(t, 2025, 16, models.SeasonTypeRegular, "2025-12-08T08:00:00Z", "2026-12-13T07:59:00Z"),
			usable: false,
		},
		{
			name:   "a week ending before it starts",
			week:   week(t, 2026, 5, models.SeasonTypeRegular, "2026-09-28T07:00:00Z", "2026-09-21T07:00:00Z"),
			usable: false,
		},
		{
			name:   "a zero-length week",
			week:   week(t, 2026, 5, models.SeasonTypeRegular, "2026-09-28T07:00:00Z", "2026-09-28T07:00:00Z"),
			usable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plausibleWeeks([]models.Week{tt.week})
			if usable := len(got) == 1; usable != tt.usable {
				t.Errorf("usable = %v, want %v", usable, tt.usable)
			}
		})
	}
}

// The bad row and the fix together: without filtering, week 16 of 2025 contains
// August 2026 and beats the real answer.
func TestPlausibleWeeksUnblocksTheCurrentSeason(t *testing.T) {
	now := mustParse(t, "2026-08-29T00:37:00Z")
	weeks := append(seededWeeks(t),
		week(t, 2025, 16, models.SeasonTypeRegular, "2025-12-08T08:00:00Z", "2026-12-13T07:59:00Z"))

	if got, _ := pickCurrentWeek(now, weeks); got.Season != 2025 {
		t.Fatalf("expected the bad row to win unfiltered, got %d week %d", got.Season, got.Number)
	}

	got, ok := pickCurrentWeek(now, plausibleWeeks(weeks))
	if !ok {
		t.Fatal("pickCurrentWeek found no week")
	}
	if got.Season != 2026 || got.Number != 1 || got.SeasonType != models.SeasonTypeRegular {
		t.Errorf("picked %d %s week %d, want 2026 regular week 1", got.Season, got.SeasonType, got.Number)
	}
}
