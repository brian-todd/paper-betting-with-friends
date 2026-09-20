package cbbdata

import (
	"testing"
	"time"
)

// A basketball season is named for the calendar year it ends in, and this got it
// wrong by one for every month of the season. The expectations are not derived
// from the function -- they are what CBBD's own `season` field says on the games
// in the committed captures, which is the only authority on the question.
func TestSeasonForMatchesTheFeedsOwnLabel(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want int
	}{
		{
			// The captures hold games from 2025-11-03, and every one of them
			// carries "season": 2026.
			name: "opening night is the season it ends in",
			when: time.Date(2025, time.November, 3, 0, 0, 0, 0, time.UTC),
			want: 2026,
		},
		{
			name: "December is still the later year's season",
			when: time.Date(2025, time.December, 20, 12, 0, 0, 0, time.UTC),
			want: 2026,
		},
		{
			name: "January is the same season, not a new one",
			when: time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC),
			want: 2026,
		},
		{
			// The last games in the captures are 2026-04-07, season 2026.
			name: "April closes the season it is named for",
			when: time.Date(2026, time.April, 7, 0, 50, 0, 0, time.UTC),
			want: 2026,
		},
		{
			name: "June is after the last game and before the next season",
			when: time.Date(2026, time.June, 30, 23, 59, 59, 0, time.UTC),
			want: 2026,
		},
		{
			// The offseason resolves forward: a seed run in the autumn wants the
			// season about to start, not the one that ended in the spring.
			name: "July flips to the season about to start",
			when: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			want: 2027,
		},
		{
			name: "September is the coming season",
			when: time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC),
			want: 2027,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeasonFor(tt.when); got != tt.want {
				t.Errorf("SeasonFor(%s) = %d, want %d",
					tt.when.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}

// SeedAll chunks a season into the six month windows the captures were recorded
// against, and SeasonFor has to agree with it about which year those months
// belong to, or a seed fetches windows from a season the caller did not ask for.
func TestSeasonForAgreesWithTheWindowsSeedAllFetches(t *testing.T) {
	const season = 2026

	// The first and last instants SeedAll's windows cover for this season.
	first := time.Date(season-1, time.November, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(season, time.May, 1, 0, 0, 0, 0, time.UTC).Add(-time.Second)

	if got := SeasonFor(first); got != season {
		t.Errorf("SeasonFor(%s) = %d, but SeedAll(%d) fetches from that instant",
			first.Format("2006-01-02"), got, season)
	}
	// The window runs to 1 May; SeasonFor's boundary is 1 July, so the whole of
	// it resolves to the same season.
	if got := SeasonFor(last); got != season {
		t.Errorf("SeasonFor(%s) = %d, but SeedAll(%d) fetches up to that instant",
			last.Format("2006-01-02"), got, season)
	}
}
