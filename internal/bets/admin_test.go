package bets

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
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
