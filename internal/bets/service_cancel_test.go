package bets

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// betKind adapts the three bet tables to one shape, so a rule that holds for
// every kind of bet is written once and run against each.
type betKind struct {
	name  string
	model any
	// create places a bet on game at a line belonging to linesOn, which is
	// game itself except in the test that staples one game's line onto another.
	create func(h *harness, user *models.User, game, linesOn *models.Game, stake string) (uuid.UUID, error)
	cancel func(h *harness, betID, userID uuid.UUID) error
	// cancelIfPending is the repository's conditional transition, the guard
	// the service's refund depends on.
	cancelIfPending func(h *harness, betID uuid.UUID) (bool, error)
}

// place is create for a test that is not about placement.
func (k betKind) place(h *harness, user *models.User, game *models.Game, stake string) uuid.UUID {
	h.t.Helper()

	id, err := k.create(h, user, game, game, stake)
	if err != nil {
		h.t.Fatalf("placing %s bet: %v", k.name, err)
	}
	return id
}

var betKinds = []betKind{
	{
		name:  "spread",
		model: &models.SpreadBet{},
		create: func(h *harness, user *models.User, game, linesOn *models.Game, stake string) (uuid.UUID, error) {
			spread, _, _ := h.lines(linesOn)
			bet, err := h.svc.CreateSpreadBet(CreateSpreadBetInput{
				UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
				Pick: models.SpreadPickHome, Stake: dec(stake), OddsID: &spread.ID,
			})
			if err != nil {
				return uuid.Nil, err
			}
			return bet.ID, nil
		},
		cancel: func(h *harness, betID, userID uuid.UUID) error { return h.svc.CancelSpreadBet(betID, userID) },
		cancelIfPending: func(h *harness, betID uuid.UUID) (bool, error) {
			return h.svc.spreadBetRepo.CancelIfPending(betID)
		},
	},
	{
		name:  "money line",
		model: &models.MoneyLineBet{},
		create: func(h *harness, user *models.User, game, linesOn *models.Game, stake string) (uuid.UUID, error) {
			_, moneyLine, _ := h.lines(linesOn)
			bet, err := h.svc.CreateMoneyLineBet(CreateMoneyLineBetInput{
				UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
				Pick: models.MoneyLinePickAway, Stake: dec(stake), OddsID: &moneyLine.ID,
			})
			if err != nil {
				return uuid.Nil, err
			}
			return bet.ID, nil
		},
		cancel: func(h *harness, betID, userID uuid.UUID) error { return h.svc.CancelMoneyLineBet(betID, userID) },
		cancelIfPending: func(h *harness, betID uuid.UUID) (bool, error) {
			return h.svc.moneyLineBetRepo.CancelIfPending(betID)
		},
	},
	{
		name:  "over/under",
		model: &models.OverUnderBet{},
		create: func(h *harness, user *models.User, game, linesOn *models.Game, stake string) (uuid.UUID, error) {
			_, _, total := h.lines(linesOn)
			bet, err := h.svc.CreateOverUnderBet(CreateOverUnderBetInput{
				UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
				Pick: models.OverUnderPickUnder, Stake: dec(stake), OddsID: &total.ID,
			})
			if err != nil {
				return uuid.Nil, err
			}
			return bet.ID, nil
		},
		cancel: func(h *harness, betID, userID uuid.UUID) error { return h.svc.CancelOverUnderBet(betID, userID) },
		cancelIfPending: func(h *harness, betID uuid.UUID) (bool, error) {
			return h.svc.overUnderBetRepo.CancelIfPending(betID)
		},
	},
}

// Cancelling is the other way money leaves a bet, and it is gated on the one
// thing a bettor could exploit: taking a stake back off a game they have seen
// start. Each case places a $100 bet from a $1,000 purse, so a refund reads as
// 1000 and a refusal as 900.
func TestCancelBet(t *testing.T) {
	kickoff := placedAt.Add(2 * time.Hour)

	cases := []struct {
		name string
		// arrange runs after the bet is placed, and returns who cancels.
		arrange func(h *harness, game *models.Game, owner *models.User) *models.User
		wantErr error
	}{
		{
			name:    "before kickoff, the owner gets the stake back",
			arrange: func(h *harness, _ *models.Game, owner *models.User) *models.User { return owner },
		},
		{
			name: "after kickoff, it is refused",
			arrange: func(h *harness, _ *models.Game, owner *models.User) *models.User {
				h.now = kickoff
				return owner
			},
			wantErr: ErrGameStarted,
		},
		{
			// The kickoff is the gate that goes stale, so a feed saying the game
			// is under way is believed even with the stored kickoff still ahead.
			name: "a game reported under way is refused before its stored kickoff",
			arrange: func(h *harness, game *models.Game, owner *models.User) *models.User {
				h.setStatus(game, models.GameStatusInProgress)
				return owner
			},
			wantErr: ErrGameStarted,
		},
		{
			// A game that will not be played is exactly when the stake should
			// come back, whatever the clock says.
			name: "a postponed game refunds after its kickoff",
			arrange: func(h *harness, game *models.Game, owner *models.User) *models.User {
				h.setStatus(game, models.GameStatusPostponed)
				h.now = kickoff.Add(time.Hour)
				return owner
			},
		},
		{
			name: "someone else's bet is refused",
			arrange: func(h *harness, _ *models.Game, _ *models.User) *models.User {
				return h.member("mallory", "1000")
			},
			wantErr: ErrNotBetOwner,
		},
	}

	for _, kind := range betKinds {
		for _, tc := range cases {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				h := newHarness(t)
				owner := h.member("alice", "1000")
				game := h.game(kickoff, nil)
				betID := kind.place(h, owner, game, "100")

				caller := tc.arrange(h, game, owner)
				err := kind.cancel(h, betID, caller.ID)

				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("cancel error = %v, want %v", err, tc.wantErr)
				}
				if tc.wantErr != nil {
					if got := h.status(kind.model, betID); got != models.BetStatusPending {
						t.Errorf("refused cancel left status %s, want pending", got)
					}
					h.requireBalance(owner, "900")
					return
				}
				if got := h.status(kind.model, betID); got != models.BetStatusVoid {
					t.Errorf("status = %s, want void", got)
				}
				h.requireBalance(owner, "1000")
			})
		}
	}
}

// A double-clicked cancel button sends two requests. Only one of them may
// refund.
func TestCancelBetRefundsOnce(t *testing.T) {
	for _, kind := range betKinds {
		t.Run(kind.name, func(t *testing.T) {
			h := newHarness(t)
			owner := h.member("alice", "1000")
			game := h.game(placedAt.Add(time.Hour), nil)
			betID := kind.place(h, owner, game, "100")

			if err := kind.cancel(h, betID, owner.ID); err != nil {
				t.Fatalf("first cancel: %v", err)
			}
			if err := kind.cancel(h, betID, owner.ID); !errors.Is(err, ErrBetNotPending) {
				t.Errorf("second cancel error = %v, want ErrBetNotPending", err)
			}
			h.requireBalance(owner, "1000")
		})
	}
}

// Calling the service twice in sequence cannot reach the race: the second call
// reads the bet as void and stops before the write. Two concurrent requests
// both read it as pending, so what stands between them and a double refund is
// the conditional update alone -- and it has to say which of them won.
func TestCancelIfPendingVoidsOnlyOnce(t *testing.T) {
	for _, kind := range betKinds {
		t.Run(kind.name, func(t *testing.T) {
			h := newHarness(t)
			owner := h.member("alice", "1000")
			game := h.game(placedAt.Add(time.Hour), nil)
			betID := kind.place(h, owner, game, "100")

			if err := h.db.Model(kind.model).Where("id = ?", betID).Update("is_holy_lock", true).Error; err != nil {
				t.Fatalf("marking holy lock: %v", err)
			}

			cancelled, err := kind.cancelIfPending(h, betID)
			if err != nil || !cancelled {
				t.Fatalf("first CancelIfPending = %v, %v; want true, nil", cancelled, err)
			}
			cancelled, err = kind.cancelIfPending(h, betID)
			if err != nil {
				t.Fatalf("second CancelIfPending: %v", err)
			}
			if cancelled {
				t.Error("second CancelIfPending reported voiding an already-void bet")
			}

			// A cancelled bet gives its week's Holy Lock slot back.
			var locked []bool
			if err := h.db.Model(kind.model).Where("id = ?", betID).Pluck("is_holy_lock", &locked).Error; err != nil {
				t.Fatalf("reading holy lock: %v", err)
			}
			if len(locked) != 1 || locked[0] {
				t.Errorf("is_holy_lock = %v after cancel, want [false]", locked)
			}
		})
	}
}

// Nor can a bet that has already settled be cancelled into a refund.
func TestCancelIfPendingLeavesASettledBetAlone(t *testing.T) {
	for _, kind := range betKinds {
		t.Run(kind.name, func(t *testing.T) {
			h := newHarness(t)
			owner := h.member("alice", "1000")
			game := h.game(placedAt.Add(time.Hour), nil)
			betID := kind.place(h, owner, game, "100")

			if err := h.db.Model(kind.model).Where("id = ?", betID).Update("status", models.BetStatusWon).Error; err != nil {
				t.Fatalf("settling: %v", err)
			}

			cancelled, err := kind.cancelIfPending(h, betID)
			if err != nil {
				t.Fatalf("CancelIfPending: %v", err)
			}
			if cancelled {
				t.Error("CancelIfPending voided a settled bet")
			}
			if got := h.status(kind.model, betID); got != models.BetStatusWon {
				t.Errorf("status = %s, want won", got)
			}
		})
	}
}
