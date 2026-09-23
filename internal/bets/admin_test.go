package bets

import (
	"errors"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TestPurseDelta pins the money movement for every status transition an admin
// can force. The delta is what actually lands on a purse, so a sign error here
// silently pays or charges a real balance.
func TestPurseDelta(t *testing.T) {
	stake := decimal.RequireFromString("100")
	// -110 pays 90.909090909090909091 profit, so a win returns 190.91 total.
	odds := decimal.RequireFromString("-110")
	payout := calculatePayout(stake, odds)

	tests := []struct {
		name string
		from models.BetStatus
		to   models.BetStatus
		want decimal.Decimal
	}{
		{"pending to won pays stake plus winnings", models.BetStatusPending, models.BetStatusWon, payout},
		{"pending to lost moves nothing", models.BetStatusPending, models.BetStatusLost, decimal.Zero},
		{"pending to push refunds the stake", models.BetStatusPending, models.BetStatusPush, stake},
		{"pending to void refunds the stake", models.BetStatusPending, models.BetStatusVoid, stake},

		// Corrections run the same subtraction backwards.
		{"won to lost claws back the whole payout", models.BetStatusWon, models.BetStatusLost, payout.Neg()},
		{"won to pending claws back the whole payout", models.BetStatusWon, models.BetStatusPending, payout.Neg()},
		{"lost to won pays the full payout", models.BetStatusLost, models.BetStatusWon, payout},
		{"push to lost takes the refunded stake back", models.BetStatusPush, models.BetStatusLost, stake.Neg()},
		{"void to pending takes the refunded stake back", models.BetStatusVoid, models.BetStatusPending, stake.Neg()},
		{"push to won tops the refund up to the payout", models.BetStatusPush, models.BetStatusWon, payout.Sub(stake)},
		{"won to push gives back only the stake", models.BetStatusWon, models.BetStatusPush, stake.Sub(payout)},

		// Statuses that owe the purse the same amount cost nothing to swap.
		{"push to void moves nothing", models.BetStatusPush, models.BetStatusVoid, decimal.Zero},
		{"pending to pending moves nothing", models.BetStatusPending, models.BetStatusPending, decimal.Zero},
		{"lost to pending moves nothing", models.BetStatusLost, models.BetStatusPending, decimal.Zero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := purseDelta(stake, odds, tt.from, tt.to)
			if !got.Equal(tt.want) {
				t.Errorf("purseDelta(%s, %s) = %s, want %s", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// TestPurseDeltaRoundTripsToZero is the property that makes a mis-settlement
// safe to undo: whatever route a bet takes between statuses, returning it to
// where it started must leave the purse exactly as it was.
func TestPurseDeltaRoundTripsToZero(t *testing.T) {
	stake := decimal.RequireFromString("25.50")
	odds := decimal.RequireFromString("+250")

	statuses := []models.BetStatus{
		models.BetStatusPending,
		models.BetStatusWon,
		models.BetStatusLost,
		models.BetStatusPush,
		models.BetStatusVoid,
	}

	for _, from := range statuses {
		for _, via := range statuses {
			total := purseDelta(stake, odds, from, via).Add(purseDelta(stake, odds, via, from))
			if !total.IsZero() {
				t.Errorf("%s -> %s -> %s moved %s, want 0", from, via, from, total)
			}
		}
	}
}

func TestValidBetStatus(t *testing.T) {
	tests := []struct {
		status models.BetStatus
		want   bool
	}{
		{models.BetStatusPending, true},
		{models.BetStatusWon, true},
		{models.BetStatusLost, true},
		{models.BetStatusPush, true},
		{models.BetStatusVoid, true},
		{models.BetStatus(""), false},
		{models.BetStatus("settled"), false},
		{models.BetStatus("WON"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := validBetStatus(tt.status); got != tt.want {
				t.Errorf("validBetStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// An admin correction walks a bet through statuses in any order, and the purse
// has to track every step -- including the backwards ones, which purseDelta
// computes but only a database can show landing. A $100 money line bet at +130
// makes each position a round number: pending and lost hold nothing back, push
// and void return 100, won returns 230.
func TestAdminSetBetStatusMovesThePurse(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")
	game := h.game(placedAt.Add(time.Hour), nil)
	_, moneyLine, _ := h.lines(game)
	bet := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "100")

	// After the game, as a real correction would be.
	h.now = placedAt.Add(6 * time.Hour)

	for _, step := range []struct {
		to          models.BetStatus
		wantBalance string
	}{
		{models.BetStatusWon, "1130"},
		{models.BetStatusWon, "1130"}, // setting the same status again moves nothing
		{models.BetStatusLost, "900"},
		{models.BetStatusPush, "1000"},
		{models.BetStatusVoid, "1000"},
		{models.BetStatusPending, "900"},
	} {
		if err := h.svc.AdminSetBetStatus(BetTypeMoneyLine, bet.ID, step.to); err != nil {
			t.Fatalf("setting %s: %v", step.to, err)
		}
		if got := h.status(&models.MoneyLineBet{}, bet.ID); got != step.to {
			t.Errorf("status = %s, want %s", got, step.to)
		}
		if got := h.balance(alice); !got.Equal(dec(step.wantBalance)) {
			t.Errorf("after %s: balance = %s, want %s", step.to, got, step.wantBalance)
		}
	}
}

// A correction has to land even against a purse spent down since the payout,
// or the bet's status and the balance disagree for good. A negative balance is
// visible and fixable; a refused correction is neither.
func TestAdminSetBetStatusCanOverdrawAPurse(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")
	game := h.game(placedAt.Add(time.Hour), nil)
	_, moneyLine, _ := h.lines(game)
	bet := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "100")

	if err := h.svc.AdminSetBetStatus(BetTypeMoneyLine, bet.ID, models.BetStatusWon); err != nil {
		t.Fatalf("paying out: %v", err)
	}
	if err := h.db.Model(&models.Purse{}).Where("user_id = ?", alice.ID).Update("balance", dec("30")).Error; err != nil {
		t.Fatalf("spending down: %v", err)
	}

	if err := h.svc.AdminSetBetStatus(BetTypeMoneyLine, bet.ID, models.BetStatusLost); err != nil {
		t.Fatalf("correcting to lost: %v", err)
	}
	h.requireBalance(alice, "-200")
}

func TestAdminSetBetStatusRefusals(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")
	game := h.game(placedAt.Add(time.Hour), nil)
	_, moneyLine, _ := h.lines(game)
	bet := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "100")

	for _, tc := range []struct {
		name    string
		betType string
		betID   func() uuid.UUID
		to      models.BetStatus
		wantErr error
	}{
		{"an unknown status", BetTypeMoneyLine, func() uuid.UUID { return bet.ID }, "cashed_out", ErrInvalidBetStatus},
		{"an unknown bet type", "parlay", func() uuid.UUID { return bet.ID }, models.BetStatusWon, ErrInvalidBetType},
		// The id is real but belongs to another table: the type decides where
		// to look, so this is a missing bet, not a money line one.
		{"the wrong table for the id", BetTypeSpread, func() uuid.UUID { return bet.ID }, models.BetStatusWon, ErrBetNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := h.svc.AdminSetBetStatus(tc.betType, tc.betID(), tc.to); !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
			h.requireBalance(alice, "900")
		})
	}
}

// FinalizeGameResult is the operator's answer to a feed that reports a score
// and never calls the game over. It stamps finality from the service's clock
// and settles in the same act.
func TestFinalizeGameResultSettlesAProvisionalScore(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")
	game := h.game(placedAt.Add(time.Hour), nil)
	_, moneyLine, _ := h.lines(game)
	bet := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "100")

	h.score(game, 10, 17, nil)
	h.now = placedAt.Add(8 * time.Hour)

	if err := h.svc.FinalizeGameResult(game.ID); err != nil {
		t.Fatalf("FinalizeGameResult: %v", err)
	}

	result, err := h.svc.FindGameResult(game.ID)
	if err != nil || result == nil {
		t.Fatalf("FindGameResult = %v, %v", result, err)
	}
	if result.FinalizedAt == nil || !result.FinalizedAt.Equal(h.now) {
		t.Errorf("finalized_at = %v, want the service clock %v", result.FinalizedAt, h.now)
	}
	if got := h.status(&models.MoneyLineBet{}, bet.ID); got != models.BetStatusWon {
		t.Errorf("status = %s, want won", got)
	}
	h.requireBalance(alice, "1130")

	if err := h.svc.FinalizeGameResult(h.game(placedAt, nil).ID); !errors.Is(err, ErrGameNotFound) {
		t.Errorf("finalizing a game with no score: error = %v, want ErrGameNotFound", err)
	}
}
