package leagues

import (
	"errors"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

//go:fix inline
func intPtr(v int) *int { return new(v) }

func TestBuildWeeklyStats(t *testing.T) {
	me := uuid.New()
	alice := uuid.New()
	zed := uuid.New()

	stake := decimal.RequireFromString("1000")

	row := func(user uuid.UUID, name string, season, week *int, status, odds string) repository.LeagueBetRow {
		return repository.LeagueBetRow{
			UserID:       user,
			Username:     name,
			Season:       season,
			Week:         week,
			Status:       status,
			Stake:        stake,
			OddsSnapshot: decimal.RequireFromString(odds),
		}
	}

	rows := []repository.LeagueBetRow{
		// Week 1: my won/lost/push/pending mix, plus a void that must vanish.
		row(me, "Me", new(2026), new(1), "won", "129"),
		row(me, "Me", new(2026), new(1), "lost", "-110"),
		row(me, "Me", new(2026), new(1), "push", "-110"),
		row(me, "Me", new(2026), new(1), "pending", "-110"),
		row(me, "Me", new(2026), new(1), "void", "-110"),
		// Week 1: other members, out of alphabetical order.
		row(zed, "Zed", new(2026), new(1), "won", "-200"),
		row(alice, "alice", new(2026), new(1), "lost", "100"),
		// Week 2, newer, should sort first.
		row(alice, "alice", new(2026), new(2), "won", "150"),
		// A bet whose game has no calendar week, should sort last.
		row(me, "Me", nil, nil, "pending", "-110"),
	}

	weeks := buildWeeklyStats(rows, me)

	if len(weeks) != 3 {
		t.Fatalf("got %d week groups, want 3", len(weeks))
	}

	t.Run("week ordering newest first, unscheduled last", func(t *testing.T) {
		wantLabels := []string{"2026 · Week 2", "2026 · Week 1", "Unscheduled"}
		for i, want := range wantLabels {
			if weeks[i].Label != want {
				t.Errorf("weeks[%d].Label = %q, want %q", i, weeks[i].Label, want)
			}
		}
	})

	t.Run("current user first then case-insensitive alphabetical", func(t *testing.T) {
		week1 := weeks[1]
		if len(week1.Rows) != 3 {
			t.Fatalf("week 1 has %d rows, want 3", len(week1.Rows))
		}
		gotOrder := []string{week1.Rows[0].Username, week1.Rows[1].Username, week1.Rows[2].Username}
		wantOrder := []string{"Me", "alice", "Zed"}
		for i := range wantOrder {
			if gotOrder[i] != wantOrder[i] {
				t.Fatalf("week 1 row order = %v, want %v", gotOrder, wantOrder)
			}
		}
		if !week1.Rows[0].IsCurrentUser {
			t.Error("first row should be the current user")
		}
	})

	t.Run("aggregates counts and money", func(t *testing.T) {
		mine := weeks[1].Rows[0]
		if mine.Wins != 1 || mine.Losses != 1 || mine.Pushes != 1 || mine.Pending != 1 {
			t.Errorf("record = %dW-%dL-%dP, %d pending; want 1W-1L-1P, 1 pending",
				mine.Wins, mine.Losses, mine.Pushes, mine.Pending)
		}
		// Void bet excluded: 4 x 1000 staked, not 5.
		if want := decimal.RequireFromString("4000"); !mine.Staked.Equal(want) {
			t.Errorf("Staked = %s, want %s", mine.Staked, want)
		}
		// Won at +129 pays 2290.00; push refunds the 1000 stake.
		if want := decimal.RequireFromString("3290.00"); !mine.Winnings.Equal(want) {
			t.Errorf("Winnings = %s, want %s", mine.Winnings, want)
		}
		// Net: +1290 profit, -1000 lost, push and pending contribute nothing.
		if want := decimal.RequireFromString("290.00"); !mine.Net.Equal(want) {
			t.Errorf("Net = %s, want %s", mine.Net, want)
		}
	})

	t.Run("negative odds payout", func(t *testing.T) {
		zedRow := weeks[1].Rows[2]
		// Won at -200: 1000 stake returns 1500.
		if want := decimal.RequireFromString("1500.00"); !zedRow.Winnings.Equal(want) {
			t.Errorf("Winnings = %s, want %s", zedRow.Winnings, want)
		}
		if want := decimal.RequireFromString("500.00"); !zedRow.Net.Equal(want) {
			t.Errorf("Net = %s, want %s", zedRow.Net, want)
		}
	})
}

func TestBuildHolyLockWeeks(t *testing.T) {
	me := uuid.New()
	alice := uuid.New()
	zed := uuid.New()

	line := func(v string) *string { return &v }
	row := func(user uuid.UUID, name string, week int, seasonType, betType, pick string, lineValue *string, odds string) repository.LeagueHolyLockRow {
		return repository.LeagueHolyLockRow{
			UserID:       user,
			Username:     name,
			Season:       2026,
			Week:         week,
			SeasonType:   seasonType,
			BetType:      betType,
			Status:       "pending",
			Pick:         pick,
			LineValue:    lineValue,
			OddsSnapshot: decimal.RequireFromString(odds),
			Stake:        decimal.RequireFromString("50"),
			HomeAbbr:     "GT",
			AwayAbbr:     "CLEM",
		}
	}

	rows := []repository.LeagueHolyLockRow{
		// Week 1, deliberately out of alphabetical order.
		row(zed, "zed", 1, "regular", "overunder", "over", line("54.5"), "-110"),
		row(alice, "Alice", 1, "regular", "moneyline", "away", nil, "150"),
		row(me, "Me", 1, "regular", "spread", "home", line("-7.0"), "-110"),
		// Week 2.
		row(me, "Me", 2, "regular", "spread", "away", line("3.5"), "-110"),
		// Postseason week 1 must not merge with regular week 1.
		row(me, "Me", 1, "postseason", "overunder", "under", line("48.0"), "-110"),
	}

	weeks := buildHolyLockWeeks(rows, me)

	if len(weeks) != 3 {
		t.Fatalf("got %d week groups, want 3 (regular 1, regular 2, postseason 1)", len(weeks))
	}

	// Newest first, and the postseason sorts ahead of the regular season it follows.
	wantLabels := []string{"2026 · Week 1 · Postseason", "2026 · Week 2", "2026 · Week 1"}
	for i, want := range wantLabels {
		if weeks[i].Label != want {
			t.Errorf("week %d label = %q, want %q", i, weeks[i].Label, want)
		}
	}

	// Regular week 1: current user first, then case-insensitive alphabetical.
	regularWeek1 := weeks[2]
	wantOrder := []string{"Me", "Alice", "zed"}
	if len(regularWeek1.Rows) != len(wantOrder) {
		t.Fatalf("regular week 1 has %d rows, want %d", len(regularWeek1.Rows), len(wantOrder))
	}
	for i, want := range wantOrder {
		if regularWeek1.Rows[i].Username != want {
			t.Errorf("regular week 1 row %d is %q, want %q", i, regularWeek1.Rows[i].Username, want)
		}
	}
	if !regularWeek1.Rows[0].IsCurrentUser {
		t.Error("the current user's row is not flagged")
	}
	if regularWeek1.Rows[1].IsCurrentUser {
		t.Error("another member's row is flagged as the current user's")
	}

	// Each bet type renders its own pick shape.
	wantPicks := map[string]string{"Me": "GT -7", "Alice": "CLEM +150", "zed": "Over 54.5"}
	for _, entry := range regularWeek1.Rows {
		if got := entry.Pick; got != wantPicks[entry.Username] {
			t.Errorf("%s's pick = %q, want %q", entry.Username, got, wantPicks[entry.Username])
		}
		if entry.Matchup != "CLEM @ GT" {
			t.Errorf("%s's matchup = %q, want %q", entry.Username, entry.Matchup, "CLEM @ GT")
		}
	}

	// A positive spread keeps its sign, or a +3.5 dog reads as a favourite.
	if got := weeks[1].Rows[0].Pick; got != "CLEM +3.5" {
		t.Errorf("week 2 pick = %q, want %q", got, "CLEM +3.5")
	}
	if got := weeks[0].Rows[0].Pick; got != "Under 48" {
		t.Errorf("postseason pick = %q, want %q", got, "Under 48")
	}
}

func TestHolyLockWeekLabel(t *testing.T) {
	tests := []struct {
		season, week int
		seasonType   string
		want         string
	}{
		{2026, 1, "regular", "2026 · Week 1"},
		// Without the suffix this collides with the line above, and a member
		// legitimately holding both looks like a broken invariant.
		{2026, 1, "postseason", "2026 · Week 1 · Postseason"},
		{2026, -1, "regular", "2026"},
	}

	for _, tt := range tests {
		if got := holyLockWeekLabel(tt.season, tt.week, tt.seasonType); got != tt.want {
			t.Errorf("holyLockWeekLabel(%d, %d, %q) = %q, want %q", tt.season, tt.week, tt.seasonType, got, tt.want)
		}
	}
}

func TestValidateLeagueName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trims surrounding space", input: "  Sunday Money  ", want: "Sunday Money"},
		{name: "rejects empty", input: "", wantErr: ErrInvalidName},
		{name: "rejects whitespace only", input: "   \t\n ", wantErr: ErrInvalidName},
		{name: "accepts the longest name the column holds", input: strings.Repeat("a", MaxLeagueNameLength), want: strings.Repeat("a", MaxLeagueNameLength)},
		{name: "rejects one character more", input: strings.Repeat("a", MaxLeagueNameLength+1), wantErr: ErrInvalidName},
		// Postgres counts characters, not bytes, so a name of multi-byte
		// characters that fits must not be rejected for its byte length.
		{name: "counts characters not bytes", input: strings.Repeat("é", MaxLeagueNameLength), want: strings.Repeat("é", MaxLeagueNameLength)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateLeagueName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
		})
	}
}
