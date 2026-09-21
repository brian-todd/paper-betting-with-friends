package templates

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The footer's copyright year is the one thing a template resolves from "now"
// by itself, and the one thing on a page rendered against a replayed recording
// that would otherwise come from today. The instants here are decades from any
// fixture and from each other on purpose: an assertion that today's year equals
// the fixture's year is unfalsifiable for eleven months and change.
func TestTheFooterYearComesFromTheRenderersClock(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	t.Run("the zero clock is time.Now", func(t *testing.T) {
		r := newTestRenderer(t, loc)
		if got, want := currentYearOf(t, r), time.Now().In(loc).Year(); got != want {
			t.Errorf("currentYear = %d, want %d", got, want)
		}
	})

	t.Run("SetClock moves it", func(t *testing.T) {
		r := newTestRenderer(t, loc)
		r.SetClock(func() time.Time { return time.Date(1999, 7, 4, 12, 0, 0, 0, time.UTC) })
		if got := currentYearOf(t, r); got != 1999 {
			t.Errorf("currentYear = %d, want 1999", got)
		}
	})

	t.Run("it reads the year in the renderer's zone, not UTC", func(t *testing.T) {
		// 1 January 04:00 UTC is still 31 December in New York, so the two
		// answers differ -- which is the only way to tell that the conversion
		// happens at all.
		r := newTestRenderer(t, loc)
		r.SetClock(func() time.Time { return time.Date(2031, 1, 1, 4, 0, 0, 0, time.UTC) })
		if got := currentYearOf(t, r); got != 2030 {
			t.Errorf("currentYear = %d, want 2030 -- the instant is still New Year's Eve in %s", got, loc)
		}
	})
}

// currentYearOf calls the template function the way a page does, since it is
// only reachable through a parsed template.
func currentYearOf(t *testing.T, r *Renderer) int {
	t.Helper()

	tmpl, err := r.templates["login"].Clone()
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if _, err := tmpl.Parse(`{{define "probe"}}{{currentYear}}{{end}}`); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "probe", nil); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}

	year, err := strconv.Atoi(out.String())
	if err != nil {
		t.Fatalf("currentYear rendered %q, which is not a year: %v", out.String(), err)
	}
	return year
}
