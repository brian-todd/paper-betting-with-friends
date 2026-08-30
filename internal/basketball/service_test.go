package basketball

import (
	"testing"
	"testing/synctest"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q) error = %v", name, err)
	}
	return loc
}

func TestParseDate(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")
	svc := &Service{location: eastern}

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			// The day has to open at Eastern midnight, not UTC midnight, or the
			// window drags in the tail of the previous night's games.
			name:  "a date is midnight in the app zone",
			value: "2026-08-28",
			want:  "2026-08-28T00:00:00-04:00",
		},
		{
			name:  "standard time uses the winter offset",
			value: "2026-01-15",
			want:  "2026-01-15T00:00:00-05:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.ParseDate(tt.value).Format(time.RFC3339)
			if got != tt.want {
				t.Errorf("ParseDate(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseDateFallsBackToToday(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"not a date", "not-a-date"},
		{"wrong layout", "08/28/2026"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{location: eastern}

			got := svc.ParseDate(tt.value)
			if want := svc.Today(); !got.Equal(want) {
				t.Errorf("ParseDate(%q) = %s, want today (%s)", tt.value, got, want)
			}
		})
	}
}

// The reported bug: at 8:30 PM Eastern the UTC date is already tomorrow, so
// "today's games" jumped a day every evening. synctest pins the clock so the
// boundary can be asserted rather than approximated.
func TestTodayUsesTheAppZoneNotUTC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eastern := mustLoad(t, "America/New_York")

		// The bubble clock starts at 2000-01-01 00:00:00 UTC, which is still
		// 7:00 PM on December 31st in New York.
		if got := time.Now().UTC().Format(time.RFC3339); got != "2000-01-01T00:00:00Z" {
			t.Fatalf("unexpected bubble start time %s", got)
		}

		easternSvc := &Service{location: eastern}
		if got, want := easternSvc.Today().Format("2006-01-02"), "1999-12-31"; got != want {
			t.Errorf("Today() = %s, want %s", got, want)
		}

		utcSvc := &Service{location: time.UTC}
		if got, want := utcSvc.Today().Format("2006-01-02"), "2000-01-01"; got != want {
			t.Errorf("Today() in UTC = %s, want %s", got, want)
		}
	})
}

func TestNewServiceDefaultsToUTC(t *testing.T) {
	// A nil location would panic deep inside time.Time.In, so the constructor
	// has to substitute one rather than trusting its caller.
	svc := NewService(nil, nil)

	if svc.location != time.UTC {
		t.Errorf("location = %v, want UTC", svc.location)
	}
}
