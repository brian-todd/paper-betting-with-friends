package bets

// The harness for the service tests that need a database: a league with a
// member and a purse, a game, a line on it, and a clock the test moves.
//
// These are logic tests, so every row is built by hand and kept minimal -- see
// AGENTS.md on choosing between Insert* and a fixture seed. What the rows are
// for is the money: a balance read back from Postgres after the service has
// moved it, which no pure function in this package can stand in for.

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// placedAt is when a harness's bets are placed: a Saturday morning, with every
// kickoff the harness writes later that day.
var placedAt = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

type harness struct {
	t      *testing.T
	db     *gorm.DB
	svc    *Service
	league *models.League
	owner  *models.User

	// now is what the service's clock reads. Tests move it forward past a
	// kickoff to watch a gate close.
	now time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{t: t, db: testdb.Open(t), now: placedAt}
	h.svc = NewService(h.db)
	h.svc.SetClock(func() time.Time { return h.now })

	h.owner = h.user("owner")
	h.league = &models.League{Name: "Harness League " + uuid.NewString()[:8], CreatedBy: h.owner.ID}
	if err := h.db.Create(h.league).Error; err != nil {
		t.Fatalf("creating league: %v", err)
	}
	h.join(h.owner, "1000")

	return h
}

// user writes a user who belongs to nothing yet.
func (h *harness) user(name string) *models.User {
	h.t.Helper()

	user := &models.User{Username: name + "-" + uuid.NewString()[:8], PasswordHash: "unused"}
	if err := h.db.Create(user).Error; err != nil {
		h.t.Fatalf("creating user %q: %v", name, err)
	}
	return user
}

// join puts user in the harness league with a purse holding balance.
func (h *harness) join(user *models.User, balance string) {
	h.t.Helper()

	member := &models.LeagueMember{LeagueID: h.league.ID, UserID: user.ID, Role: "member"}
	if err := h.db.Create(member).Error; err != nil {
		h.t.Fatalf("adding member: %v", err)
	}
	purse := &models.Purse{UserID: user.ID, LeagueID: h.league.ID, Balance: decimal.RequireFromString(balance)}
	if err := h.db.Create(purse).Error; err != nil {
		h.t.Fatalf("opening purse: %v", err)
	}
}

// member is a new user already in the league with a purse.
func (h *harness) member(name, balance string) *models.User {
	h.t.Helper()

	user := h.user(name)
	h.join(user, balance)
	return user
}

// week writes a football week around placedAt, for the Holy Lock rules.
func (h *harness) week() *models.Week {
	h.t.Helper()

	week := &models.Week{
		Season:     2026,
		Number:     7,
		SeasonType: models.SeasonTypeRegular,
		StartDate:  placedAt.AddDate(0, 0, -3),
		EndDate:    placedAt.AddDate(0, 0, 3),
	}
	if err := h.db.Create(week).Error; err != nil {
		h.t.Fatalf("creating week: %v", err)
	}
	return week
}

// game writes a game kicking off at kickoff, in week when one is given.
func (h *harness) game(kickoff time.Time, week *models.Week) *models.Game {
	h.t.Helper()

	game := models.Game{ScheduledAt: kickoff}
	if week != nil {
		game.WeekID = &week.ID
	}
	return testdb.InsertGame(h.t, h.db, game)
}

// lines writes one book's spread, money line and total for game: home -3.5
// and 45.5 at -110 each way, the money line home -150 / away +130.
func (h *harness) lines(game *models.Game) (spread *models.SpreadOdds, moneyLine *models.MoneyLineOdds, total *models.OverUnderOdds) {
	h.t.Helper()

	spread = &models.SpreadOdds{
		GameID: game.ID, Source: models.OddsSourceDraftKings,
		HomeSpread: dec("-3.5"), AwaySpread: dec("3.5"), HomeOdds: dec("-110"), AwayOdds: dec("-110"),
	}
	moneyLine = &models.MoneyLineOdds{
		GameID: game.ID, Source: models.OddsSourceDraftKings,
		HomeOdds: dec("-150"), AwayOdds: dec("130"),
	}
	total = &models.OverUnderOdds{
		GameID: game.ID, Source: models.OddsSourceDraftKings,
		Total: dec("45.5"), OverOdds: dec("-110"), UnderOdds: dec("-110"),
	}
	for _, row := range []any{spread, moneyLine, total} {
		if err := h.db.Create(row).Error; err != nil {
			h.t.Fatalf("creating odds: %v", err)
		}
	}
	return spread, moneyLine, total
}

// score writes a result for game. A nil finalizedAt is a provisional score.
func (h *harness) score(game *models.Game, home, away int, finalizedAt *time.Time) {
	h.t.Helper()

	result := &models.GameResult{GameID: game.ID, HomeScore: home, AwayScore: away, FinalizedAt: finalizedAt}
	if err := h.db.Create(result).Error; err != nil {
		h.t.Fatalf("writing result: %v", err)
	}
}

// setStatus moves a game's status the way a sync would, leaving its kickoff alone.
func (h *harness) setStatus(game *models.Game, status models.GameStatus) {
	h.t.Helper()

	if err := h.db.Model(&models.Game{}).Where("id = ?", game.ID).Update("status", status).Error; err != nil {
		h.t.Fatalf("setting game status: %v", err)
	}
}

// balance reads a purse back from the database.
func (h *harness) balance(user *models.User) decimal.Decimal {
	h.t.Helper()

	var purse models.Purse
	if err := h.db.Where("user_id = ? AND league_id = ?", user.ID, h.league.ID).First(&purse).Error; err != nil {
		h.t.Fatalf("reading purse: %v", err)
	}
	return purse.Balance
}

func (h *harness) requireBalance(user *models.User, want string) {
	h.t.Helper()

	if got := h.balance(user); !got.Equal(dec(want)) {
		h.t.Errorf("balance = %s, want %s", got, want)
	}
}

// status reads a bet's status back, from whichever table holds it.
func (h *harness) status(model any, id uuid.UUID) models.BetStatus {
	h.t.Helper()

	var statuses []models.BetStatus
	if err := h.db.Model(model).Where("id = ?", id).Pluck("status", &statuses).Error; err != nil {
		h.t.Fatalf("reading bet status: %v", err)
	}
	if len(statuses) != 1 {
		h.t.Fatalf("bet %s: found %d rows, want 1", id, len(statuses))
	}
	return statuses[0]
}

func (h *harness) placeSpread(user *models.User, game *models.Game, odds *models.SpreadOdds, pick models.SpreadPick, stake string) *models.SpreadBet {
	h.t.Helper()

	bet, err := h.svc.CreateSpreadBet(CreateSpreadBetInput{
		UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
		Pick: pick, Stake: dec(stake), OddsID: &odds.ID,
	})
	if err != nil {
		h.t.Fatalf("placing spread bet: %v", err)
	}
	return bet
}

func (h *harness) placeMoneyLine(user *models.User, game *models.Game, odds *models.MoneyLineOdds, pick models.MoneyLinePick, stake string) *models.MoneyLineBet {
	h.t.Helper()

	bet, err := h.svc.CreateMoneyLineBet(CreateMoneyLineBetInput{
		UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
		Pick: pick, Stake: dec(stake), OddsID: &odds.ID,
	})
	if err != nil {
		h.t.Fatalf("placing money line bet: %v", err)
	}
	return bet
}

func (h *harness) placeOverUnder(user *models.User, game *models.Game, odds *models.OverUnderOdds, pick models.OverUnderPick, stake string) *models.OverUnderBet {
	h.t.Helper()

	bet, err := h.svc.CreateOverUnderBet(CreateOverUnderBetInput{
		UserID: user.ID, LeagueID: h.league.ID, GameID: game.ID,
		Pick: pick, Stake: dec(stake), OddsID: &odds.ID,
	})
	if err != nil {
		h.t.Fatalf("placing over/under bet: %v", err)
	}
	return bet
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
