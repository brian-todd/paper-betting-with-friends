package bets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
)

// Settlement is the one path that pays money out, and every guard on it exists
// because a real feed did the thing the guard refuses: reported a score before
// the game ended, called a game final minutes after kickoff, or had two callers
// reach the same game at once. So these tests drive the sweep the scheduler
// runs, through each of those states in the order a real Saturday produces them,
// and read the purse back from the database at every step.
func TestSettleFinalGamesPaysEachBetOnceAtItsSnapshot(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")

	kickoff := placedAt.Add(3 * time.Hour)
	game := h.game(kickoff, nil)
	spread, moneyLine, total := h.lines(game)

	// Home wins 24-20: covers -3.5 by half a point, and 44 stays under 45.5.
	homeCovers := h.placeSpread(alice, game, spread, models.SpreadPickHome, "110")        // won at -110: 210
	awayCovers := h.placeSpread(alice, game, spread, models.SpreadPickAway, "55")         // lost
	awayWins := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "100") // lost
	homeWins := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickHome, "150") // won at -150: 250
	over := h.placeOverUnder(alice, game, total, models.OverUnderPickOver, "110")         // lost
	under := h.placeOverUnder(alice, game, total, models.OverUnderPickUnder, "22")        // won at -110: 42

	h.requireBalance(alice, "453") // 1000 - 547 staked

	// The book moves every line after the bets are in. A bet pays at the
	// numbers it was agreed at, so none of this may reach the purse.
	for _, update := range []struct {
		model  any
		column string
		value  string
	}{
		{&models.SpreadOdds{}, "home_odds", "200"},
		{&models.SpreadOdds{}, "home_spread", "-10.5"},
		{&models.MoneyLineOdds{}, "home_odds", "300"},
		{&models.OverUnderOdds{}, "under_odds", "500"},
	} {
		if err := h.db.Model(update.model).Where("game_id = ?", game.ID).Update(update.column, dec(update.value)).Error; err != nil {
			t.Fatalf("moving the line: %v", err)
		}
	}

	pending := map[string]func() models.BetStatus{
		"home spread": func() models.BetStatus { return h.status(&models.SpreadBet{}, homeCovers.ID) },
		"away spread": func() models.BetStatus { return h.status(&models.SpreadBet{}, awayCovers.ID) },
		"away ML":     func() models.BetStatus { return h.status(&models.MoneyLineBet{}, awayWins.ID) },
		"home ML":     func() models.BetStatus { return h.status(&models.MoneyLineBet{}, homeWins.ID) },
		"over":        func() models.BetStatus { return h.status(&models.OverUnderBet{}, over.ID) },
		"under":       func() models.BetStatus { return h.status(&models.OverUnderBet{}, under.ID) },
	}
	requireAllPending := func(t *testing.T) {
		t.Helper()
		for name, status := range pending {
			if got := status(); got != models.BetStatusPending {
				t.Errorf("%s: status = %s, want pending", name, got)
			}
		}
		h.requireBalance(alice, "453")
	}

	t.Run("a provisional score settles nothing", func(t *testing.T) {
		// The scoreboard writes a result row as soon as there are points. At
		// halftime the home side leads, and paying on that is the bug IsFinal
		// exists to prevent.
		h.score(game, 24, 20, nil)
		h.now = kickoff.Add(3 * time.Hour)

		if err := h.svc.SettleFinalGames(context.Background()); err != nil {
			t.Fatalf("SettleFinalGames: %v", err)
		}
		if err := h.svc.EvaluateBetsForGame(game.ID); err != nil {
			t.Fatalf("EvaluateBetsForGame: %v", err)
		}
		requireAllPending(t)
	})

	t.Run("a final sooner than a game can be played is not believed yet", func(t *testing.T) {
		finalized := kickoff.Add(time.Hour)
		if err := h.db.Model(&models.GameResult{}).Where("game_id = ?", game.ID).Update("finalized_at", finalized).Error; err != nil {
			t.Fatalf("finalizing: %v", err)
		}
		// Literal rather than minTimeToPlay, so that shrinking the floor fails
		// here instead of moving this clock along with it.
		h.now = kickoff.Add(89 * time.Minute)

		if err := h.svc.SettleFinalGames(context.Background()); err != nil {
			t.Fatalf("SettleFinalGames: %v", err)
		}
		requireAllPending(t)
	})

	t.Run("once played out, every bet settles and pays at its snapshot", func(t *testing.T) {
		h.now = kickoff.Add(90 * time.Minute)

		if err := h.svc.SettleFinalGames(context.Background()); err != nil {
			t.Fatalf("SettleFinalGames: %v", err)
		}

		want := map[string]models.BetStatus{
			"home spread": models.BetStatusWon,
			"away spread": models.BetStatusLost,
			"away ML":     models.BetStatusLost,
			"home ML":     models.BetStatusWon,
			"over":        models.BetStatusLost,
			"under":       models.BetStatusWon,
		}
		for name, status := range pending {
			if got := status(); got != want[name] {
				t.Errorf("%s: status = %s, want %s", name, got, want[name])
			}
		}
		h.requireBalance(alice, "955") // 453 + 210 + 250 + 42
	})

	t.Run("settling again pays nothing", func(t *testing.T) {
		if err := h.svc.SettleFinalGames(context.Background()); err != nil {
			t.Fatalf("SettleFinalGames: %v", err)
		}
		if err := h.svc.EvaluateBetsForGame(game.ID); err != nil {
			t.Fatalf("EvaluateBetsForGame: %v", err)
		}
		h.requireBalance(alice, "955")

		awaiting, err := h.svc.settlementRepo.FindGamesAwaitingSettlement(h.now)
		if err != nil {
			t.Fatalf("FindGamesAwaitingSettlement: %v", err)
		}
		if len(awaiting) != 0 {
			t.Errorf("a settled game is still awaiting settlement: %v", awaiting)
		}
	})
}

// A push hands the stake back and nothing more. It needs a whole-number line to
// land on, so the total is a custom one and the money line needs a tie.
func TestSettlementRefundsAPush(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")

	kickoff := placedAt.Add(time.Hour)
	game := h.game(kickoff, nil)
	_, moneyLine, _ := h.lines(game)

	h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickHome, "150")
	customTotal, customOdds := dec("42"), dec("-110")
	if _, err := h.svc.CreateOverUnderBet(CreateOverUnderBetInput{
		UserID: alice.ID, LeagueID: h.league.ID, GameID: game.ID,
		Pick: models.OverUnderPickOver, Stake: dec("50"),
		CustomTotal: &customTotal, CustomOverOdds: &customOdds, CustomUnderOdds: &customOdds,
	}); err != nil {
		t.Fatalf("placing custom total: %v", err)
	}
	h.requireBalance(alice, "800")

	finalized := kickoff.Add(4 * time.Hour)
	h.score(game, 21, 21, &finalized)
	h.now = finalized

	if err := h.svc.SettleFinalGames(context.Background()); err != nil {
		t.Fatalf("SettleFinalGames: %v", err)
	}

	for _, model := range []any{&models.MoneyLineBet{}, &models.OverUnderBet{}} {
		var statuses []models.BetStatus
		if err := h.db.Model(model).Where("game_id = ?", game.ID).Pluck("status", &statuses).Error; err != nil {
			t.Fatalf("reading statuses: %v", err)
		}
		if len(statuses) != 1 || statuses[0] != models.BetStatusPush {
			t.Errorf("%T statuses = %v, want [push]", model, statuses)
		}
	}
	h.requireBalance(alice, "1000")
}

// SettleIfPending is what lets two callers reach one finalized game -- the sweep
// and a /games run that has just written its result -- without both paying.
// Only the call that moves the bet may credit the purse, so the second call has
// to report that it moved nothing.
//
// EvaluateBetsForGame cannot show this by being called twice: its second call
// reads no pending bets and never reaches the guard. The race is between the
// read and the write, which is exactly where this sits.
func TestSettleIfPendingMovesABetOnlyOnce(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")

	game := h.game(placedAt.Add(time.Hour), nil)
	spread, moneyLine, total := h.lines(game)

	spreadBet := h.placeSpread(alice, game, spread, models.SpreadPickHome, "10")
	moneyLineBet := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickHome, "10")
	overUnderBet := h.placeOverUnder(alice, game, total, models.OverUnderPickOver, "10")

	for _, tc := range []struct {
		name   string
		settle func(models.BetStatus) (bool, error)
		status func() models.BetStatus
	}{
		{
			"spread",
			func(to models.BetStatus) (bool, error) { return h.svc.spreadBetRepo.SettleIfPending(spreadBet.ID, to) },
			func() models.BetStatus { return h.status(&models.SpreadBet{}, spreadBet.ID) },
		},
		{
			"money line",
			func(to models.BetStatus) (bool, error) {
				return h.svc.moneyLineBetRepo.SettleIfPending(moneyLineBet.ID, to)
			},
			func() models.BetStatus { return h.status(&models.MoneyLineBet{}, moneyLineBet.ID) },
		},
		{
			"over/under",
			func(to models.BetStatus) (bool, error) {
				return h.svc.overUnderBetRepo.SettleIfPending(overUnderBet.ID, to)
			},
			func() models.BetStatus { return h.status(&models.OverUnderBet{}, overUnderBet.ID) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moved, err := tc.settle(models.BetStatusWon)
			if err != nil || !moved {
				t.Fatalf("first settle = %v, %v; want true, nil", moved, err)
			}

			moved, err = tc.settle(models.BetStatusLost)
			if err != nil {
				t.Fatalf("second settle: %v", err)
			}
			if moved {
				t.Error("second settle reported moving an already-settled bet")
			}
			if got := tc.status(); got != models.BetStatusWon {
				t.Errorf("status = %s, want won (the first settlement)", got)
			}
		})
	}
}

// Settling a bet and paying it are one act. When the payout cannot land, the
// bet has to stay pending -- the sweep only ever looks at pending bets, so one
// left settled and unpaid would never be looked at again.
func TestSettlementThatCannotPayLeavesTheBetForTheNextSweep(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")

	kickoff := placedAt.Add(time.Hour)
	game := h.game(kickoff, nil)
	_, moneyLine, _ := h.lines(game)
	bet := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "100")

	finalized := kickoff.Add(4 * time.Hour)
	h.score(game, 10, 17, &finalized)
	h.now = finalized

	h.dropPurse(alice)
	if err := h.svc.SettleFinalGames(context.Background()); !errors.Is(err, repository.ErrPurseNotFound) {
		t.Fatalf("SettleFinalGames error = %v, want ErrPurseNotFound", err)
	}
	if got := h.status(&models.MoneyLineBet{}, bet.ID); got != models.BetStatusPending {
		t.Fatalf("status after a failed payout = %s, want pending", got)
	}

	h.restorePurse(alice, "900")
	if err := h.svc.SettleFinalGames(context.Background()); err != nil {
		t.Fatalf("retrying the sweep: %v", err)
	}
	if got := h.status(&models.MoneyLineBet{}, bet.ID); got != models.BetStatusWon {
		t.Errorf("status after the retry = %s, want won", got)
	}
	h.requireBalance(alice, "1130")
}
