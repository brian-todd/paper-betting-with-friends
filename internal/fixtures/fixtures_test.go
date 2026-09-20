package fixtures

import (
	"strings"
	"testing"
	"time"
)

func TestQuerySlugSortsSoCallerOrderDoesNotMatter(t *testing.T) {
	// The point of sorting: the client assembles /games?year=...&week=... and
	// /rankings?year=...&week=...&seasonType=..., and a capture taken through
	// a different tool need not have built the string the same way. Without
	// this, the two land in different directories and the server 500s on a
	// fixture that is right there.
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"empty is underscore", "", "_"},
		{"single pair", "year=2026", "year=2026"},
		{"sorted, not as given", "year=2026&week=2", "week=2&year=2026"},
		{"already sorted", "week=2&year=2026", "week=2&year=2026"},
		{"three keys", "year=2026&week=2&seasonType=regular", "seasonType=regular&week=2&year=2026"},
		{"repeated key sorts its values", "cls=fcs&cls=fbs", "cls=fbs&cls=fcs"},
		{"underscore survives", "status=in_progress", "status=in_progress"},
		{"a slash cannot become a directory", "q=a/b", "q=a%2Fb"},
		{"a dot-dot cannot climb", "q=..", "q=.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QuerySlug(tt.query)
			if err != nil {
				t.Fatalf("QuerySlug(%q): %v", tt.query, err)
			}
			if got != tt.want {
				t.Errorf("QuerySlug(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestDirPutsQueryAtTheLeaf(t *testing.T) {
	// teams/_/ and teams/ats/week=2&year=2026/ have to coexist without either
	// being mistaken for the other, which is the whole reason "_" exists.
	tests := []struct {
		path, query, want string
	}{
		{"/teams", "", "cfbd/teams/_"},
		{"/teams/ats", "year=2026&week=2", "cfbd/teams/ats/week=2&year=2026"},
		{"/scoreboard", "classification=fbs", "cfbd/scoreboard/classification=fbs"},
		{"/games/weather", "year=2026", "cfbd/games/weather/year=2026"},
	}

	for _, tt := range tests {
		got, err := Dir(CFBD, tt.path, tt.query)
		if err != nil {
			t.Fatalf("Dir(%q, %q): %v", tt.path, tt.query, err)
		}
		if got != tt.want {
			t.Errorf("Dir(%q, %q) = %q, want %q", tt.path, tt.query, got, tt.want)
		}
	}
}

func TestParseNameReadsInstantAndStatus(t *testing.T) {
	tests := []struct {
		name          string
		wantAt        string
		wantStatus    int
		wantMalformed bool
		wantErr       bool
	}{
		{name: "20260905T204909Z.json", wantAt: "2026-09-05T20:49:09Z", wantStatus: 200},
		{name: "20260905T204909Z.502.json", wantAt: "2026-09-05T20:49:09Z", wantStatus: 502},
		{name: "20260905T204909Z.bad", wantAt: "2026-09-05T20:49:09Z", wantStatus: 200, wantMalformed: true},
		{name: "20260905T204909Z.500.bad", wantAt: "2026-09-05T20:49:09Z", wantStatus: 500, wantMalformed: true},
		{name: "summary.tsv", wantErr: true},
		{name: "notatime.json", wantErr: true},
		{name: "20260905T204909Z.xx.json", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseName(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseName(%q) = %+v, want an error", tt.name, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseName(%q): %v", tt.name, err)
			}
			if want, _ := time.Parse(time.RFC3339, tt.wantAt); !got.At.Equal(want) {
				t.Errorf("At = %s, want %s", got.At, want)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", got.Status, tt.wantStatus)
			}
			if got.Malformed != tt.wantMalformed {
				t.Errorf("Malformed = %v, want %v", got.Malformed, tt.wantMalformed)
			}
		})
	}
}

func TestEmbeddedScoreboardSequenceIsOrderedAndWhole(t *testing.T) {
	// The captured Saturday is the most valuable fixture in the tree: 99 FBS
	// games walking from mostly-scheduled to mostly-final, sampled every
	// fifteen minutes. A test replaying it depends on the order, and the order
	// comes from the file names rather than from whatever ReadDir happens to
	// return.
	captures, err := Embedded().Sequence(CFBD, "/scoreboard", "classification=fbs")
	if err != nil {
		t.Fatalf("resolving the scoreboard sequence: %v", err)
	}

	if len(captures) < 20 {
		t.Fatalf("got %d captures, want the whole recorded Saturday", len(captures))
	}

	for i, c := range captures {
		if i > 0 && !c.At.After(captures[i-1].At) {
			t.Errorf("capture %d (%s) does not follow %s", i, c.At, captures[i-1].At)
		}
		if c.Status != 200 {
			t.Errorf("capture %d has status %d, want 200", i, c.Status)
		}
		if len(c.Body) == 0 {
			t.Errorf("capture %d (%s) is empty", i, c.Path)
		}
	}
}

func TestSequenceSaysWhereItLookedWhenNothingIsThere(t *testing.T) {
	// The error text is the whole ergonomics of this package: a missing
	// fixture is going to happen, and the fix is always "capture that path",
	// which the reader can only do if the message names it.
	_, err := Embedded().Sequence(CFBD, "/ratings/sp", "year=2026")
	if err == nil {
		t.Fatal("want an error for a path with no fixtures")
	}
	if want := "cfbd/ratings/sp/year=2026"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the directory %q", err, want)
	}
}
