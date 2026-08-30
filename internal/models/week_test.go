package models

import (
	"testing"
	"time"
)

func TestWeekPlausible(t *testing.T) {
	parse := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parsing %q: %v", s, err)
		}
		return v
	}

	tests := []struct {
		name  string
		start string
		end   string
		want  bool
	}{
		{
			name:  "a regular season week",
			start: "2026-08-29T07:00:00Z",
			end:   "2026-09-08T06:59:00Z",
			want:  true,
		},
		{
			name:  "the postseason block",
			start: "2026-12-12T08:00:00Z",
			end:   "2027-01-28T07:59:00Z",
			want:  true,
		},
		{
			// The row that pinned the sync to 2025 while a 2026 game sat
			// unsettled: its span contains every instant of the next season.
			name:  "a year-long span from the calendar feed",
			start: "2025-12-08T08:00:00Z",
			end:   "2026-12-13T07:59:00Z",
			want:  false,
		},
		{
			name:  "an end date before the start date",
			start: "2026-09-08T07:00:00Z",
			end:   "2026-09-01T07:00:00Z",
			want:  false,
		},
		{
			name:  "a zero-length week",
			start: "2026-09-08T07:00:00Z",
			end:   "2026-09-08T07:00:00Z",
			want:  false,
		},
		{
			name:  "exactly the maximum span",
			start: "2026-01-01T00:00:00Z",
			end:   "2026-04-01T00:00:00Z", // 90 days
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			week := Week{StartDate: parse(tt.start), EndDate: parse(tt.end)}
			if got := week.Plausible(); got != tt.want {
				t.Errorf("Plausible() = %v, want %v", got, tt.want)
			}
		})
	}
}
