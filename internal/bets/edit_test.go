package bets

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// editKind extends betKind with an edit, and with the one column an edit is
// most likely to get silently wrong: the foreign key to the odds row. A
// whole-struct save wrote the preloaded odds row's ID back over it; the
// page-level test for that bug covers spread bets alone.
type editKind struct {
	betKind
	oddsColumn string
	// update moves the bet to the second book's line on game, at stake.
	update func(h *harness, betID, userID uuid.UUID, game *models.Game, stake string) error
	// secondLine is the id of the line update moves the bet to.
	secondLine func(h *harness, game *models.Game) uuid.UUID
}

// secondBook writes another book's lines for game, distinct from the harness's
// default, so a bet moved onto them has somewhere visibly different to point.
func secondBook(h *harness, game *models.Game) (*models.SpreadOdds, *models.MoneyLineOdds, *models.OverUnderOdds) {
	h.t.Helper()

	spread := &models.SpreadOdds{
		GameID: game.ID, Source: models.OddsSourceFanDuel,
		HomeSpread: dec("-4.5"), AwaySpread: dec("4.5"), HomeOdds: dec("-105"), AwayOdds: dec("-115"),
	}
	moneyLine := &models.MoneyLineOdds{
		GameID: game.ID, Source: models.OddsSourceFanDuel,
		HomeOdds: dec("-160"), AwayOdds: dec("140"),
	}
	total := &models.OverUnderOdds{
		GameID: game.ID, Source: models.OddsSourceFanDuel,
		Total: dec("47.5"), OverOdds: dec("-105"), UnderOdds: dec("-115"),
	}
	for _, row := range []any{spread, moneyLine, total} {
		if err := h.db.Create(row).Error; err != nil {
			h.t.Fatalf("creating second book: %v", err)
		}
	}
	return spread, moneyLine, total
}

func findOdds[T any](h *harness, game *models.Game, source models.OddsSource) uuid.UUID {
	h.t.Helper()

	var ids []uuid.UUID
	if err := h.db.Model(new(T)).Where("game_id = ? AND source = ?", game.ID, source).Pluck("id", &ids).Error; err != nil || len(ids) != 1 {
		h.t.Fatalf("finding %s odds: %v (%d rows)", source, err, len(ids))
	}
	return ids[0]
}

var editKinds = []editKind{
	{
		betKind:    betKinds[0],
		oddsColumn: "spread_odds_id",
		update: func(h *harness, betID, userID uuid.UUID, game *models.Game, stake string) error {
			oddsID := findOdds[models.SpreadOdds](h, game, models.OddsSourceFanDuel)
			_, err := h.svc.UpdateSpreadBet(UpdateSpreadBetInput{
				BetID: betID, UserID: userID, Pick: models.SpreadPickHome, Stake: dec(stake), OddsID: &oddsID,
			})
			return err
		},
		secondLine: func(h *harness, game *models.Game) uuid.UUID {
			return findOdds[models.SpreadOdds](h, game, models.OddsSourceFanDuel)
		},
	},
	{
		betKind:    betKinds[1],
		oddsColumn: "money_line_odds_id",
		update: func(h *harness, betID, userID uuid.UUID, game *models.Game, stake string) error {
			oddsID := findOdds[models.MoneyLineOdds](h, game, models.OddsSourceFanDuel)
			_, err := h.svc.UpdateMoneyLineBet(UpdateMoneyLineBetInput{
				BetID: betID, UserID: userID, Pick: models.MoneyLinePickAway, Stake: dec(stake), OddsID: &oddsID,
			})
			return err
		},
		secondLine: func(h *harness, game *models.Game) uuid.UUID {
			return findOdds[models.MoneyLineOdds](h, game, models.OddsSourceFanDuel)
		},
	},
	{
		betKind:    betKinds[2],
		oddsColumn: "over_under_odds_id",
		update: func(h *harness, betID, userID uuid.UUID, game *models.Game, stake string) error {
			oddsID := findOdds[models.OverUnderOdds](h, game, models.OddsSourceFanDuel)
			_, err := h.svc.UpdateOverUnderBet(UpdateOverUnderBetInput{
				BetID: betID, UserID: userID, Pick: models.OverUnderPickUnder, Stake: dec(stake), OddsID: &oddsID,
			})
			return err
		},
		secondLine: func(h *harness, game *models.Game) uuid.UUID {
			return findOdds[models.OverUnderOdds](h, game, models.OddsSourceFanDuel)
		},
	},
}

// oddsIDOf reads the foreign key a bet actually stores.
func (k editKind) oddsIDOf(h *harness, betID uuid.UUID) uuid.UUID {
	h.t.Helper()

	var ids []uuid.UUID
	if err := h.db.Model(k.model).Where("id = ?", betID).Pluck(k.oddsColumn, &ids).Error; err != nil || len(ids) != 1 {
		h.t.Fatalf("reading %s: %v", k.oddsColumn, err)
	}
	return ids[0]
}

// An edit moves only the difference in stake. Each case starts from a $100 bet
// out of a $1,000 purse, so the purse holds $900 before the edit.
func TestEditBetMovesTheStakeDifference(t *testing.T) {
	cases := []struct {
		name        string
		stake       string
		wantErr     error
		wantBalance string
	}{
		{name: "raising takes only the increase", stake: "150", wantBalance: "850"},
		{name: "lowering returns only the decrease", stake: "40", wantBalance: "960"},
		// The whole point of moving the difference: raising to $1,000 needs
		// the $900 that is free, not $1,000.
		{name: "raising to the whole purse needs only what is free", stake: "1000", wantBalance: "0"},
		{name: "raising past what is free is refused", stake: "1000.01", wantErr: ErrInsufficientFunds, wantBalance: "900"},
		{name: "a zero stake is refused", stake: "0", wantErr: ErrInvalidStake, wantBalance: "900"},
	}

	for _, kind := range editKinds {
		for _, tc := range cases {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				h := newHarness(t)
				alice := h.member("alice", "1000")
				game := h.game(placedAt.Add(time.Hour), nil)
				betID := kind.place(h, alice, game, "100")
				firstLine := kind.oddsIDOf(h, betID)
				secondBook(h, game)

				err := kind.update(h, betID, alice.ID, game, tc.stake)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				h.requireBalance(alice, tc.wantBalance)

				wantLine := kind.secondLine(h, game)
				if tc.wantErr != nil {
					wantLine = firstLine
				}
				if got := kind.oddsIDOf(h, betID); got != wantLine {
					t.Errorf("%s = %s, want %s", kind.oddsColumn, got, wantLine)
				}
			})
		}
	}
}

// The edit gates are the placement gates, and editable() on the page has to
// agree with every one of them.
func TestEditBetRefusals(t *testing.T) {
	kickoff := placedAt.Add(time.Hour)

	cases := []struct {
		name    string
		arrange func(h *harness, betKind editKind, betID uuid.UUID, owner *models.User) *models.User
		wantErr error
	}{
		{
			name: "at kickoff",
			arrange: func(h *harness, _ editKind, _ uuid.UUID, owner *models.User) *models.User {
				h.now = kickoff
				return owner
			},
			wantErr: ErrGameStarted,
		},
		{
			name: "by someone else",
			arrange: func(h *harness, _ editKind, _ uuid.UUID, _ *models.User) *models.User {
				return h.member("mallory", "1000")
			},
			wantErr: ErrNotBetOwner,
		},
		{
			name: "once settled",
			arrange: func(h *harness, kind editKind, betID uuid.UUID, owner *models.User) *models.User {
				if err := h.db.Model(kind.model).Where("id = ?", betID).Update("status", models.BetStatusLost).Error; err != nil {
					h.t.Fatalf("settling: %v", err)
				}
				return owner
			},
			wantErr: ErrBetNotPending,
		},
	}

	for _, kind := range editKinds {
		for _, tc := range cases {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				h := newHarness(t)
				alice := h.member("alice", "1000")
				game := h.game(kickoff, nil)
				betID := kind.place(h, alice, game, "100")
				secondBook(h, game)

				caller := tc.arrange(h, kind, betID, alice)
				if err := kind.update(h, betID, caller.ID, game, "200"); !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				h.requireBalance(alice, "900")
			})
		}
	}
}

// An edit reads the bet before it writes, and a cancel or a settlement can land
// in between. The write has to find the bet no longer pending and leave it
// alone: saving the struct it read would put a refunded or paid bet back in
// play with the stake the edit asked for.
//
// The service reads and writes within one call, so a test cannot slip a cancel
// between the two; this reads the bet the way the service does, voids it the way
// a concurrent cancel would, and then makes the write.
func TestUpdateIfPendingLeavesAMovedBetAlone(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")

	for _, kind := range editKinds {
		t.Run(kind.name, func(t *testing.T) {
			game := h.game(placedAt.Add(time.Hour), nil)
			betID := kind.place(h, alice, game, "100")

			var write func() (bool, error)
			switch kind.model.(type) {
			case *models.SpreadBet:
				bet, err := h.svc.spreadBetRepo.FindByID(betID)
				if err != nil {
					t.Fatalf("reading bet: %v", err)
				}
				bet.Stake = dec("500")
				write = func() (bool, error) { return h.svc.spreadBetRepo.UpdateIfPending(bet) }
			case *models.MoneyLineBet:
				bet, err := h.svc.moneyLineBetRepo.FindByID(betID)
				if err != nil {
					t.Fatalf("reading bet: %v", err)
				}
				bet.Stake = dec("500")
				write = func() (bool, error) { return h.svc.moneyLineBetRepo.UpdateIfPending(bet) }
			case *models.OverUnderBet:
				bet, err := h.svc.overUnderBetRepo.FindByID(betID)
				if err != nil {
					t.Fatalf("reading bet: %v", err)
				}
				bet.Stake = dec("500")
				write = func() (bool, error) { return h.svc.overUnderBetRepo.UpdateIfPending(bet) }
			}

			if err := kind.cancel(h, betID, alice.ID); err != nil {
				t.Fatalf("cancelling: %v", err)
			}

			updated, err := write()
			if err != nil {
				t.Fatalf("UpdateIfPending: %v", err)
			}
			if updated {
				t.Error("UpdateIfPending wrote to a void bet")
			}
			if got := h.status(kind.model, betID); got != models.BetStatusVoid {
				t.Errorf("status = %s, want void", got)
			}
			var stakes []string
			if err := h.db.Model(kind.model).Where("id = ?", betID).Pluck("stake::text", &stakes).Error; err != nil || len(stakes) != 1 {
				t.Fatalf("reading stake: %v", err)
			}
			if !dec(stakes[0]).Equal(dec("100")) {
				t.Errorf("stake = %s, want the original 100", stakes[0])
			}
		})
	}
}
