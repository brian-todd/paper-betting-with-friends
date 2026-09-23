package bets

import (
	"errors"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// Every refusal to place a bet has to leave the purse and the bet tables as it
// found them. The stake is deducted partway through placement, so a refusal
// that comes after the deduction and forgets to put it back is invisible from
// the returned error alone.
func TestPlaceBetRefusals(t *testing.T) {
	kickoff := placedAt.Add(2 * time.Hour)

	cases := []struct {
		name    string
		stake   string
		arrange func(h *harness, owner *models.User) (bettor *models.User, linesOn *models.Game)
		wantErr error
	}{
		{
			// The handler refuses these before the service sees them, but the
			// purse is the service's to protect: a negative stake passes
			// DeductStake's balance >= amount guard and credits the purse.
			name:    "a negative stake",
			stake:   "-100",
			wantErr: ErrInvalidStake,
		},
		{
			name:    "a zero stake",
			stake:   "0",
			wantErr: ErrInvalidStake,
		},
		{
			name:    "a stake larger than the purse",
			stake:   "1000.01",
			wantErr: ErrInsufficientFunds,
		},
		{
			name:  "a game already kicked off",
			stake: "100",
			arrange: func(h *harness, owner *models.User) (*models.User, *models.Game) {
				h.now = kickoff
				return owner, nil
			},
			wantErr: ErrGameStarted,
		},
		{
			name:  "a league the bettor is not in",
			stake: "100",
			arrange: func(h *harness, _ *models.User) (*models.User, *models.Game) {
				return h.user("outsider"), nil
			},
			wantErr: ErrNotLeagueMember,
		},
		{
			// An odds row belongs to one game. Taking it on another would let a
			// caller staple any line onto any matchup.
			name:  "a line from a different game",
			stake: "100",
			arrange: func(h *harness, owner *models.User) (*models.User, *models.Game) {
				return owner, h.game(kickoff, nil)
			},
			wantErr: ErrOddsNotFound,
		},
	}

	for _, kind := range betKinds {
		for _, tc := range cases {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				h := newHarness(t)
				owner := h.member("alice", "1000")
				game := h.game(kickoff, nil)

				bettor, linesOn := owner, game
				if tc.arrange != nil {
					var other *models.Game
					bettor, other = tc.arrange(h, owner)
					if other != nil {
						linesOn = other
					}
				}

				_, err := kind.create(h, bettor, game, linesOn, tc.stake)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}

				var bets int64
				if err := h.db.Model(kind.model).Where("game_id = ?", game.ID).Count(&bets).Error; err != nil {
					t.Fatalf("counting bets: %v", err)
				}
				if bets != 0 {
					t.Errorf("refused placement wrote %d bets", bets)
				}
				h.requireBalance(owner, "1000")
			})
		}
	}
}

// A bet freezes the side it picked, not the home side and not whatever the
// first column of the odds row happens to be.
func TestPlaceBetSnapshotsThePickedSide(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")
	game := h.game(placedAt.Add(time.Hour), nil)
	spread, moneyLine, total := h.lines(game)

	awaySpread := h.placeSpread(alice, game, spread, models.SpreadPickAway, "10")
	if !awaySpread.SpreadSnapshot.Equal(dec("3.5")) || !awaySpread.OddsSnapshot.Equal(dec("-110")) {
		t.Errorf("away spread snapshot = %s at %s, want 3.5 at -110", awaySpread.SpreadSnapshot, awaySpread.OddsSnapshot)
	}

	awayML := h.placeMoneyLine(alice, game, moneyLine, models.MoneyLinePickAway, "10")
	if !awayML.OddsSnapshot.Equal(dec("130")) {
		t.Errorf("away money line snapshot = %s, want 130", awayML.OddsSnapshot)
	}

	// Distinct over and under prices, so taking the wrong one shows.
	if err := h.db.Model(total).Update("under_odds", dec("-120")).Error; err != nil {
		t.Fatalf("moving under price: %v", err)
	}
	under := h.placeOverUnder(alice, game, total, models.OverUnderPickUnder, "10")
	if !under.TotalSnapshot.Equal(dec("45.5")) || !under.OddsSnapshot.Equal(dec("-120")) {
		t.Errorf("under snapshot = %s at %s, want 45.5 at -120", under.TotalSnapshot, under.OddsSnapshot)
	}

	h.requireBalance(alice, "970")
}

// A custom spread is entered from the picked side's point of view and stored as
// a whole line, so the other side is its mirror.
func TestPlaceBetWithCustomLineWritesACustomOddsRow(t *testing.T) {
	h := newHarness(t)
	alice := h.member("alice", "1000")
	game := h.game(placedAt.Add(time.Hour), nil)

	customSpread, customOdds := dec("7"), dec("-105")
	bet, err := h.svc.CreateSpreadBet(CreateSpreadBetInput{
		UserID: alice.ID, LeagueID: h.league.ID, GameID: game.ID,
		Pick: models.SpreadPickAway, Stake: dec("10"),
		CustomSpread: &customSpread, CustomOdds: &customOdds,
	})
	if err != nil {
		t.Fatalf("placing custom spread: %v", err)
	}

	var odds models.SpreadOdds
	if err := h.db.First(&odds, "id = ?", bet.SpreadOddsID).Error; err != nil {
		t.Fatalf("reading custom odds: %v", err)
	}
	if odds.Source != models.OddsSourceCustom {
		t.Errorf("source = %s, want custom", odds.Source)
	}
	if odds.GameID != game.ID {
		t.Errorf("custom odds on game %s, want %s", odds.GameID, game.ID)
	}
	if !odds.AwaySpread.Equal(dec("7")) || !odds.HomeSpread.Equal(dec("-7")) {
		t.Errorf("custom line = home %s / away %s, want -7 / 7", odds.HomeSpread, odds.AwaySpread)
	}
	if !bet.SpreadSnapshot.Equal(dec("7")) || !bet.OddsSnapshot.Equal(dec("-105")) {
		t.Errorf("snapshot = %s at %s, want 7 at -105", bet.SpreadSnapshot, bet.OddsSnapshot)
	}
}
