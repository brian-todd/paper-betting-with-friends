package bets

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestHolyLockEligible(t *testing.T) {
	now := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	weekID := uuid.New()
	football := func(offset time.Duration) models.Game {
		return models.Game{ScheduledAt: now.Add(offset), WeekID: &weekID}
	}

	tests := []struct {
		name   string
		status models.BetStatus
		game   models.Game
		frozen bool
		want   bool
	}{
		{"pending football bet before kickoff", models.BetStatusPending, football(time.Hour), false, true},
		{"pending bet after kickoff", models.BetStatusPending, football(-time.Hour), false, false},
		// Exactly at kickoff the game has started, and authorizeHolyLock
		// rejects it, so the page must not offer it either.
		{"pending bet at kickoff", models.BetStatusPending, football(0), false, false},
		{"settled win", models.BetStatusWon, football(time.Hour), false, false},
		{"settled loss", models.BetStatusLost, football(time.Hour), false, false},
		{"cancelled bet", models.BetStatusVoid, football(time.Hour), false, false},
		// Basketball games carry no week, so there is no slot to occupy.
		{"basketball bet", models.BetStatusPending, models.Game{ScheduledAt: now.Add(time.Hour)}, false, false},
		// The week's lock has kicked off, closing the week to everything else.
		{"frozen week", models.BetStatusPending, football(time.Hour), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := holyLockEligible(tt.status, tt.game, tt.frozen, now); got != tt.want {
				t.Errorf("holyLockEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFrozenHolyLockWeeks(t *testing.T) {
	now := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	leagueA, leagueB := uuid.New(), uuid.New()
	week1, week2 := uuid.New(), uuid.New()

	slot := func(league, week uuid.UUID, offset time.Duration) repository.HolyLockSlot {
		return repository.HolyLockSlot{
			LeagueID:    league,
			WeekID:      week,
			BetID:       uuid.New(),
			BetType:     BetTypeSpread,
			ScheduledAt: now.Add(offset),
		}
	}

	frozen := frozenHolyLockWeeks([]repository.HolyLockSlot{
		slot(leagueA, week1, -time.Hour), // kicked off
		slot(leagueA, week2, time.Hour),  // still open
		slot(leagueB, week1, time.Hour),  // same week, other league, still open
	}, now)

	if !frozen[holyLockWeek{league: leagueA, week: week1}] {
		t.Error("a week whose lock has kicked off should be frozen")
	}
	if frozen[holyLockWeek{league: leagueA, week: week2}] {
		t.Error("a week whose lock has not kicked off should stay open")
	}
	// The designation is per league, so one league's frozen week must not close
	// the same week in another.
	if frozen[holyLockWeek{league: leagueB, week: week1}] {
		t.Error("league A's frozen week leaked into league B")
	}

	if got := frozenHolyLockWeeks(nil, now); len(got) != 0 {
		t.Errorf("frozenHolyLockWeeks(nil) = %v, want an empty map", got)
	}
}

// A lock exactly at kickoff has started, matching holyLockEligible's boundary.
func TestFrozenHolyLockWeeksAtKickoff(t *testing.T) {
	now := time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	league, week := uuid.New(), uuid.New()

	frozen := frozenHolyLockWeeks([]repository.HolyLockSlot{
		{LeagueID: league, WeekID: week, BetID: uuid.New(), ScheduledAt: now},
	}, now)

	if !frozen[holyLockWeek{league: league, week: week}] {
		t.Error("a lock exactly at kickoff should freeze its week")
	}
}

func TestDescribeHolyLock(t *testing.T) {
	line := func(v string) *string { return &v }
	row := func(betType, pick string, lineValue *string, odds string) repository.LeagueHolyLockRow {
		return repository.LeagueHolyLockRow{
			BetType:      betType,
			Pick:         pick,
			LineValue:    lineValue,
			OddsSnapshot: decimal.RequireFromString(odds),
			HomeAbbr:     "GT",
			AwayAbbr:     "CLEM",
		}
	}

	tests := []struct {
		name string
		row  repository.LeagueHolyLockRow
		want string
	}{
		// The numeric column carries a trailing zero the reader should not see.
		{"spread on the favourite", row(BetTypeSpread, "home", line("-7.0"), "-110"), "GT -7 (CLEM @ GT)"},
		{"spread on the dog keeps its sign", row(BetTypeSpread, "away", line("3.5"), "-110"), "CLEM +3.5 (CLEM @ GT)"},
		{"money line", row(BetTypeMoneyLine, "away", nil, "150"), "CLEM +150 (CLEM @ GT)"},
		{"over", row(BetTypeOverUnder, "over", line("54.5"), "-110"), "Over 54.5 (CLEM @ GT)"},
		{"under", row(BetTypeOverUnder, "under", line("48.0"), "-110"), "Under 48 (CLEM @ GT)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeHolyLock(tt.row); got != tt.want {
				t.Errorf("describeHolyLock() = %q, want %q", got, tt.want)
			}
		})
	}
}
