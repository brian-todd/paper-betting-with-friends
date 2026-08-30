package timeutil

import (
	"testing"
	"time"
)

func TestStartOfDay(t *testing.T) {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	tests := []struct {
		name string
		in   time.Time
		loc  *time.Location
		want string
	}{
		{
			// The bug this replaced: 8:30 PM Eastern is already tomorrow in UTC,
			// so a UTC-truncated "today" skipped a day for anyone on the East Coast.
			name: "late evening stays on the same eastern day",
			in:   time.Date(2026, 8, 28, 20, 30, 0, 0, eastern),
			loc:  eastern,
			want: "2026-08-28T00:00:00-04:00",
		},
		{
			name: "an instant stored in UTC resolves to its eastern day",
			in:   time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC),
			loc:  eastern,
			want: "2026-08-28T00:00:00-04:00",
		},
		{
			name: "early morning eastern is still the previous UTC day",
			in:   time.Date(2026, 8, 28, 0, 30, 0, 0, eastern),
			loc:  eastern,
			want: "2026-08-28T00:00:00-04:00",
		},
		{
			name: "midnight is already the start of its day",
			in:   time.Date(2026, 8, 28, 0, 0, 0, 0, eastern),
			loc:  eastern,
			want: "2026-08-28T00:00:00-04:00",
		},
		{
			name: "standard time carries the winter offset",
			in:   time.Date(2026, 1, 15, 22, 0, 0, 0, eastern),
			loc:  eastern,
			want: "2026-01-15T00:00:00-05:00",
		},
		{
			name: "a nil location falls back to UTC",
			in:   time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC),
			loc:  nil,
			want: "2026-08-29T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StartOfDay(tt.in, tt.loc).Format(time.RFC3339)
			if got != tt.want {
				t.Errorf("StartOfDay(%s) = %s, want %s", tt.in.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}

// A day is not always 24 hours. Building the boundary with time.Date keeps
// AddDate landing on the next midnight across a DST change, which a
// Truncate-then-add-24h window would miss by an hour.
func TestStartOfDaySpansDSTTransition(t *testing.T) {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	tests := []struct {
		name     string
		day      time.Time
		wantSpan time.Duration
	}{
		{"spring forward", time.Date(2026, 3, 8, 12, 0, 0, 0, eastern), 23 * time.Hour},
		{"fall back", time.Date(2026, 11, 1, 12, 0, 0, 0, eastern), 25 * time.Hour},
		{"ordinary day", time.Date(2026, 8, 28, 12, 0, 0, 0, eastern), 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := StartOfDay(tt.day, eastern)
			end := start.AddDate(0, 0, 1)

			if got := end.Sub(start); got != tt.wantSpan {
				t.Errorf("day span = %v, want %v", got, tt.wantSpan)
			}
			if h, m, s := end.Clock(); h != 0 || m != 0 || s != 0 {
				t.Errorf("next day starts at %02d:%02d:%02d, want midnight", h, m, s)
			}
		})
	}
}
