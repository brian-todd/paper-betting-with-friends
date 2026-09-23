package cbbdata

import (
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
