package cfbdata

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestParseSpread(t *testing.T) {
	tests := []struct {
		name         string
		formatted    string
		spreadValue  float64
		homeTeam     *models.Team
		awayTeam     *models.Team
		expectedHome string
		expectedAway string
	}{
		{
			"home team favored",
			"Alabama -7", 7.0,
			&models.Team{Name: "Alabama"}, &models.Team{Name: "Auburn"},
			"-7", "7",
		},
		{
			"away team favored",
			"Auburn -3", 3.0,
			&models.Team{Name: "Alabama"}, &models.Team{Name: "Auburn"},
			"3", "-3",
		},
		{
			"half-point spread",
			"Georgia -7.5", 7.5,
			&models.Team{Name: "Georgia"}, &models.Team{Name: "Florida"},
			"-7.5", "7.5",
		},
		{
			"negative spread value",
			"Ohio State -14", -14.0,
			&models.Team{Name: "Ohio State"}, &models.Team{Name: "Michigan"},
			"-14", "14",
		},
		{
			"unparseable format falls back to home favored",
			"TBD", 7.0,
			&models.Team{Name: "Alabama"}, &models.Team{Name: "Auburn"},
			"-7", "7",
		},
		{
			"nil home team away matched",
			"Auburn -3", 3.0,
			nil, &models.Team{Name: "Auburn"},
			"3", "-3",
		},
		{
			// With both teams nil, the regex-extracted name won't match home (nil),
			// so the function assumes away is favored: home gets positive spread.
			"nil both teams name not matched",
			"Alabama -3", 3.0,
			nil, nil,
			"3", "-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, away := parseSpread(tt.formatted, tt.spreadValue, tt.homeTeam, tt.awayTeam)
			expectedHome := decimal.RequireFromString(tt.expectedHome)
			expectedAway := decimal.RequireFromString(tt.expectedAway)

			if !home.Equal(expectedHome) {
				t.Errorf("homeSpread = %s, want %s", home, expectedHome)
			}
			if !away.Equal(expectedAway) {
				t.Errorf("awaySpread = %s, want %s", away, expectedAway)
			}
		})
	}
}

func TestMapProviderToSource(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		expected models.OddsSource
	}{
		{"draftkings", "DraftKings", models.OddsSourceDraftKings},
		{"fanduel", "FanDuel", models.OddsSourceFanDuel},
		{"betmgm", "BetMGM", models.OddsSourceBetMGM},
		{"caesars", "Caesars", models.OddsSourceCaesars},
		{"espn bet", "ESPN Bet", models.OddsSourceESPN},
		{"espn", "espn", models.OddsSourceESPN},
		{"bovada", "Bovada", models.OddsSourceBovada},
		{"lowercase", "draftkings", models.OddsSourceDraftKings},
		{"unknown provider", "UnknownBook", ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapProviderToSource(tt.provider)
			if got != tt.expected {
				t.Errorf("mapProviderToSource(%q) = %q, want %q", tt.provider, got, tt.expected)
			}
		})
	}
}

func TestFormatColor(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *string
	}{
		{"empty string", "", nil},
		{"with hash", "#FF0000", strPtr("#FF0000")},
		{"without hash", "FF0000", strPtr("#FF0000")},
		{"too long truncated", "#FF0000FF", strPtr("#FF0000")},
		{"short code", "#FFF", strPtr("#FFF")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatColor(tt.input)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("formatColor(%q) = %q, want nil", tt.input, *got)
				}
				return
			}
			if got == nil {
				t.Errorf("formatColor(%q) = nil, want %q", tt.input, *tt.expected)
				return
			}
			if *got != *tt.expected {
				t.Errorf("formatColor(%q) = %q, want %q", tt.input, *got, *tt.expected)
			}
		})
	}
}

func TestStrPtr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *string
	}{
		{"empty returns nil", "", nil},
		{"non-empty returns pointer", "hello", new("hello")},
		{"whitespace is not empty", " ", new(" ")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strPtr(tt.input)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("strPtr(%q) = %q, want nil", tt.input, *got)
				}
				return
			}
			if got == nil {
				t.Errorf("strPtr(%q) = nil, want %q", tt.input, *tt.expected)
				return
			}
			if *got != *tt.expected {
				t.Errorf("strPtr(%q) = %q, want %q", tt.input, *got, *tt.expected)
			}
		})
	}
}

func TestSyncScope(t *testing.T) {
	five := 5
	regular := "regular"
	tests := []struct {
		name       string
		year       int
		week       *int
		seasonType *string
		expected   []any
	}{
		{"year only", 2024, nil, nil, []any{"year", 2024}},
		{"with week", 2024, &five, nil, []any{"year", 2024, "week", 5}},
		{"with season type", 2024, nil, &regular, []any{"year", 2024, "season_type", "regular"}},
		{"full scope", 2024, &five, &regular, []any{"year", 2024, "week", 5, "season_type", "regular"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := syncScope(tt.year, tt.week, tt.seasonType)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("syncScope() = %v, want %v", got, tt.expected)
			}
		})
	}
}

//go:fix inline
func ptrTo(s string) *string {
	return new(s)
}

func TestGameResultFrom(t *testing.T) {
	gameID := uuid.New()
	now := time.Date(2026, 8, 28, 22, 30, 0, 0, time.UTC)
	home, away := 21, 14

	tests := []struct {
		name          string
		completed     bool
		wantFinalized bool
	}{
		{"completed game is finalized", true, true},
		// The regression this guards: a live score used to be written only for
		// a completed game, and settlement keys off FinalizedAt, so marking a
		// game in progress as finalized would pay out at halftime.
		{"in-progress game is not finalized", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gameResultFrom(gameID, APIGame{
				Completed:      tt.completed,
				HomePoints:     &home,
				AwayPoints:     &away,
				HomeLineScores: []int{7, 14},
				AwayLineScores: []int{0, 14},
			}, now)

			if got.GameID != gameID {
				t.Errorf("GameID = %v, want %v", got.GameID, gameID)
			}
			if got.HomeScore != home || got.AwayScore != away {
				t.Errorf("score = %d-%d, want %d-%d", got.HomeScore, got.AwayScore, home, away)
			}
			if got.IsFinal() != tt.wantFinalized {
				t.Errorf("IsFinal() = %v, want %v", got.IsFinal(), tt.wantFinalized)
			}
			if tt.wantFinalized && !got.FinalizedAt.Equal(now) {
				t.Errorf("FinalizedAt = %v, want %v", got.FinalizedAt, now)
			}
			if len(got.HomeLineScores) != 2 || len(got.AwayLineScores) != 2 {
				t.Errorf("line scores = %v / %v, want both preserved", got.HomeLineScores, got.AwayLineScores)
			}
		})
	}
}

func TestGameResultFromCarriesExcitementIndex(t *testing.T) {
	home, away := 3, 0
	index := 7.25

	got := gameResultFrom(uuid.New(), APIGame{
		HomePoints:      &home,
		AwayPoints:      &away,
		ExcitementIndex: &index,
	}, time.Now())

	if got.ExcitementIndex == nil {
		t.Fatal("ExcitementIndex was dropped")
	}
	if want := decimal.NewFromFloat(index); !got.ExcitementIndex.Equal(want) {
		t.Errorf("ExcitementIndex = %s, want %s", got.ExcitementIndex, want)
	}

	// An absent index must stay absent rather than becoming a zero score.
	got = gameResultFrom(uuid.New(), APIGame{HomePoints: &home, AwayPoints: &away}, time.Now())
	if got.ExcitementIndex != nil {
		t.Errorf("ExcitementIndex = %s, want nil", got.ExcitementIndex)
	}
}

// discardLogger is a logger that writes nowhere, for tests that only care
// about the return value of a function that also happens to log.
var discardLogger = slog.New(slog.DiscardHandler)

func TestRankingsFromPollSkipsUnknownSchools(t *testing.T) {
	weekID := uuid.New()
	bamaID := uuid.New()

	resolve := func(school string) (uuid.UUID, bool) {
		if school == "Alabama" {
			return bamaID, true
		}
		return uuid.Nil, false
	}

	poll := APIPoll{
		Poll: models.PollAP,
		Ranks: []APIRank{
			{Rank: 1, School: "Alabama"},
			// A misspelled or renamed school should not cost the rest of the
			// poll -- it is skipped and logged, not fatal.
			{Rank: 2, School: "Not A Real School"},
		},
	}

	got := rankingsFromPoll(weekID, poll, resolve, discardLogger)

	if len(got) != 1 {
		t.Fatalf("got %d rankings, want 1 (the unresolved school dropped)", len(got))
	}
	if got[0].TeamID != bamaID || got[0].Rank != 1 || got[0].WeekID != weekID || got[0].Poll != models.PollAP {
		t.Errorf("rankings[0] = %+v, want Alabama at rank 1 for week %s poll %s", got[0], weekID, models.PollAP)
	}
}

func TestRankingsFromPollTeamDroppingOutOfPoll(t *testing.T) {
	weekID := uuid.New()
	bamaID, georgiaID := uuid.New(), uuid.New()

	resolve := func(school string) (uuid.UUID, bool) {
		switch school {
		case "Alabama":
			return bamaID, true
		case "Georgia":
			return georgiaID, true
		default:
			return uuid.Nil, false
		}
	}

	// Week N: both teams ranked.
	before := rankingsFromPoll(weekID, APIPoll{
		Poll: models.PollAP,
		Ranks: []APIRank{
			{Rank: 1, School: "Georgia"},
			{Rank: 2, School: "Alabama"},
		},
	}, resolve, discardLogger)
	if len(before) != 2 {
		t.Fatalf("got %d rankings before, want 2", len(before))
	}

	// Week N+1: Alabama drops out of the poll entirely. Since syncRankings
	// re-syncs a (week, poll) group as delete-then-insert of exactly what this
	// function returns, Alabama's absence here is what removes its row.
	after := rankingsFromPoll(weekID, APIPoll{
		Poll: models.PollAP,
		Ranks: []APIRank{
			{Rank: 1, School: "Georgia"},
		},
	}, resolve, discardLogger)

	if len(after) != 1 {
		t.Fatalf("got %d rankings after, want 1", len(after))
	}
	if after[0].TeamID != georgiaID {
		t.Errorf("surviving ranking = %+v, want Georgia", after[0])
	}
	for _, r := range after {
		if r.TeamID == bamaID {
			t.Error("Alabama is still present after dropping out of the poll")
		}
	}
}
