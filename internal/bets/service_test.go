package bets

import (
	"slices"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestCalculatePayout(t *testing.T) {
	tests := []struct {
		name     string
		stake    string
		odds     string
		expected string
	}{
		{"positive odds +150", "100", "150", "250"},
		{"positive odds even +100", "100", "100", "200"},
		{"negative odds -150", "100", "-150", "166.6666666666666667"},
		{"negative odds even -100", "100", "-100", "200"},
		{"standard -110", "110", "-110", "210"},
		{"long shot +500", "50", "500", "300"},
		{"small stake", "1", "150", "2.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stake := decimal.RequireFromString(tt.stake)
			odds := decimal.RequireFromString(tt.odds)
			expected := decimal.RequireFromString(tt.expected)

			got := calculatePayout(stake, odds)
			if !got.Equal(expected) {
				t.Errorf("calculatePayout(%s, %s) = %s, want %s", tt.stake, tt.odds, got, expected)
			}
		})
	}
}

func TestEvaluateSpreadBet(t *testing.T) {
	tests := []struct {
		name      string
		pick      models.SpreadPick
		spread    string // SpreadSnapshot
		homeScore int
		awayScore int
		expected  models.BetStatus
	}{
		{"home favored covers", models.SpreadPickHome, "-7", 28, 14, models.BetStatusWon},
		{"home favored fails to cover", models.SpreadPickHome, "-7", 20, 14, models.BetStatusLost},
		{"home favored push", models.SpreadPickHome, "-7", 21, 14, models.BetStatusPush},
		{"home underdog covers", models.SpreadPickHome, "3", 17, 14, models.BetStatusWon},
		{"home zero spread wins", models.SpreadPickHome, "0", 21, 14, models.BetStatusWon},
		{"home zero spread tie", models.SpreadPickHome, "0", 14, 14, models.BetStatusPush},
		{"home half-point loses", models.SpreadPickHome, "-7.5", 21, 14, models.BetStatusLost},
		{"home half-point wins", models.SpreadPickHome, "-6.5", 21, 14, models.BetStatusWon},
		// Away pick: SpreadSnapshot is the away spread (e.g., +7 for underdog, -3 for favorite).
		// pickedAdjusted = awayScore + SpreadSnapshot vs homeScore.
		{"away underdog covers", models.SpreadPickAway, "7", 21, 17, models.BetStatusWon},
		{"away underdog fails to cover", models.SpreadPickAway, "7", 28, 14, models.BetStatusLost},
		{"away underdog push", models.SpreadPickAway, "7", 21, 14, models.BetStatusPush},
		{"away favored covers", models.SpreadPickAway, "-3", 14, 20, models.BetStatusWon},
		{"away favored fails to cover", models.SpreadPickAway, "-3", 14, 16, models.BetStatusLost},
		{"away favored push", models.SpreadPickAway, "-3", 14, 17, models.BetStatusPush},
		{"away half-point covers", models.SpreadPickAway, "7.5", 21, 14, models.BetStatusWon},
		{"away half-point fails", models.SpreadPickAway, "6.5", 21, 14, models.BetStatusLost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bet := &models.SpreadBet{
				Pick:           tt.pick,
				SpreadSnapshot: decimal.RequireFromString(tt.spread),
			}
			result := &models.GameResult{
				HomeScore: tt.homeScore,
				AwayScore: tt.awayScore,
			}

			got := evaluateSpreadBet(bet, result)
			if got != tt.expected {
				t.Errorf("evaluateSpreadBet(pick=%s, spread=%s, home=%d, away=%d) = %s, want %s",
					tt.pick, tt.spread, tt.homeScore, tt.awayScore, got, tt.expected)
			}
		})
	}
}

func TestEvaluateMoneyLineBet(t *testing.T) {
	tests := []struct {
		name      string
		pick      models.MoneyLinePick
		homeScore int
		awayScore int
		expected  models.BetStatus
	}{
		{"home pick home wins", models.MoneyLinePickHome, 28, 14, models.BetStatusWon},
		{"home pick away wins", models.MoneyLinePickHome, 14, 28, models.BetStatusLost},
		{"away pick away wins", models.MoneyLinePickAway, 14, 28, models.BetStatusWon},
		{"away pick home wins", models.MoneyLinePickAway, 28, 14, models.BetStatusLost},
		{"tie home pick", models.MoneyLinePickHome, 14, 14, models.BetStatusPush},
		{"tie away pick", models.MoneyLinePickAway, 14, 14, models.BetStatusPush},
		{"one point margin", models.MoneyLinePickHome, 15, 14, models.BetStatusWon},
		{"zero zero tie", models.MoneyLinePickHome, 0, 0, models.BetStatusPush},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bet := &models.MoneyLineBet{Pick: tt.pick}
			result := &models.GameResult{
				HomeScore: tt.homeScore,
				AwayScore: tt.awayScore,
			}

			got := evaluateMoneyLineBet(bet, result)
			if got != tt.expected {
				t.Errorf("evaluateMoneyLineBet(pick=%s, home=%d, away=%d) = %s, want %s",
					tt.pick, tt.homeScore, tt.awayScore, got, tt.expected)
			}
		})
	}
}

func TestEvaluateOverUnderBet(t *testing.T) {
	tests := []struct {
		name      string
		pick      models.OverUnderPick
		total     string
		homeScore int
		awayScore int
		expected  models.BetStatus
	}{
		{"over hits", models.OverUnderPickOver, "45", 28, 21, models.BetStatusWon},
		{"over misses", models.OverUnderPickOver, "45", 14, 10, models.BetStatusLost},
		{"under hits", models.OverUnderPickUnder, "45", 14, 10, models.BetStatusWon},
		{"under misses", models.OverUnderPickUnder, "45", 28, 21, models.BetStatusLost},
		{"push exact over", models.OverUnderPickOver, "45", 24, 21, models.BetStatusPush},
		{"push exact under", models.OverUnderPickUnder, "45", 24, 21, models.BetStatusPush},
		{"half-point over loses", models.OverUnderPickOver, "45.5", 24, 21, models.BetStatusLost},
		{"half-point under wins", models.OverUnderPickUnder, "45.5", 24, 21, models.BetStatusWon},
		{"zero zero under", models.OverUnderPickUnder, "45", 0, 0, models.BetStatusWon},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bet := &models.OverUnderBet{
				Pick:          tt.pick,
				TotalSnapshot: decimal.RequireFromString(tt.total),
			}
			result := &models.GameResult{
				HomeScore: tt.homeScore,
				AwayScore: tt.awayScore,
			}

			got := evaluateOverUnderBet(bet, result)
			if got != tt.expected {
				t.Errorf("evaluateOverUnderBet(pick=%s, total=%s, home=%d, away=%d) = %s, want %s",
					tt.pick, tt.total, tt.homeScore, tt.awayScore, got, tt.expected)
			}
		})
	}
}

func TestFormatSpread(t *testing.T) {
	tests := []struct {
		name     string
		spread   string
		expected string
	}{
		{"negative", "-7", "-7"},
		{"positive", "7", "+7"},
		{"zero", "0", "0"},
		{"negative half", "-3.5", "-3.5"},
		{"positive half", "3.5", "+3.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSpread(decimal.RequireFromString(tt.spread))
			if got != tt.expected {
				t.Errorf("formatSpread(%s) = %q, want %q", tt.spread, got, tt.expected)
			}
		})
	}
}

func TestFormatOdds(t *testing.T) {
	tests := []struct {
		name     string
		odds     string
		expected string
	}{
		{"positive", "150", "+150"},
		{"negative", "-150", "-150"},
		{"even positive", "100", "+100"},
		{"even negative", "-100", "-100"},
		{"zero", "0", "0"},
		{"standard -110", "-110", "-110"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOdds(decimal.RequireFromString(tt.odds))
			if got != tt.expected {
				t.Errorf("formatOdds(%s) = %q, want %q", tt.odds, got, tt.expected)
			}
		})
	}
}

func TestMirrorSpread(t *testing.T) {
	tests := []struct {
		name     string
		pick     models.SpreadPick
		spread   string
		wantHome string
		wantAway string
	}{
		{"home favorite", models.SpreadPickHome, "-7", "-7", "7"},
		{"home underdog", models.SpreadPickHome, "3.5", "3.5", "-3.5"},
		// The sign flip is the whole point: taking the away team at -7 must
		// store the home team at +7, not -7.
		{"away favorite", models.SpreadPickAway, "-7", "7", "-7"},
		{"away underdog", models.SpreadPickAway, "3.5", "-3.5", "3.5"},
		{"pick em", models.SpreadPickHome, "0", "0", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, away := mirrorSpread(tt.pick, decimal.RequireFromString(tt.spread))

			if want := decimal.RequireFromString(tt.wantHome); !home.Equal(want) {
				t.Errorf("home spread = %s, want %s", home, want)
			}
			if want := decimal.RequireFromString(tt.wantAway); !away.Equal(want) {
				t.Errorf("away spread = %s, want %s", away, want)
			}
			if !home.Add(away).IsZero() {
				t.Errorf("the two sides do not cancel: %s and %s", home, away)
			}
		})
	}
}

func TestEditable(t *testing.T) {
	now := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	kickoff := func(offset time.Duration) models.Game {
		return models.Game{ScheduledAt: now.Add(offset)}
	}

	tests := []struct {
		name   string
		status models.BetStatus
		game   models.Game
		want   bool
	}{
		{"pending bet before kickoff", models.BetStatusPending, kickoff(time.Hour), true},
		{"pending bet after kickoff", models.BetStatusPending, kickoff(-time.Hour), false},
		// Exactly at kickoff the game has started; authorizeEdit rejects it, so
		// the page must not offer it either.
		{"pending bet at kickoff", models.BetStatusPending, kickoff(0), false},
		{"settled win", models.BetStatusWon, kickoff(time.Hour), false},
		{"settled loss", models.BetStatusLost, kickoff(time.Hour), false},
		{"cancelled bet", models.BetStatusVoid, kickoff(time.Hour), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := editable(tt.status, tt.game, now); got != tt.want {
				t.Errorf("editable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourceLabel(t *testing.T) {
	if got, want := sourceLabel(models.OddsSourceDraftKings), "DraftKings"; got != want {
		t.Errorf("sourceLabel(draftkings) = %q, want %q", got, want)
	}
	// An unrecognised source has to still name itself; a blank entry in the
	// line dropdown would be unpickable.
	if got, want := sourceLabel(models.OddsSource("pinnacle")), "pinnacle"; got != want {
		t.Errorf("sourceLabel(pinnacle) = %q, want %q", got, want)
	}
}

func TestFilterOptions(t *testing.T) {
	week := func(n int) *int { return &n }
	season := func(n int) *int { return &n }

	tests := []struct {
		name        string
		periods     []repository.BetPeriod
		selected    *int
		wantSeasons []int
		wantWeeks   []int
	}{
		{
			name: "with no season selected every week holding a bet is offered",
			periods: []repository.BetPeriod{
				{Season: 2026, Week: week(1)},
				{Season: 2025, Week: week(14)},
				{Season: 2025, Week: week(3)},
			},
			selected:    nil,
			wantSeasons: []int{2026, 2025},
			wantWeeks:   []int{1, 3, 14},
		},
		{
			name: "selecting a season narrows the weeks to it",
			periods: []repository.BetPeriod{
				{Season: 2026, Week: week(1)},
				{Season: 2025, Week: week(14)},
				{Season: 2025, Week: week(3)},
			},
			selected:    season(2025),
			wantSeasons: []int{2026, 2025},
			wantWeeks:   []int{3, 14},
		},
		{
			name: "a season is still offered when its weeks are filtered out",
			periods: []repository.BetPeriod{
				{Season: 2026, Week: week(1)},
				{Season: 2025, Week: week(3)},
			},
			selected:    season(2026),
			wantSeasons: []int{2026, 2025},
			wantWeeks:   []int{1},
		},
		{
			name: "repeated periods collapse to one option each",
			periods: []repository.BetPeriod{
				{Season: 2026, Week: week(1)},
				{Season: 2026, Week: week(1)},
				{Season: 2026, Week: week(2)},
			},
			selected:    nil,
			wantSeasons: []int{2026},
			wantWeeks:   []int{1, 2},
		},
		{
			// A game with no week row still belongs to a season, so the season
			// stays selectable even though it contributes no week.
			name: "a bet on a game with no week contributes only its season",
			periods: []repository.BetPeriod{
				{Season: 2026, Week: nil},
				{Season: 2026, Week: week(4)},
			},
			selected:    nil,
			wantSeasons: []int{2026},
			wantWeeks:   []int{4},
		},
		{
			name:        "no bets means no options",
			periods:     nil,
			selected:    nil,
			wantSeasons: nil,
			wantWeeks:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seasons, weeks := filterOptions(tt.periods, tt.selected)

			if !slices.Equal(seasons, tt.wantSeasons) {
				t.Errorf("seasons = %v, want %v", seasons, tt.wantSeasons)
			}
			if !slices.Equal(weeks, tt.wantWeeks) {
				t.Errorf("weeks = %v, want %v", weeks, tt.wantWeeks)
			}
		})
	}
}

func TestWithCurrentLine(t *testing.T) {
	book := uuid.New()
	mine := uuid.New()
	options := []BetLineOption{{OddsID: book, Label: "DraftKings: GT -7 (-110) / CLEM +7 (-110)"}}

	t.Run("a bet on a book line is left alone", func(t *testing.T) {
		got := withCurrentLine(options, book, "Your line: GT -7 (-110)")
		if len(got) != 1 || got[0].OddsID != book {
			t.Errorf("options = %+v, want the book line unchanged", got)
		}
	})

	// Custom lines are not listed for anyone else, so a bet on one has nothing
	// to select. Without its own entry the select would open on the book line
	// and an edit that only changed the stake would move the bet onto it.
	t.Run("a bet on a custom line gets its own entry", func(t *testing.T) {
		got := withCurrentLine(options, mine, "Your line: GT -7 (-110)")
		if len(got) != 2 {
			t.Fatalf("options = %+v, want the bet's own line added", got)
		}
		if got[0].OddsID != mine {
			t.Errorf("the bet's own line is at %d, want first", 0)
		}
		if got[0].Label != "Your line: GT -7 (-110)" {
			t.Errorf("label = %q", got[0].Label)
		}
	})

	t.Run("the original slice is not modified", func(t *testing.T) {
		withCurrentLine(options, mine, "Your line")
		if len(options) != 1 {
			t.Errorf("the shared per-game options grew to %d", len(options))
		}
	})
}

// The union behind UserBetSummaries returns one row per bet, so the same pick
// placed in two leagues arrives twice. It is one fact to the reader, and
// "WGA +2000; WGA +2000" reads as a rendering bug rather than as two entries.
func TestUserBetSummariesCollapsesDuplicatePicks(t *testing.T) {
	gameA := uuid.New()
	gameB := uuid.New()
	line := func(value string) *string { return &value }

	moneyLine := func(game uuid.UUID) repository.UserBetRow {
		return repository.UserBetRow{
			GameID:       game,
			BetType:      BetTypeMoneyLine,
			Pick:         string(models.MoneyLinePickAway),
			OddsSnapshot: decimal.RequireFromString("2000"),
			HomeAbbr:     "GT",
			AwayAbbr:     "WGA",
		}
	}
	spread := repository.UserBetRow{
		GameID:       gameA,
		BetType:      BetTypeSpread,
		Pick:         string(models.SpreadPickHome),
		LineValue:    line("-7.5"),
		OddsSnapshot: decimal.RequireFromString("-110"),
		HomeAbbr:     "GT",
		AwayAbbr:     "WGA",
	}
	total := repository.UserBetRow{
		GameID:       gameA,
		BetType:      BetTypeOverUnder,
		Pick:         string(models.OverUnderPickOver),
		LineValue:    line("52.5"),
		OddsSnapshot: decimal.RequireFromString("-110"),
		HomeAbbr:     "GT",
		AwayAbbr:     "WGA",
	}

	tests := []struct {
		name string
		rows []repository.UserBetRow
		want map[uuid.UUID]string
	}{
		{"no bets", nil, map[uuid.UUID]string{}},
		{
			"one bet",
			[]repository.UserBetRow{moneyLine(gameA)},
			map[uuid.UUID]string{gameA: "WGA +2000"},
		},
		{
			"same pick in two leagues collapses",
			[]repository.UserBetRow{moneyLine(gameA), moneyLine(gameA)},
			map[uuid.UUID]string{gameA: "WGA +2000"},
		},
		{
			"different picks on one game are both kept",
			[]repository.UserBetRow{spread, total},
			map[uuid.UUID]string{gameA: "GT -7.5; Over 52.5"},
		},
		// Sorted, so the join is the same on every page load -- the union
		// query has no ORDER BY to lean on.
		{
			"pick order does not change the summary",
			[]repository.UserBetRow{total, spread},
			map[uuid.UUID]string{gameA: "GT -7.5; Over 52.5"},
		},
		{
			"games are summarised independently",
			[]repository.UserBetRow{moneyLine(gameA), moneyLine(gameB), moneyLine(gameB)},
			map[uuid.UUID]string{gameA: "WGA +2000", gameB: "WGA +2000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userBetSummaries(tt.rows)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d summaries, want %d: %v", len(got), len(tt.want), got)
			}
			for gameID, want := range tt.want {
				if got[gameID] != want {
					t.Errorf("summary[%s] = %q, want %q", gameID, got[gameID], want)
				}
			}
		})
	}
}
