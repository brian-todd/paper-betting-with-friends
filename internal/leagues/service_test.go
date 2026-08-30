package leagues

import (
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
