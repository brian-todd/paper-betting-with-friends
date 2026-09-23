package bets

import (
	"errors"
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

// placeLock places a $10 home spread bet on game as a Holy Lock.
func (h *harness) placeLock(user *models.User, game *models.Game, spread *models.SpreadOdds) (*models.SpreadBet, error) {
	return h.svc.CreateSpreadBet(CreateSpreadBetInput{
		UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
		Pick: models.SpreadPickHome, Stake: dec("10"), OddsID: &spread.ID, HolyLock: true,
	})
}

// locked lists the spread bets flagged as a Holy Lock among ids.
func (h *harness) locked(ids ...uuid.UUID) []uuid.UUID {
	h.t.Helper()

	var flagged []uuid.UUID
	if err := h.db.Model(&models.SpreadBet{}).Where("id IN ? AND is_holy_lock", ids).Pluck("id", &flagged).Error; err != nil {
		h.t.Fatalf("reading holy locks: %v", err)
	}
	return flagged
}

// One Holy Lock per user, league and week is not something the schema can
// hold -- the week is two joins from the bet -- so every path that writes the
// flag has to keep it true itself. These are those paths, against real rows.
func TestHolyLockWrites(t *testing.T) {
	type setup struct {
		h                   *harness
		alice               *models.User
		early, late         *models.Game
		earlyLine, lateLine *models.SpreadOdds
	}
	// Two games in one week: early kicks off in an hour, late in five.
	arrange := func(t *testing.T) setup {
		h := newHarness(t)
		week := h.week()
		early, late := h.game(placedAt.Add(time.Hour), week), h.game(placedAt.Add(5*time.Hour), week)
		earlyLine, _, _ := h.lines(early)
		lateLine, _, _ := h.lines(late)
		return setup{h, h.member("alice", "1000"), early, late, earlyLine, lateLine}
	}

	t.Run("setting a lock moves it off the week's other bet", func(t *testing.T) {
		s := arrange(t)
		first := s.h.placeSpread(s.alice, s.early, s.earlyLine, models.SpreadPickHome, "10")
		second := s.h.placeSpread(s.alice, s.late, s.lateLine, models.SpreadPickHome, "10")

		if err := s.h.svc.SetHolyLock(BetTypeSpread, first.ID, s.alice.ID); err != nil {
			t.Fatalf("locking first: %v", err)
		}
		if err := s.h.svc.SetHolyLock(BetTypeSpread, second.ID, s.alice.ID); err != nil {
			t.Fatalf("locking second: %v", err)
		}
		if got := s.h.locked(first.ID, second.ID); len(got) != 1 || got[0] != second.ID {
			t.Errorf("locked = %v, want only the second bet", got)
		}

		if err := s.h.svc.ClearHolyLock(BetTypeSpread, second.ID, s.alice.ID); err != nil {
			t.Fatalf("clearing: %v", err)
		}
		if got := s.h.locked(first.ID, second.ID); len(got) != 0 {
			t.Errorf("locked after clear = %v, want none", got)
		}
	})

	t.Run("a lock whose game has kicked off cannot be moved", func(t *testing.T) {
		s := arrange(t)
		first := s.h.placeSpread(s.alice, s.early, s.earlyLine, models.SpreadPickHome, "10")
		second := s.h.placeSpread(s.alice, s.late, s.lateLine, models.SpreadPickHome, "10")
		if err := s.h.svc.SetHolyLock(BetTypeSpread, first.ID, s.alice.ID); err != nil {
			t.Fatalf("locking first: %v", err)
		}

		// Between the two kickoffs: the late game is still open, but the lock
		// is riding on a game being played.
		s.h.now = s.early.ScheduledAt
		if err := s.h.svc.SetHolyLock(BetTypeSpread, second.ID, s.alice.ID); !errors.Is(err, ErrHolyLockSettled) {
			t.Fatalf("moving a started lock: error = %v, want ErrHolyLockSettled", err)
		}
		if got := s.h.locked(first.ID, second.ID); len(got) != 1 || got[0] != first.ID {
			t.Errorf("locked = %v, want the first bet still", got)
		}
	})

	t.Run("placing into a taken slot is refused and moves no money", func(t *testing.T) {
		s := arrange(t)
		if _, err := s.h.placeLock(s.alice, s.early, s.earlyLine); err != nil {
			t.Fatalf("placing first lock: %v", err)
		}

		if _, err := s.h.placeLock(s.alice, s.late, s.lateLine); !errors.Is(err, ErrHolyLockExists) {
			t.Fatalf("second lock: error = %v, want ErrHolyLockExists", err)
		}
		var bets int64
		if err := s.h.db.Model(&models.SpreadBet{}).Where("game_id = ?", s.late.ID).Count(&bets).Error; err != nil {
			t.Fatalf("counting: %v", err)
		}
		if bets != 0 {
			t.Errorf("refused lock wrote %d bets", bets)
		}
		s.h.requireBalance(s.alice, "990")
	})

	t.Run("a cancelled lock gives the slot back", func(t *testing.T) {
		s := arrange(t)
		first, err := s.h.placeLock(s.alice, s.early, s.earlyLine)
		if err != nil {
			t.Fatalf("placing first lock: %v", err)
		}
		if err := s.h.svc.CancelSpreadBet(first.ID, s.alice.ID); err != nil {
			t.Fatalf("cancelling: %v", err)
		}
		if _, err := s.h.placeLock(s.alice, s.late, s.lateLine); err != nil {
			t.Errorf("locking after cancel: %v", err)
		}
	})

	t.Run("an admin void gives the slot back", func(t *testing.T) {
		// The admin path knows nothing about Holy Locks; the status <> 'void'
		// predicate in every slot query is what releases it.
		s := arrange(t)
		first, err := s.h.placeLock(s.alice, s.early, s.earlyLine)
		if err != nil {
			t.Fatalf("placing first lock: %v", err)
		}
		if err := s.h.svc.AdminVoidBet(BetTypeSpread, first.ID); err != nil {
			t.Fatalf("voiding: %v", err)
		}
		if _, err := s.h.placeLock(s.alice, s.late, s.lateLine); err != nil {
			t.Errorf("locking after void: %v", err)
		}
	})

	t.Run("a game with no week has no slot", func(t *testing.T) {
		s := arrange(t)
		unweeked := s.h.game(placedAt.Add(time.Hour), nil)
		line, _, _ := s.h.lines(unweeked)

		if _, err := s.h.placeLock(s.alice, unweeked, line); !errors.Is(err, ErrBetNotFootballWeek) {
			t.Fatalf("error = %v, want ErrBetNotFootballWeek", err)
		}
		bet := s.h.placeSpread(s.alice, unweeked, line, models.SpreadPickHome, "10")
		if err := s.h.svc.SetHolyLock(BetTypeSpread, bet.ID, s.alice.ID); !errors.Is(err, ErrBetNotFootballWeek) {
			t.Errorf("SetHolyLock error = %v, want ErrBetNotFootballWeek", err)
		}
	})

	t.Run("a member who has left cannot lock", func(t *testing.T) {
		s := arrange(t)
		bet := s.h.placeSpread(s.alice, s.early, s.earlyLine, models.SpreadPickHome, "10")
		if err := s.h.db.Where("league_id = ? AND user_id = ?", s.h.league.ID, s.alice.ID).Delete(&models.LeagueMember{}).Error; err != nil {
			t.Fatalf("leaving: %v", err)
		}

		if err := s.h.svc.SetHolyLock(BetTypeSpread, bet.ID, s.alice.ID); !errors.Is(err, ErrNotLeagueMember) {
			t.Errorf("error = %v, want ErrNotLeagueMember", err)
		}
	})
}
