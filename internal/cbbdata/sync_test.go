package cbbdata

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

func TestMapGameStatus(t *testing.T) {
	tests := []struct {
		status string
		want   models.GameStatus
	}{
		{"scheduled", models.GameStatusScheduled},
		{"in_progress", models.GameStatusInProgress},
		{"final", models.GameStatusFinal},
		{"postponed", models.GameStatusPostponed},
		{"cancelled", models.GameStatusCancelled},
		{"canceled", models.GameStatusCancelled},
		{"FINAL", models.GameStatusFinal},
		{"something-new", models.GameStatusScheduled},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := mapGameStatus(tt.status); got != tt.want {
				t.Errorf("mapGameStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestGameResultFrom(t *testing.T) {
	gameID := uuid.New()
	now := time.Date(2026, 8, 28, 22, 30, 0, 0, time.UTC)
	home, away := 68, 71

	tests := []struct {
		name          string
		status        models.GameStatus
		wantFinalized bool
	}{
		{"final game is finalized", models.GameStatusFinal, true},
		// Settlement keys off FinalizedAt, so a game still being played must
		// leave it nil however lopsided the score looks.
		{"in-progress game is not finalized", models.GameStatusInProgress, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gameResultFrom(gameID, APIGame{
				HomePoints:       &home,
				AwayPoints:       &away,
				HomePeriodPoints: []int{30, 38},
				AwayPeriodPoints: []int{35, 36},
			}, tt.status, now)

			if got.GameID != gameID {
				t.Errorf("GameID = %v, want %v", got.GameID, gameID)
			}
			if got.HomeScore != home || got.AwayScore != away {
				t.Errorf("score = %d-%d, want %d-%d", got.HomeScore, got.AwayScore, home, away)
			}
			if got.IsFinal() != tt.wantFinalized {
				t.Errorf("IsFinal() = %v, want %v", got.IsFinal(), tt.wantFinalized)
			}
			// Basketball stores halves rather than quarters, but through the
			// same columns.
			if len(got.HomeLineScores) != 2 || len(got.AwayLineScores) != 2 {
				t.Errorf("period points = %v / %v, want both preserved", got.HomeLineScores, got.AwayLineScores)
			}
		})
	}
}

func TestMapProviderToSource(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		expected models.OddsSource
		known    bool
	}{
		{"bovada", "Bovada", models.OddsSourceBovada, true},
		{"espn bet", "ESPN BET", models.OddsSourceESPN, true},

		// CBBD spells DraftKings with a space and never without one, so this
		// case is not a nicety: it is 42% of every quote the feed sends, and
		// the only book pricing 471 games in a season. cfbdata refuses the same
		// string, because CFBD sends both spellings and they disagree on 70 of
		// the 278 games carrying both -- so mapping both there would store
		// whichever the feed listed last. The two are measured separately on
		// purpose; see the comments on both functions before unifying them.
		{"the spaced spelling is the only one CBBD sends", "Draft Kings", models.OddsSourceDraftKings, true},
		{"and the unspaced one still maps", "DraftKings", models.OddsSourceDraftKings, true},

		{"unknown provider", "UnknownBook", "", false},
		{"empty string", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, known := mapProviderToSource(tt.provider)
			if got != tt.expected {
				t.Errorf("mapProviderToSource(%q) = %q, want %q", tt.provider, got, tt.expected)
			}
			if known != tt.known {
				t.Errorf("mapProviderToSource(%q) known = %v, want %v", tt.provider, known, tt.known)
			}
		})
	}
}

func TestQuotesBySourceFoldsTwoSpellingsOfOneBook(t *testing.T) {
	// Basketball's feed spells DraftKings with a space and both spellings map
	// to one source here, so a response carrying both must still write the
	// line history once per series per run.
	quotes, unknown := quotesBySource([]APILineProvider{
		{Provider: "DraftKings", Spread: new(-3.0), OverUnder: new(141.5)},
		{Provider: "PointsBet", Spread: new(-2.5)},
		{Provider: "Draft Kings", OverUnder: new(142.5), HomeMoneyline: new(-150.0), AwayMoneyline: new(130.0)},
	})

	if len(quotes) != 1 || quotes[0].source != models.OddsSourceDraftKings {
		t.Fatalf("got %+v, want a single draftkings quote", quotes)
	}
	dk := quotes[0].line
	if dk.OverUnder == nil || *dk.OverUnder != 142.5 {
		t.Errorf("total = %v, want the later quote's 142.5", dk.OverUnder)
	}
	if dk.HomeMoneyline == nil || *dk.HomeMoneyline != -150 {
		t.Errorf("home money line = %v, want the later quote's -150", dk.HomeMoneyline)
	}
	if dk.Spread == nil || *dk.Spread != -3 {
		t.Errorf("spread = %v, want the earlier quote's -3, which the later one left empty", dk.Spread)
	}
	if len(unknown) != 1 || unknown[0] != "PointsBet" {
		t.Errorf("unknown = %q, want only PointsBet", unknown)
	}
}

func TestIncrementalWindow(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		now       time.Time
		wantStart string
		wantEnd   string
	}{
		{
			// The instant of the run that exposed it: a clock mid-second, whose
			// hour, minute, second and milliseconds all leaked into the old
			// layout.
			name:      "a run at an arbitrary instant",
			now:       time.Date(2026, 9, 23, 21, 30, 30, 747_000_000, time.UTC),
			wantStart: "2026-09-22T00:00:00.000Z",
			wantEnd:   "2026-09-26T23:59:59.000Z",
		},
		{
			name:      "the window crosses a year end",
			now:       time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC),
			wantStart: "2026-12-29T00:00:00.000Z",
			wantEnd:   "2027-01-02T23:59:59.000Z",
		},
		{
			// 8pm on the 15th in New York is already the 16th in UTC, and the
			// strings claim UTC.
			name:      "a clock in another zone is read as its UTC day",
			now:       time.Date(2026, 1, 15, 20, 0, 0, 0, newYork),
			wantStart: "2026-01-15T00:00:00.000Z",
			wantEnd:   "2026-01-19T23:59:59.000Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := incrementalWindow(tt.now)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("incrementalWindow(%s) = %s .. %s, want %s .. %s",
					tt.now, start, end, tt.wantStart, tt.wantEnd)
			}
			for _, s := range []string{start, end} {
				if _, err := time.Parse(time.RFC3339, s); err != nil {
					t.Errorf("%q is not an RFC 3339 instant, which is what CBBD validates: %v", s, err)
				}
			}
		})
	}
}

// The incremental syncs are the only callers that build their own date window,
// and they sent a malformed one on every run from the day it was written: CBBD
// answered 400 and the job never succeeded. This drives each real job so the
// window is checked where it leaves the process.
//
// It also pins that each job asks only for its own endpoint. They were one job,
// and a /games failure returned before /lines was ever asked -- so a job that
// still reached the other endpoint would still be carrying the other's failure.
func TestIncrementalSyncsSendAValidDateWindow(t *testing.T) {
	tests := []struct {
		name string
		path string
		run  func(*SyncService, context.Context) error
	}{
		{"games", "/games", (*SyncService).SyncGames},
		{"lines", "/lines", (*SyncService).SyncLines},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var queries []string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
				w.Write([]byte(`[]`))
			})

			// An empty answer means nothing is looked up, so no database is
			// needed.
			sync := NewSyncService(c, nil)
			sync.SetClock(func() time.Time { return time.Date(2026, 9, 23, 21, 30, 30, 747_000_000, time.UTC) })
			if err := tt.run(sync, context.Background()); err != nil {
				t.Fatalf("sync error = %v", err)
			}

			if len(queries) != 1 {
				t.Fatalf("made %d requests, want one to %s: %v", len(queries), tt.path, queries)
			}
			u, err := url.Parse(queries[0])
			if err != nil {
				t.Fatalf("parsing %q: %v", queries[0], err)
			}
			if u.Path != tt.path {
				t.Errorf("requested %s, want %s", u.Path, tt.path)
			}
			for _, param := range []string{"startDateRange", "endDateRange"} {
				v := u.Query().Get(param)
				if _, err := time.Parse(time.RFC3339, v); err != nil {
					t.Errorf("%s sent %s=%q, which is not an RFC 3339 instant: %v", u.Path, param, v, err)
				}
			}
		})
	}
}

// A failing /games no longer costs the lines anything, because it no longer
// runs in the same call. The upstream here refuses /games and answers /lines.
func TestLinesSyncDoesNotDependOnGames(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/games" {
			http.Error(w, "upstream down", http.StatusBadGateway)
			return
		}
		w.Write([]byte(`[]`))
	})

	sync := NewSyncService(c, nil)
	sync.SetClock(func() time.Time { return time.Date(2026, 12, 5, 19, 0, 0, 0, time.UTC) })

	if err := sync.SyncGames(context.Background()); err == nil {
		t.Fatal("SyncGames() error = nil against a failing /games; the test's upstream is not refusing it")
	}
	if err := sync.SyncLines(context.Background()); err != nil {
		t.Errorf("SyncLines() error = %v with /games down; the lines should not be asking for it", err)
	}
}
