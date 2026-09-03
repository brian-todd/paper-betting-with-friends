// Command seedtestdata populates the database with test users, test leagues,
// and a mix of bets against whatever games are already loaded, for exercising
// the app locally without hand-creating fixtures through the UI.
//
// It reads games rather than fetching them, so `make seed` (and, for
// basketball, `make seedcbb`) needs to have loaded a season first.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/database"
	"github.com/brian/paper-betting-with-friends/internal/leagues"
	"github.com/brian/paper-betting-with-friends/internal/logging"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// testUserPassword is shared by every seeded account so it only needs
// mentioning once, in the log line printed after they're created.
const testUserPassword = "testpass123"

var testUsernames = []string{"testalice", "testbob", "testcarol", "testdave"}

var testLeagueSpecs = []struct {
	Name            string
	Public          bool
	StartingBalance decimal.Decimal
}{
	{Name: "Test League", Public: true, StartingBalance: decimal.NewFromInt(1000)},
	{Name: "Test League Two", Public: false, StartingBalance: decimal.NewFromInt(500)},
}

// stakeAmounts is cycled through for bet variety rather than using one flat
// stake for every bet.
var stakeAmounts = []decimal.Decimal{
	decimal.NewFromInt(20),
	decimal.NewFromInt(35),
	decimal.NewFromInt(50),
	decimal.NewFromInt(75),
}

// candidateWindow bounds how far the seed looks for games: far enough back to
// catch a completed season, far enough ahead to catch next week's slate even
// right at a season's start.
const (
	pastWindow   = 400 * 24 * time.Hour
	futureWindow = 45 * 24 * time.Hour
	gamesPerSide = 4
)

func main() {
	cfg := config.Load()
	logger := logging.Setup(cfg.Env)

	if err := run(cfg); err != nil {
		logger.Error("seed test data failed", "error", err)
		os.Exit(1)
	}

	logger.Info("test data seeded successfully")
}

// run performs the seed. Keeping the work out of main means deferred cleanup
// still runs when the command fails.
func run(cfg *config.Config) error {
	db, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	defer database.Close(db)

	authService := auth.NewService(db, cfg)
	leagueService := leagues.NewService(db)
	betsService := bets.NewService(db)

	users, err := seedUsers(db, authService)
	if err != nil {
		return fmt.Errorf("seeding users: %w", err)
	}
	slog.Info("test users ready", "count", len(users), "password", testUserPassword)

	testLeagues, err := seedLeagues(leagueService, users)
	if err != nil {
		return fmt.Errorf("seeding leagues: %w", err)
	}
	slog.Info("test leagues ready", "count", len(testLeagues))

	upcoming, past, err := loadCandidateGames(db)
	if err != nil {
		return fmt.Errorf("loading games: %w", err)
	}
	if len(upcoming) == 0 && len(past) == 0 {
		return errors.New("no games with odds found in the database -- run `make seed year=<season> week=<week>` first")
	}

	state := &seedState{users: users, leagues: testLeagues}

	pending, err := seedPendingBets(betsService, state, upcoming)
	if err != nil {
		return fmt.Errorf("seeding pending bets: %w", err)
	}
	slog.Info("pending bets placed", "count", pending, "games", len(upcoming))

	settled, err := seedSettledBets(db, betsService, state, past)
	if err != nil {
		return fmt.Errorf("seeding settled bets: %w", err)
	}
	slog.Info("settled bets placed", "count", settled, "games", len(past))

	return nil
}

// seedUsers creates the test accounts, reusing any that already exist so the
// command can be run again without erroring.
func seedUsers(db *gorm.DB, authService *auth.Service) ([]models.User, error) {
	userRepo := repository.NewUserRepository(db)
	users := make([]models.User, 0, len(testUsernames))

	for _, username := range testUsernames {
		existing, err := userRepo.FindByUsername(username)
		if err == nil {
			users = append(users, *existing)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		created, err := authService.Register(username, testUserPassword)
		if err != nil {
			return nil, fmt.Errorf("registering %s: %w", username, err)
		}
		users = append(users, *created)
	}

	return users, nil
}

// seedLeagues creates the test leagues under the first test user and adds the
// rest as members, reusing any league that already has a matching name so the
// command can be run again without piling up duplicates.
func seedLeagues(leagueService *leagues.Service, users []models.User) ([]models.League, error) {
	creator := users[0]

	existing, err := leagueService.GetUserLeagues(creator.ID)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]models.League, len(existing))
	for _, l := range existing {
		byName[l.Name] = l
	}

	result := make([]models.League, 0, len(testLeagueSpecs))
	for _, spec := range testLeagueSpecs {
		league, ok := byName[spec.Name]
		if !ok {
			created, err := leagueService.CreateLeague(spec.Name, creator.ID, spec.Public, spec.StartingBalance)
			if err != nil {
				return nil, fmt.Errorf("creating league %q: %w", spec.Name, err)
			}
			league = *created
		}
		result = append(result, league)

		for _, u := range users[1:] {
			if err := joinLeague(leagueService, league, u.ID); err != nil {
				return nil, fmt.Errorf("adding %s to %q: %w", u.Username, spec.Name, err)
			}
		}
	}

	return result, nil
}

// joinLeague adds a user to a league by whichever route its visibility
// requires, treating "already a member" as success.
func joinLeague(leagueService *leagues.Service, league models.League, userID uuid.UUID) error {
	var err error
	if league.IsPublic != nil && *league.IsPublic {
		err = leagueService.JoinLeague(league.ID, userID)
	} else {
		_, err = leagueService.JoinByCode(league.InviteCode, userID)
	}
	if err != nil && !errors.Is(err, leagues.ErrAlreadyMember) {
		return err
	}
	return nil
}

// gameOdds is a game alongside the first odds row per market found for it, if
// any -- enough to place a bet that mirrors what the odds page would offer.
type gameOdds struct {
	game      models.Game
	moneyLine *models.MoneyLineOdds
	spread    *models.SpreadOdds
	overUnder *models.OverUnderOdds
}

// loadCandidateGames finds bettable games split into upcoming (for pending
// bets) and finalized past ones (for settled bets), nearest to now first in
// both directions -- the closest thing to "current week and before" without
// depending on the week calendar being populated for the exact date range in
// play.
func loadCandidateGames(db *gorm.DB) (upcoming, past []gameOdds, err error) {
	gameRepo := repository.NewGameRepository(db)
	moneyLineRepo := repository.NewMoneyLineOddsRepository(db)
	spreadRepo := repository.NewSpreadOddsRepository(db)
	overUnderRepo := repository.NewOverUnderOddsRepository(db)

	now := time.Now()

	upcomingGames, err := gameRepo.FindByDateRangeAndSport(models.SportFootball, now, now.Add(futureWindow))
	if err != nil {
		return nil, nil, err
	}
	pastGames, err := gameRepo.FindByDateRangeAndSport(models.SportFootball, now.Add(-pastWindow), now)
	if err != nil {
		return nil, nil, err
	}

	upcoming = attachOdds(moneyLineRepo, spreadRepo, overUnderRepo, upcomingGames)
	past = attachOdds(moneyLineRepo, spreadRepo, overUnderRepo, finalizedOnly(pastGames))

	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].game.ScheduledAt.Before(upcoming[j].game.ScheduledAt)
	})
	sort.Slice(past, func(i, j int) bool {
		return past[i].game.ScheduledAt.After(past[j].game.ScheduledAt)
	})

	return capGames(upcoming, gamesPerSide), capGames(past, gamesPerSide), nil
}

// finalizedOnly drops games whose result, if any, is not yet final -- settling
// a bet against a live score would pay out on a halftime lead.
func finalizedOnly(games []models.Game) []models.Game {
	final := make([]models.Game, 0, len(games))
	for _, g := range games {
		if g.Result.IsFinal() {
			final = append(final, g)
		}
	}
	return final
}

// attachOdds loads each market's odds for the given games and drops any game
// with no odds in any market, since nothing can be bet on it.
func attachOdds(
	moneyLineRepo *repository.MoneyLineOddsRepository,
	spreadRepo *repository.SpreadOddsRepository,
	overUnderRepo *repository.OverUnderOddsRepository,
	games []models.Game,
) []gameOdds {
	result := make([]gameOdds, 0, len(games))
	for _, g := range games {
		gm := gameOdds{game: g}

		if odds, err := moneyLineRepo.FindBookLinesByGame(g.ID); err == nil && len(odds) > 0 {
			gm.moneyLine = &odds[0]
		}
		if odds, err := spreadRepo.FindBookLinesByGame(g.ID); err == nil && len(odds) > 0 {
			gm.spread = &odds[0]
		}
		if odds, err := overUnderRepo.FindBookLinesByGame(g.ID); err == nil && len(odds) > 0 {
			gm.overUnder = &odds[0]
		}

		if gm.moneyLine == nil && gm.spread == nil && gm.overUnder == nil {
			continue
		}
		result = append(result, gm)
	}
	return result
}

func capGames(games []gameOdds, n int) []gameOdds {
	if len(games) > n {
		return games[:n]
	}
	return games
}

// seedState round-robins users, leagues, and stake amounts across the bets
// being placed so the data doesn't pile onto a single account or line.
type seedState struct {
	users   []models.User
	leagues []models.League

	userN, leagueN, stakeN int
}

func (s *seedState) user() models.User {
	u := s.users[s.userN%len(s.users)]
	s.userN++
	return u
}

func (s *seedState) league() models.League {
	l := s.leagues[s.leagueN%len(s.leagues)]
	s.leagueN++
	return l
}

func (s *seedState) stake() decimal.Decimal {
	amount := stakeAmounts[s.stakeN%len(stakeAmounts)]
	s.stakeN++
	return amount
}

// seedPendingBets places bets on upcoming games through the bets service, the
// same path the UI uses, so each one passes the usual validation and purse
// deduction.
func seedPendingBets(betsService *bets.Service, state *seedState, upcoming []gameOdds) (int, error) {
	count := 0
	for _, gm := range upcoming {
		if gm.spread != nil {
			user, league := state.user(), state.league()
			_, err := betsService.CreateSpreadBet(bets.CreateSpreadBetInput{
				UserID: user.ID, LeagueID: league.ID, GameID: gm.game.ID,
				Pick: models.SpreadPickHome, Stake: state.stake(), OddsID: &gm.spread.ID,
			})
			if err != nil {
				return count, fmt.Errorf("spread bet on game %s: %w", gm.game.ID, err)
			}
			count++
		}

		if gm.moneyLine != nil {
			user, league := state.user(), state.league()
			_, err := betsService.CreateMoneyLineBet(bets.CreateMoneyLineBetInput{
				UserID: user.ID, LeagueID: league.ID, GameID: gm.game.ID,
				Pick: models.MoneyLinePickAway, Stake: state.stake(), OddsID: &gm.moneyLine.ID,
			})
			if err != nil {
				return count, fmt.Errorf("money line bet on game %s: %w", gm.game.ID, err)
			}
			count++
		}

		if gm.overUnder != nil {
			user, league := state.user(), state.league()
			_, err := betsService.CreateOverUnderBet(bets.CreateOverUnderBetInput{
				UserID: user.ID, LeagueID: league.ID, GameID: gm.game.ID,
				Pick: models.OverUnderPickOver, Stake: state.stake(), OddsID: &gm.overUnder.ID,
			})
			if err != nil {
				return count, fmt.Errorf("over/under bet on game %s: %w", gm.game.ID, err)
			}
			count++
		}
	}
	return count, nil
}

// seedSettledBets places bets directly against past, finalized games -- the
// bets service refuses these since it requires a game that hasn't started --
// then runs the real settlement path so status and payouts come out exactly
// as they would for a live bet.
//
// Money line bets are placed on both sides of each game, which guarantees a
// win and a loss per game since a completed football game always has a
// winner. Spread and total bets go to whichever side the game index picks,
// so their outcome comes from the real score rather than being forced.
func seedSettledBets(db *gorm.DB, betsService *bets.Service, state *seedState, past []gameOdds) (int, error) {
	spreadBetRepo := repository.NewSpreadBetRepository(db)
	moneyLineBetRepo := repository.NewMoneyLineBetRepository(db)
	overUnderBetRepo := repository.NewOverUnderBetRepository(db)
	purseRepo := repository.NewPurseRepository(db)

	count := 0
	for i, gm := range past {
		if gm.moneyLine != nil {
			if err := insertMoneyLineBet(moneyLineBetRepo, purseRepo, state, gm, models.MoneyLinePickHome); err != nil {
				return count, err
			}
			count++
			if err := insertMoneyLineBet(moneyLineBetRepo, purseRepo, state, gm, models.MoneyLinePickAway); err != nil {
				return count, err
			}
			count++
		}

		if gm.spread != nil {
			pick := models.SpreadPickHome
			if i%2 == 1 {
				pick = models.SpreadPickAway
			}
			if err := insertSpreadBet(spreadBetRepo, purseRepo, state, gm, pick); err != nil {
				return count, err
			}
			count++
		}

		if gm.overUnder != nil {
			pick := models.OverUnderPickOver
			if i%2 == 1 {
				pick = models.OverUnderPickUnder
			}
			if err := insertOverUnderBet(overUnderBetRepo, purseRepo, state, gm, pick); err != nil {
				return count, err
			}
			count++
		}

		if err := betsService.EvaluateBetsForGame(gm.game.ID); err != nil {
			return count, fmt.Errorf("evaluating bets for game %s: %w", gm.game.ID, err)
		}
	}

	return count, nil
}

func insertMoneyLineBet(
	repo *repository.MoneyLineBetRepository,
	purseRepo *repository.PurseRepository,
	state *seedState,
	gm gameOdds,
	pick models.MoneyLinePick,
) error {
	user, league, stake := state.user(), state.league(), state.stake()

	odds := gm.moneyLine.HomeOdds
	if pick == models.MoneyLinePickAway {
		odds = gm.moneyLine.AwayOdds
	}

	if err := purseRepo.DeductStake(user.ID, league.ID, stake); err != nil {
		return fmt.Errorf("deducting stake for %s: %w", user.Username, err)
	}

	bet := &models.MoneyLineBet{
		UserID: user.ID, LeagueID: league.ID, GameID: gm.game.ID,
		MoneyLineOddsID: gm.moneyLine.ID, Pick: pick,
		OddsSnapshot: odds, Stake: stake, Status: models.BetStatusPending,
	}
	if err := repo.Create(bet); err != nil {
		_ = purseRepo.CreditWinnings(user.ID, league.ID, stake)
		return fmt.Errorf("creating money line bet on game %s: %w", gm.game.ID, err)
	}
	return nil
}

func insertSpreadBet(
	repo *repository.SpreadBetRepository,
	purseRepo *repository.PurseRepository,
	state *seedState,
	gm gameOdds,
	pick models.SpreadPick,
) error {
	user, league, stake := state.user(), state.league(), state.stake()

	spread, odds := gm.spread.HomeSpread, gm.spread.HomeOdds
	if pick == models.SpreadPickAway {
		spread, odds = gm.spread.AwaySpread, gm.spread.AwayOdds
	}

	if err := purseRepo.DeductStake(user.ID, league.ID, stake); err != nil {
		return fmt.Errorf("deducting stake for %s: %w", user.Username, err)
	}

	bet := &models.SpreadBet{
		UserID: user.ID, LeagueID: league.ID, GameID: gm.game.ID,
		SpreadOddsID: gm.spread.ID, Pick: pick,
		SpreadSnapshot: spread, OddsSnapshot: odds, Stake: stake, Status: models.BetStatusPending,
	}
	if err := repo.Create(bet); err != nil {
		_ = purseRepo.CreditWinnings(user.ID, league.ID, stake)
		return fmt.Errorf("creating spread bet on game %s: %w", gm.game.ID, err)
	}
	return nil
}

func insertOverUnderBet(
	repo *repository.OverUnderBetRepository,
	purseRepo *repository.PurseRepository,
	state *seedState,
	gm gameOdds,
	pick models.OverUnderPick,
) error {
	user, league, stake := state.user(), state.league(), state.stake()

	odds := gm.overUnder.OverOdds
	if pick == models.OverUnderPickUnder {
		odds = gm.overUnder.UnderOdds
	}

	if err := purseRepo.DeductStake(user.ID, league.ID, stake); err != nil {
		return fmt.Errorf("deducting stake for %s: %w", user.Username, err)
	}

	bet := &models.OverUnderBet{
		UserID: user.ID, LeagueID: league.ID, GameID: gm.game.ID,
		OverUnderOddsID: gm.overUnder.ID, Pick: pick,
		TotalSnapshot: gm.overUnder.Total, OddsSnapshot: odds, Stake: stake, Status: models.BetStatusPending,
	}
	if err := repo.Create(bet); err != nil {
		_ = purseRepo.CreditWinnings(user.ID, league.ID, stake)
		return fmt.Errorf("creating over/under bet on game %s: %w", gm.game.ID, err)
	}
	return nil
}
