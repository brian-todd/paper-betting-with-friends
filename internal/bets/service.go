package bets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/syncerr"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrGameNotFound      = errors.New("game not found")
	ErrGameStarted       = errors.New("game has already started")
	ErrBetNotFound       = errors.New("bet not found")
	ErrBetNotPending     = errors.New("bet is not pending")
	ErrNotBetOwner       = errors.New("not the owner of this bet")
	ErrOddsNotFound      = errors.New("odds not found")
	ErrLeagueNotFound    = errors.New("league not found")
	ErrNotLeagueMember   = errors.New("user is not a member of this league")
	ErrInvalidBetType    = errors.New("invalid bet type")
	ErrInsufficientFunds = errors.New("insufficient funds in purse")
	ErrInvalidStake      = errors.New("stake must be greater than zero")
)

// Service handles betting business logic.
type Service struct {
	db                *gorm.DB
	gameRepo          *repository.GameRepository
	gameResultRepo    *repository.GameResultRepository
	leagueRepo        *repository.LeagueRepository
	weekRepo          *repository.WeekRepository
	purseRepo         *repository.PurseRepository
	spreadBetRepo     *repository.SpreadBetRepository
	moneyLineBetRepo  *repository.MoneyLineBetRepository
	overUnderBetRepo  *repository.OverUnderBetRepository
	spreadOddsRepo    *repository.SpreadOddsRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository
	betPeriodRepo     *repository.BetPeriodRepository
	holyLockRepo      *repository.HolyLockRepository
	userBetRepo       *repository.UserBetRepository
	betPageRepo       *repository.BetPageRepository
	settlementRepo    *repository.SettlementRepository
	logger            *slog.Logger
}

// NewService creates a new bets service.
func NewService(db *gorm.DB) *Service {
	return &Service{
		db:                db,
		gameRepo:          repository.NewGameRepository(db),
		gameResultRepo:    repository.NewGameResultRepository(db),
		leagueRepo:        repository.NewLeagueRepository(db),
		weekRepo:          repository.NewWeekRepository(db),
		purseRepo:         repository.NewPurseRepository(db),
		spreadBetRepo:     repository.NewSpreadBetRepository(db),
		moneyLineBetRepo:  repository.NewMoneyLineBetRepository(db),
		overUnderBetRepo:  repository.NewOverUnderBetRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
		betPeriodRepo:     repository.NewBetPeriodRepository(db),
		holyLockRepo:      repository.NewHolyLockRepository(db),
		userBetRepo:       repository.NewUserBetRepository(db),
		betPageRepo:       repository.NewBetPageRepository(db),
		settlementRepo:    repository.NewSettlementRepository(db),
		logger:            slog.Default().With("component", "bet-settlement"),
	}
}

// CreateSpreadBetInput contains the input for creating a spread bet.
type CreateSpreadBetInput struct {
	UserID   uuid.UUID
	LeagueID uuid.UUID
	GameID   uuid.UUID
	Pick     models.SpreadPick
	Stake    decimal.Decimal
	// HolyLock nominates this bet as the week's Holy Lock as it is placed.
	// Placement is refused outright if the week's slot is already taken.
	HolyLock bool
	// For existing odds.
	OddsID *uuid.UUID
	// For custom odds.
	CustomSpread *decimal.Decimal
	CustomOdds   *decimal.Decimal
}

// CreateSpreadBet creates a new spread bet.
func (s *Service) CreateSpreadBet(input CreateSpreadBetInput) (*models.SpreadBet, error) {
	// Validate game exists and hasn't started.
	game, err := s.gameRepo.FindByID(input.GameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGameNotFound
		}
		return nil, err
	}

	if game.ScheduledAt.Before(time.Now()) {
		return nil, ErrGameStarted
	}

	// Validate league membership.
	isMember, err := s.leagueRepo.IsMember(input.LeagueID, input.UserID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotLeagueMember
	}

	// Checked before the stake is deducted and before the odds are resolved: a
	// custom line is written as a side effect of resolving, so a refusal after
	// that point would leave an orphan odds row behind as well as needing a
	// compensating refund.
	if input.HolyLock {
		if err := s.ensureHolyLockAvailable(input.UserID, input.LeagueID, *game); err != nil {
			return nil, err
		}
	}

	selection, err := s.resolveSpreadOdds(input.GameID, input.Pick, input.OddsID, input.CustomSpread, input.CustomOdds)
	if err != nil {
		return nil, err
	}

	// Deduct stake from purse.
	if err := s.purseRepo.DeductStake(input.UserID, input.LeagueID, input.Stake); err != nil {
		if errors.Is(err, repository.ErrInsufficientBalance) {
			return nil, ErrInsufficientFunds
		}
		return nil, err
	}

	bet := &models.SpreadBet{
		UserID:         input.UserID,
		LeagueID:       input.LeagueID,
		GameID:         input.GameID,
		SpreadOddsID:   selection.OddsID,
		Pick:           input.Pick,
		SpreadSnapshot: selection.Spread,
		OddsSnapshot:   selection.Odds,
		Stake:          input.Stake,
		Status:         models.BetStatusPending,
		IsHolyLock:     input.HolyLock,
	}

	if err := s.spreadBetRepo.Create(bet); err != nil {
		// Refund stake on error.
		_ = s.purseRepo.CreditWinnings(input.UserID, input.LeagueID, input.Stake)
		return nil, err
	}

	return bet, nil
}

// CreateMoneyLineBetInput contains the input for creating a money line bet.
type CreateMoneyLineBetInput struct {
	UserID   uuid.UUID
	LeagueID uuid.UUID
	GameID   uuid.UUID
	Pick     models.MoneyLinePick
	Stake    decimal.Decimal
	// HolyLock nominates this bet as the week's Holy Lock as it is placed.
	// Placement is refused outright if the week's slot is already taken.
	HolyLock bool
	// For existing odds.
	OddsID *uuid.UUID
	// For custom odds.
	CustomHomeOdds *decimal.Decimal
	CustomAwayOdds *decimal.Decimal
}

// CreateMoneyLineBet creates a new money line bet.
func (s *Service) CreateMoneyLineBet(input CreateMoneyLineBetInput) (*models.MoneyLineBet, error) {
	// Validate game exists and hasn't started.
	game, err := s.gameRepo.FindByID(input.GameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGameNotFound
		}
		return nil, err
	}

	if game.ScheduledAt.Before(time.Now()) {
		return nil, ErrGameStarted
	}

	// Validate league membership.
	isMember, err := s.leagueRepo.IsMember(input.LeagueID, input.UserID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotLeagueMember
	}

	// Checked before the stake is deducted and before the odds are resolved: a
	// custom line is written as a side effect of resolving, so a refusal after
	// that point would leave an orphan odds row behind as well as needing a
	// compensating refund.
	if input.HolyLock {
		if err := s.ensureHolyLockAvailable(input.UserID, input.LeagueID, *game); err != nil {
			return nil, err
		}
	}

	selection, err := s.resolveMoneyLineOdds(input.GameID, input.Pick, input.OddsID, input.CustomHomeOdds, input.CustomAwayOdds)
	if err != nil {
		return nil, err
	}

	// Deduct stake from purse.
	if err := s.purseRepo.DeductStake(input.UserID, input.LeagueID, input.Stake); err != nil {
		if errors.Is(err, repository.ErrInsufficientBalance) {
			return nil, ErrInsufficientFunds
		}
		return nil, err
	}

	bet := &models.MoneyLineBet{
		UserID:          input.UserID,
		LeagueID:        input.LeagueID,
		GameID:          input.GameID,
		MoneyLineOddsID: selection.OddsID,
		Pick:            input.Pick,
		OddsSnapshot:    selection.Odds,
		Stake:           input.Stake,
		Status:          models.BetStatusPending,
		IsHolyLock:      input.HolyLock,
	}

	if err := s.moneyLineBetRepo.Create(bet); err != nil {
		// Refund stake on error.
		_ = s.purseRepo.CreditWinnings(input.UserID, input.LeagueID, input.Stake)
		return nil, err
	}

	return bet, nil
}

// CreateOverUnderBetInput contains the input for creating an over/under bet.
type CreateOverUnderBetInput struct {
	UserID   uuid.UUID
	LeagueID uuid.UUID
	GameID   uuid.UUID
	Pick     models.OverUnderPick
	Stake    decimal.Decimal
	// HolyLock nominates this bet as the week's Holy Lock as it is placed.
	// Placement is refused outright if the week's slot is already taken.
	HolyLock bool
	// For existing odds.
	OddsID *uuid.UUID
	// For custom odds.
	CustomTotal     *decimal.Decimal
	CustomOverOdds  *decimal.Decimal
	CustomUnderOdds *decimal.Decimal
}

// CreateOverUnderBet creates a new over/under bet.
func (s *Service) CreateOverUnderBet(input CreateOverUnderBetInput) (*models.OverUnderBet, error) {
	// Validate game exists and hasn't started.
	game, err := s.gameRepo.FindByID(input.GameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGameNotFound
		}
		return nil, err
	}

	if game.ScheduledAt.Before(time.Now()) {
		return nil, ErrGameStarted
	}

	// Validate league membership.
	isMember, err := s.leagueRepo.IsMember(input.LeagueID, input.UserID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotLeagueMember
	}

	// Checked before the stake is deducted and before the odds are resolved: a
	// custom line is written as a side effect of resolving, so a refusal after
	// that point would leave an orphan odds row behind as well as needing a
	// compensating refund.
	if input.HolyLock {
		if err := s.ensureHolyLockAvailable(input.UserID, input.LeagueID, *game); err != nil {
			return nil, err
		}
	}

	selection, err := s.resolveOverUnderOdds(
		input.GameID, input.Pick, input.OddsID,
		input.CustomTotal, input.CustomOverOdds, input.CustomUnderOdds,
	)
	if err != nil {
		return nil, err
	}

	// Deduct stake from purse.
	if err := s.purseRepo.DeductStake(input.UserID, input.LeagueID, input.Stake); err != nil {
		if errors.Is(err, repository.ErrInsufficientBalance) {
			return nil, ErrInsufficientFunds
		}
		return nil, err
	}

	bet := &models.OverUnderBet{
		UserID:          input.UserID,
		LeagueID:        input.LeagueID,
		GameID:          input.GameID,
		OverUnderOddsID: selection.OddsID,
		Pick:            input.Pick,
		TotalSnapshot:   selection.Total,
		OddsSnapshot:    selection.Odds,
		Stake:           input.Stake,
		Status:          models.BetStatusPending,
		IsHolyLock:      input.HolyLock,
	}

	if err := s.overUnderBetRepo.Create(bet); err != nil {
		// Refund stake on error.
		_ = s.purseRepo.CreditWinnings(input.UserID, input.LeagueID, input.Stake)
		return nil, err
	}

	return bet, nil
}

// cancellable reports whether a bet on game may still be voided for a refund.
//
// The kickoff gate reads ScheduledAt rather than Game.Status, matching
// authorizeEdit and editable: status only advances when the sync runs, and
// between kickoff and the next run it still says "scheduled" -- close enough
// for a badge, not for deciding whether someone may still take their stake
// back off a game being played. The football sync is hourly overnight, so that
// window is already most of an hour, and relaxing the /games cadence widens it.
func cancellable(game *models.Game, now time.Time) error {
	// A game that will not be played is the one case where a passed kickoff
	// must not block a refund -- that is precisely when the stake should come
	// back. No football path writes either status, but the basketball sync does
	// (cbbdata.mapGameStatus) and this is sport-agnostic.
	if game.Status == models.GameStatusPostponed || game.Status == models.GameStatusCancelled {
		return nil
	}

	// Both gates, not one in place of the other. Status is the half that goes
	// stale, so it cannot be trusted alone. But it is not wrong when it is set,
	// and it catches what kickoff misses: a feed that has called a game final
	// while scheduled_at still reads in the future.
	if game.Status == models.GameStatusInProgress || game.Status == models.GameStatusFinal {
		return ErrGameStarted
	}
	if !game.ScheduledAt.After(now) {
		return ErrGameStarted
	}
	return nil
}

// CancelSpreadBet cancels a pending spread bet.
func (s *Service) CancelSpreadBet(betID, userID uuid.UUID) error {
	bet, err := s.spreadBetRepo.FindByID(betID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBetNotFound
		}
		return err
	}

	if bet.UserID != userID {
		return ErrNotBetOwner
	}

	if bet.Status != models.BetStatusPending {
		return ErrBetNotPending
	}

	game, err := s.gameRepo.FindByID(bet.GameID)
	if err != nil {
		return err
	}
	if err := cancellable(game, time.Now()); err != nil {
		return err
	}

	bet.Status = models.BetStatusVoid
	// A cancelled bet gives its week's Holy Lock slot back.
	bet.IsHolyLock = false
	if err := s.spreadBetRepo.Update(bet); err != nil {
		return err
	}

	// Refund stake to purse.
	return s.purseRepo.CreditWinnings(bet.UserID, bet.LeagueID, bet.Stake)
}

// CancelMoneyLineBet cancels a pending money line bet.
func (s *Service) CancelMoneyLineBet(betID, userID uuid.UUID) error {
	bet, err := s.moneyLineBetRepo.FindByID(betID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBetNotFound
		}
		return err
	}

	if bet.UserID != userID {
		return ErrNotBetOwner
	}

	if bet.Status != models.BetStatusPending {
		return ErrBetNotPending
	}

	game, err := s.gameRepo.FindByID(bet.GameID)
	if err != nil {
		return err
	}
	if err := cancellable(game, time.Now()); err != nil {
		return err
	}

	bet.Status = models.BetStatusVoid
	// A cancelled bet gives its week's Holy Lock slot back.
	bet.IsHolyLock = false
	if err := s.moneyLineBetRepo.Update(bet); err != nil {
		return err
	}

	// Refund stake to purse.
	return s.purseRepo.CreditWinnings(bet.UserID, bet.LeagueID, bet.Stake)
}

// CancelOverUnderBet cancels a pending over/under bet.
func (s *Service) CancelOverUnderBet(betID, userID uuid.UUID) error {
	bet, err := s.overUnderBetRepo.FindByID(betID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBetNotFound
		}
		return err
	}

	if bet.UserID != userID {
		return ErrNotBetOwner
	}

	if bet.Status != models.BetStatusPending {
		return ErrBetNotPending
	}

	game, err := s.gameRepo.FindByID(bet.GameID)
	if err != nil {
		return err
	}
	if err := cancellable(game, time.Now()); err != nil {
		return err
	}

	bet.Status = models.BetStatusVoid
	// A cancelled bet gives its week's Holy Lock slot back.
	bet.IsHolyLock = false
	if err := s.overUnderBetRepo.Update(bet); err != nil {
		return err
	}

	// Refund stake to purse.
	return s.purseRepo.CreditWinnings(bet.UserID, bet.LeagueID, bet.Stake)
}

// minTimeToPlay is how long after kickoff a game must be before its bets may
// settle.
//
// Finality now comes from whichever feed reports it first, and for football
// that is the scoreboard -- minutes after the whistle rather than at the next
// /games run. That speed is the point of the sweep, and it cuts both ways: a
// feed that mislabels a game as complete is acted on just as quickly, and a
// settled bet has already moved money.
//
// No football or basketball game is played out in ninety minutes, so a "final"
// arriving sooner than that is a feed error rather than a result. Waiting costs
// nothing, because the sweep comes round again five minutes later.
const minTimeToPlay = 90 * time.Minute

// SettleFinalGames settles the bets on every game whose result is final.
//
// It exists because finalizing a game and paying out on it used to be the same
// act: EvaluateBetsForGame was reached only from the middle of a /games sync,
// so a bet settled when that endpoint next happened to be polled and the feed
// that had actually seen the game end -- the scoreboard, which writes
// finalized_at and stops there -- could not settle anything at all.
//
// Running it as its own sweep decouples the two. Any feed can finalize a game;
// this turns finality into money, on its own schedule, reading nothing but the
// database. It also makes settlement retriable, which it was not: a game whose
// payout failed halfway used to depend on another sync run passing the same way.
//
// Settling one game is best effort. A single bet that will not save is no
// reason to leave the rest of a Saturday unpaid, so failures are tallied and
// reported at the end rather than abandoning the run -- see syncerr.
func (s *Service) SettleFinalGames(ctx context.Context) error {
	games, err := s.settlementRepo.FindGamesAwaitingSettlement(time.Now().Add(-minTimeToPlay))
	if err != nil {
		return err
	}
	if len(games) == 0 {
		// The common case by far, and not worth a log line every five minutes.
		return nil
	}

	var failed syncerr.Tally
	settled := 0

	for _, gameID := range games {
		// Shutdown cancels mid-sweep. Stopping between games leaves the rest
		// for the next run, which is exactly what the sweep is for.
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := s.EvaluateBetsForGame(gameID); err != nil {
			s.logger.Error("failed to settle bets for game", "game", gameID, "error", err)
			failed.Add(err)
			continue
		}
		settled++
	}

	s.logger.Info("settled finalized games", "games", settled, "failed", failed.Count())
	return failed.Err("settlement")
}

// EvaluateBetsForGame evaluates all pending bets for a completed game.
//
// It is safe to call repeatedly and from more than one place at once: a bet is
// only ever moved off pending by the conditional update in SettleIfPending, and
// the purse is credited only by the caller that won that update.
func (s *Service) EvaluateBetsForGame(gameID uuid.UUID) error {
	// Get game result.
	result, err := s.gameResultRepo.FindByGameID(gameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // No result yet, nothing to evaluate.
		}
		return err
	}

	// A result row also exists while a game is in progress so live scores can
	// be displayed. Settling against one would pay out on a halftime lead, so
	// only a finalized score counts.
	if !result.IsFinal() {
		return nil
	}

	// Evaluate spread bets.
	spreadBets, err := s.spreadBetRepo.FindPendingByGame(gameID)
	if err != nil {
		return err
	}
	for i := range spreadBets {
		status := evaluateSpreadBet(&spreadBets[i], result)

		settled, err := s.spreadBetRepo.SettleIfPending(spreadBets[i].ID, status)
		if err != nil {
			return err
		}
		if !settled {
			// Someone else settled it between the read and the write.
			// Crediting the purse now would pay the bet a second time.
			continue
		}
		spreadBets[i].Status = status

		// Update purse based on outcome.
		if err := s.updatePurseForBet(spreadBets[i].UserID, spreadBets[i].LeagueID, spreadBets[i].Stake, spreadBets[i].OddsSnapshot, status); err != nil {
			return err
		}
	}

	// Evaluate money line bets.
	moneyLineBets, err := s.moneyLineBetRepo.FindPendingByGame(gameID)
	if err != nil {
		return err
	}
	for i := range moneyLineBets {
		status := evaluateMoneyLineBet(&moneyLineBets[i], result)

		settled, err := s.moneyLineBetRepo.SettleIfPending(moneyLineBets[i].ID, status)
		if err != nil {
			return err
		}
		if !settled {
			// Someone else settled it between the read and the write.
			// Crediting the purse now would pay the bet a second time.
			continue
		}
		moneyLineBets[i].Status = status

		// Update purse based on outcome.
		if err := s.updatePurseForBet(moneyLineBets[i].UserID, moneyLineBets[i].LeagueID, moneyLineBets[i].Stake, moneyLineBets[i].OddsSnapshot, status); err != nil {
			return err
		}
	}

	// Evaluate over/under bets.
	overUnderBets, err := s.overUnderBetRepo.FindPendingByGame(gameID)
	if err != nil {
		return err
	}
	for i := range overUnderBets {
		status := evaluateOverUnderBet(&overUnderBets[i], result)

		settled, err := s.overUnderBetRepo.SettleIfPending(overUnderBets[i].ID, status)
		if err != nil {
			return err
		}
		if !settled {
			// Someone else settled it between the read and the write.
			// Crediting the purse now would pay the bet a second time.
			continue
		}
		overUnderBets[i].Status = status

		// Update purse based on outcome.
		if err := s.updatePurseForBet(overUnderBets[i].UserID, overUnderBets[i].LeagueID, overUnderBets[i].Stake, overUnderBets[i].OddsSnapshot, status); err != nil {
			return err
		}
	}

	return nil
}

// updatePurseForBet updates the purse based on bet outcome.
func (s *Service) updatePurseForBet(userID, leagueID uuid.UUID, stake, odds decimal.Decimal, status models.BetStatus) error {
	switch status {
	case models.BetStatusWon:
		// Credit stake + winnings.
		payout := calculatePayout(stake, odds)
		return s.purseRepo.CreditWinnings(userID, leagueID, payout)
	case models.BetStatusPush:
		// Refund stake only.
		return s.purseRepo.CreditWinnings(userID, leagueID, stake)
	case models.BetStatusLost:
		// Stake already deducted, nothing to do.
		return nil
	default:
		return nil
	}
}

// calculatePayout calculates total payout (stake + profit) from American odds.
func calculatePayout(stake, odds decimal.Decimal) decimal.Decimal {
	return models.PayoutForOdds(stake, odds)
}

// evaluateSpreadBet determines the outcome of a spread bet.
func evaluateSpreadBet(bet *models.SpreadBet, result *models.GameResult) models.BetStatus {
	// Calculate spread-adjusted score.
	// Spread is from the perspective of the picked team.
	homeAdjusted := decimal.NewFromInt(int64(result.HomeScore)).Add(bet.SpreadSnapshot)
	awayScore := decimal.NewFromInt(int64(result.AwayScore))

	var pickedAdjusted, opponentScore decimal.Decimal
	if bet.Pick == models.SpreadPickHome {
		pickedAdjusted = homeAdjusted
		opponentScore = awayScore
	} else {
		// For away pick, we need to flip: away score + away spread vs home score.
		pickedAdjusted = awayScore.Add(bet.SpreadSnapshot)
		opponentScore = decimal.NewFromInt(int64(result.HomeScore))
	}

	if pickedAdjusted.GreaterThan(opponentScore) {
		return models.BetStatusWon
	} else if pickedAdjusted.LessThan(opponentScore) {
		return models.BetStatusLost
	}
	return models.BetStatusPush
}

// evaluateMoneyLineBet determines the outcome of a money line bet.
func evaluateMoneyLineBet(bet *models.MoneyLineBet, result *models.GameResult) models.BetStatus {
	homeWon := result.HomeScore > result.AwayScore
	awayWon := result.AwayScore > result.HomeScore

	if result.HomeScore == result.AwayScore {
		return models.BetStatusPush
	}

	if bet.Pick == models.MoneyLinePickHome {
		if homeWon {
			return models.BetStatusWon
		}
		return models.BetStatusLost
	}

	// Away pick.
	if awayWon {
		return models.BetStatusWon
	}
	return models.BetStatusLost
}

// evaluateOverUnderBet determines the outcome of an over/under bet.
func evaluateOverUnderBet(bet *models.OverUnderBet, result *models.GameResult) models.BetStatus {
	totalScore := decimal.NewFromInt(int64(result.HomeScore + result.AwayScore))

	if totalScore.Equal(bet.TotalSnapshot) {
		return models.BetStatusPush
	}

	if bet.Pick == models.OverUnderPickOver {
		if totalScore.GreaterThan(bet.TotalSnapshot) {
			return models.BetStatusWon
		}
		return models.BetStatusLost
	}

	// Under pick.
	if totalScore.LessThan(bet.TotalSnapshot) {
		return models.BetStatusWon
	}
	return models.BetStatusLost
}

// BetView is a unified view model for displaying any bet type.
type BetView struct {
	ID           uuid.UUID
	Type         string // "spread", "moneyline", "overunder"
	Game         models.Game
	League       models.League
	User         models.User
	Pick         string // Team abbr or "Over"/"Under"
	PickSide     string // "home"/"away", or "over"/"under"
	Line         string // "-7", "+150", "O 54.5"
	OddsSnapshot decimal.Decimal
	Stake        decimal.Decimal
	Status       models.BetStatus
	CreatedAt    time.Time

	// OddsID is the line the bet currently points at, so the edit form can
	// preselect it.
	OddsID uuid.UUID
	// Editable is true while the bet can still be changed: still pending, and
	// its game has not started.
	Editable bool
	// LineOptions are the lines on offer for this game, populated only for an
	// editable bet since nothing else needs them.
	LineOptions []BetLineOption

	// IsHolyLock marks the bet its owner nominated for this league and week.
	IsHolyLock bool
	// HolyLockEligible is true while the page may offer to nominate this bet.
	// It is set only for a user's own list -- see GetUserBets -- since the
	// admin browser spans every user and has no button to offer.
	HolyLockEligible bool
}

// BetLineOption is one line a bet can be moved to in the edit form.
type BetLineOption struct {
	OddsID uuid.UUID
	Label  string
}

// BetListFilter contains filters for querying user bets.
type BetListFilter struct {
	Season   *int
	Week     *int
	LeagueID *uuid.UUID
}

// UserBetSummaries returns, of the given games, a one-line description of
// each uncancelled bet userID holds on them, keyed by game ID. The games grid
// and detail page use this to show the reader what they already bet rather
// than making them open the slip again to find out. Settled bets are included
// -- see repository.UserBetRepository.FindByUserAndGames.
//
// A game with more than one bet -- the spread and the total both, say --
// joins them with "; " so the marker still fits on one line.
//
// Picks that describe the same thing collapse to one entry. The same pick in
// two leagues is two rows here but one fact to the reader, and repeating it
// ("WGA +2000; WGA +2000") reads as a rendering bug rather than as two league
// entries. Sorting is what makes that collapse stable: the union query has no
// ORDER BY, so without it the same two bets could join in either order from
// one page load to the next.
func (s *Service) UserBetSummaries(userID uuid.UUID, gameIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := s.userBetRepo.FindByUserAndGames(userID, gameIDs)
	if err != nil {
		return nil, err
	}
	return userBetSummaries(rows), nil
}

// userBetSummaries collapses bet rows into one line per game. Kept
// independent of Service, the way rankingsFromPoll is of SyncService, so the
// collapsing can be tested without a database -- it is the whole of the
// interesting behaviour and none of it needs one.
func userBetSummaries(rows []repository.UserBetRow) map[uuid.UUID]string {
	picks := make(map[uuid.UUID]map[string]bool)
	for _, row := range rows {
		pick := HolyLockPick(repository.LeagueHolyLockRow{
			BetType:      row.BetType,
			Pick:         row.Pick,
			LineValue:    row.LineValue,
			OddsSnapshot: row.OddsSnapshot,
			HomeAbbr:     row.HomeAbbr,
			AwayAbbr:     row.AwayAbbr,
		})
		if picks[row.GameID] == nil {
			picks[row.GameID] = make(map[string]bool)
		}
		picks[row.GameID][pick] = true
	}

	summaries := make(map[uuid.UUID]string, len(picks))
	for gameID, gamePicks := range picks {
		unique := make([]string, 0, len(gamePicks))
		for pick := range gamePicks {
			unique = append(unique, pick)
		}
		sort.Strings(unique)
		summaries[gameID] = strings.Join(unique, "; ")
	}
	return summaries
}

// BetPageSize is how many bets one page of the bets list holds.
const BetPageSize = 100

// Page describes one page of the bets list.
//
// It mirrors games.Page rather than sharing it. The two are view models for
// different templates, and having this package depend on games to render a
// pager would be the wrong direction for one struct of ints.
type Page struct {
	Number int
	Size   int
	Total  int
	Pages  int
	First  int
	Last   int
}

// HasPrev reports whether a page exists before this one.
func (p Page) HasPrev() bool { return p.Number > 1 }

// HasNext reports whether a page exists after this one.
func (p Page) HasNext() bool { return p.Number < p.Pages }

// Prev is the page number before this one, floored at the first page.
func (p Page) Prev() int {
	if p.Number <= 1 {
		return 1
	}
	return p.Number - 1
}

// Next is the page number after this one, capped at the last page.
func (p Page) Next() int {
	if p.Number >= p.Pages {
		return p.Pages
	}
	return p.Number + 1
}

// paginate resolves a requested page number against a total, clamping it into
// range. A page past the end lands on the last page rather than returning
// nothing, which reads as an empty result set rather than as a bad link.
func paginate(page, total int) Page {
	pages := max((total+BetPageSize-1)/BetPageSize, 1)
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}

	first := (page-1)*BetPageSize + 1
	last := min(page*BetPageSize, total)
	if total == 0 {
		first = 0
	}

	return Page{Number: page, Size: BetPageSize, Total: total, Pages: pages, First: first, Last: last}
}

// BetListResult contains the bets and filter options.
type BetListResult struct {
	Bets    []BetView
	Page    Page
	Seasons []int
	Weeks   []int
	Leagues []models.League
}

// GetUserBets retrieves one page of a user's bets, newest first, with optional
// filters.
//
// The page is resolved before anything is loaded: BetPageRepository orders and
// slices across all three bet tables in SQL, and only the bets it names are
// then read. Fetching the three tables whole and slicing in Go would page
// wrongly past page 1, and would run the per-bet work in betViews over the
// user's entire betting history to render a hundred rows of it.
func (s *Service) GetUserBets(userID uuid.UUID, filter BetListFilter, page int) (*BetListResult, error) {
	repoFilter := repository.BetFilter{
		Season:   filter.Season,
		Week:     filter.Week,
		LeagueID: filter.LeagueID,
		UserID:   &userID,
	}

	bets, pageInfo, err := s.loadBetPage(repoFilter, page)
	if err != nil {
		return nil, err
	}
	s.markHolyLockEligibility(userID, bets)

	// Get filter options from the periods the user actually has bets in.
	periods, err := s.betPeriodRepo.FindByUser(userID)
	if err != nil {
		// Losing the dropdowns costs the reader the filters, not the page.
		slog.Error("failed to load bet filter periods", "user", userID, "error", err)
	}
	seasons, weeks := filterOptions(periods, filter.Season)
	leagues, _ := s.leagueRepo.FindUserLeagues(userID)

	return &BetListResult{
		Bets:    bets,
		Page:    pageInfo,
		Seasons: seasons,
		Weeks:   weeks,
		Leagues: leagues,
	}, nil
}

// loadBetPage resolves one page of a filtered bet listing and loads only the
// bets on it.
//
// Both the user's own list and the admin browser over every user page the same
// three tables against the same BetFilter, so the sequence lives here once:
// count, clamp the requested page against that count, ask SQL which bets the
// page holds, then read just those and restore the order SQL chose.
//
// The count and the refs come from the same query builder, so the pager can
// never offer a page the listing cannot fill.
func (s *Service) loadBetPage(filter repository.BetFilter, page int) ([]BetView, Page, error) {
	total, err := s.betPageRepo.CountFiltered(filter)
	if err != nil {
		return nil, Page{}, err
	}

	pageInfo := paginate(page, total)
	refs, err := s.betPageRepo.FindRefs(filter, BetPageSize, (pageInfo.Number-1)*BetPageSize)
	if err != nil {
		return nil, Page{}, err
	}

	var spreadIDs, moneyLineIDs, overUnderIDs []uuid.UUID
	for _, ref := range refs {
		switch ref.BetType {
		case BetTypeSpread:
			spreadIDs = append(spreadIDs, ref.BetID)
		case BetTypeMoneyLine:
			moneyLineIDs = append(moneyLineIDs, ref.BetID)
		case BetTypeOverUnder:
			overUnderIDs = append(overUnderIDs, ref.BetID)
		}
	}

	spreadBets, err := s.spreadBetRepo.FindByIDs(spreadIDs)
	if err != nil {
		return nil, Page{}, err
	}

	moneyLineBets, err := s.moneyLineBetRepo.FindByIDs(moneyLineIDs)
	if err != nil {
		return nil, Page{}, err
	}

	overUnderBets, err := s.overUnderBetRepo.FindByIDs(overUnderIDs)
	if err != nil {
		return nil, Page{}, err
	}

	return orderByRefs(s.betViews(spreadBets, moneyLineBets, overUnderBets), refs), pageInfo, nil
}

// orderByRefs puts views back into the order the page query decided, which is
// the only place the merged ordering across the three tables exists. betViews
// sorts by creation time alone, so bets sharing an instant could otherwise
// come out in a different order than the one the offsets were computed
// against.
//
// A ref with no matching view is dropped: the bet was deleted between the two
// queries, and a short page is a better answer than a zero-valued row.
func orderByRefs(views []BetView, refs []repository.BetRef) []BetView {
	byID := make(map[uuid.UUID]BetView, len(views))
	for _, view := range views {
		byID[view.ID] = view
	}

	ordered := make([]BetView, 0, len(refs))
	for _, ref := range refs {
		if view, ok := byID[ref.BetID]; ok {
			ordered = append(ordered, view)
		}
	}
	return ordered
}

// betViews converts the three bet tables into one list of view models sorted
// newest first. Both the user's own bet list and the admin browser over every
// user render the same rows, so they share this rather than each walking the
// three shapes themselves.
func (s *Service) betViews(spreadBets []models.SpreadBet, moneyLineBets []models.MoneyLineBet, overUnderBets []models.OverUnderBet) []BetView {
	// Convert to unified view.
	var bets []BetView
	// Several bets often share a game, and the edit form needs that game's
	// lines, so look each one up once.
	lines := newLineOptionCache(s)
	now := time.Now()

	for _, bet := range spreadBets {
		pick := bet.Game.HomeTeam.Abbreviation
		if bet.Pick == models.SpreadPickAway {
			pick = bet.Game.AwayTeam.Abbreviation
		}

		view := BetView{
			ID:           bet.ID,
			Type:         "spread",
			Game:         bet.Game,
			League:       bet.League,
			User:         bet.User,
			Pick:         pick,
			PickSide:     string(bet.Pick),
			Line:         formatSpread(bet.SpreadSnapshot),
			OddsSnapshot: bet.OddsSnapshot,
			Stake:        bet.Stake,
			Status:       bet.Status,
			CreatedAt:    bet.CreatedAt,
			OddsID:       bet.SpreadOddsID,
			Editable:     editable(bet.Status, bet.Game, now),
			IsHolyLock:   bet.IsHolyLock,
		}
		if view.Editable {
			view.LineOptions = withCurrentLine(lines.spread(bet.Game), bet.SpreadOddsID,
				fmt.Sprintf("Your line: %s %s (%s)", pick, formatSpread(bet.SpreadSnapshot), formatOdds(bet.OddsSnapshot)))
		}
		bets = append(bets, view)
	}

	for _, bet := range moneyLineBets {
		pick := bet.Game.HomeTeam.Abbreviation
		if bet.Pick == models.MoneyLinePickAway {
			pick = bet.Game.AwayTeam.Abbreviation
		}

		view := BetView{
			ID:           bet.ID,
			Type:         "moneyline",
			Game:         bet.Game,
			League:       bet.League,
			User:         bet.User,
			Pick:         pick,
			PickSide:     string(bet.Pick),
			Line:         formatOdds(bet.OddsSnapshot),
			OddsSnapshot: bet.OddsSnapshot,
			Stake:        bet.Stake,
			Status:       bet.Status,
			CreatedAt:    bet.CreatedAt,
			OddsID:       bet.MoneyLineOddsID,
			Editable:     editable(bet.Status, bet.Game, now),
			IsHolyLock:   bet.IsHolyLock,
		}
		if view.Editable {
			view.LineOptions = withCurrentLine(lines.moneyLine(bet.Game), bet.MoneyLineOddsID,
				fmt.Sprintf("Your line: %s %s", pick, formatOdds(bet.OddsSnapshot)))
		}
		bets = append(bets, view)
	}

	for _, bet := range overUnderBets {
		pick := "Over"
		if bet.Pick == models.OverUnderPickUnder {
			pick = "Under"
		}

		view := BetView{
			ID:           bet.ID,
			Type:         "overunder",
			Game:         bet.Game,
			League:       bet.League,
			User:         bet.User,
			Pick:         pick,
			PickSide:     string(bet.Pick),
			Line:         fmt.Sprintf("O/U %s", bet.TotalSnapshot.String()),
			OddsSnapshot: bet.OddsSnapshot,
			Stake:        bet.Stake,
			Status:       bet.Status,
			CreatedAt:    bet.CreatedAt,
			OddsID:       bet.OverUnderOddsID,
			Editable:     editable(bet.Status, bet.Game, now),
			IsHolyLock:   bet.IsHolyLock,
		}
		if view.Editable {
			view.LineOptions = withCurrentLine(lines.overUnder(bet.Game), bet.OverUnderOddsID,
				fmt.Sprintf("Your line: %s %s (%s)", pick, bet.TotalSnapshot.String(), formatOdds(bet.OddsSnapshot)))
		}
		bets = append(bets, view)
	}

	// Sort by creation date descending.
	sort.Slice(bets, func(i, j int) bool {
		return bets[i].CreatedAt.After(bets[j].CreatedAt)
	})

	return bets
}

// filterOptions reduces the user's bet periods to the choices the page offers.
//
// Weeks narrow to the selected season so the two dropdowns stay consistent.
// With no season selected every week the user has a bet in is offered, which is
// what lets the week filter work on arrival rather than only after a season has
// been picked and the page reloaded.
func filterOptions(periods []repository.BetPeriod, season *int) (seasons, weeks []int) {
	seenSeason := make(map[int]bool)
	seenWeek := make(map[int]bool)

	for _, period := range periods {
		if !seenSeason[period.Season] {
			seenSeason[period.Season] = true
			seasons = append(seasons, period.Season)
		}

		if period.Week == nil || (season != nil && period.Season != *season) {
			continue
		}
		if !seenWeek[*period.Week] {
			seenWeek[*period.Week] = true
			weeks = append(weeks, *period.Week)
		}
	}

	sort.Ints(weeks)
	return seasons, weeks
}

// formatSpread formats a spread value with sign.
func formatSpread(spread decimal.Decimal) string {
	if spread.IsPositive() {
		return "+" + spread.String()
	}
	return spread.String()
}

// formatOdds formats American odds with sign.
func formatOdds(odds decimal.Decimal) string {
	if odds.IsPositive() {
		return "+" + odds.StringFixed(0)
	}
	return odds.StringFixed(0)
}

// editable reports whether a bet can still be changed or cancelled. It mirrors
// the gate in authorizeEdit so the UI never offers an edit the service will
// then refuse.
func editable(status models.BetStatus, game models.Game, now time.Time) bool {
	return status == models.BetStatusPending && game.ScheduledAt.After(now)
}

// withCurrentLine keeps the line a bet is actually on selectable in its edit
// form.
//
// Custom lines are left out of the book list above, so a bet placed on one has
// no matching entry. Without this the select would open on an unrelated line,
// and an edit that only meant to change the stake would silently move the bet
// onto it.
func withCurrentLine(options []BetLineOption, oddsID uuid.UUID, label string) []BetLineOption {
	for _, option := range options {
		if option.OddsID == oddsID {
			return options
		}
	}
	return append([]BetLineOption{{OddsID: oddsID, Label: label}}, options...)
}

// lineOptionCache builds the line choices for the edit form, remembering each
// game so a list holding several bets on one game queries its odds once.
type lineOptionCache struct {
	service    *Service
	spreads    map[uuid.UUID][]BetLineOption
	moneyLines map[uuid.UUID][]BetLineOption
	overUnders map[uuid.UUID][]BetLineOption
}

func newLineOptionCache(service *Service) *lineOptionCache {
	return &lineOptionCache{
		service:    service,
		spreads:    make(map[uuid.UUID][]BetLineOption),
		moneyLines: make(map[uuid.UUID][]BetLineOption),
		overUnders: make(map[uuid.UUID][]BetLineOption),
	}
}

func (c *lineOptionCache) spread(game models.Game) []BetLineOption {
	if cached, ok := c.spreads[game.ID]; ok {
		return cached
	}

	odds, err := c.service.spreadOddsRepo.FindBookLinesByGame(game.ID)
	if err != nil {
		// A missing line list costs the reader the dropdown, not the page, and
		// they can still enter a custom line.
		slog.Error("failed to load spread lines for bet edit", "game", game.ID, "error", err)
		odds = nil
	}

	options := make([]BetLineOption, 0, len(odds))
	for _, o := range odds {
		options = append(options, BetLineOption{
			OddsID: o.ID,
			Label: fmt.Sprintf("%s: %s %s (%s) / %s %s (%s)",
				sourceLabel(o.Source),
				game.HomeTeam.Abbreviation, formatSpread(o.HomeSpread), formatOdds(o.HomeOdds),
				game.AwayTeam.Abbreviation, formatSpread(o.AwaySpread), formatOdds(o.AwayOdds),
			),
		})
	}

	c.spreads[game.ID] = options
	return options
}

func (c *lineOptionCache) moneyLine(game models.Game) []BetLineOption {
	if cached, ok := c.moneyLines[game.ID]; ok {
		return cached
	}

	odds, err := c.service.moneyLineOddsRepo.FindBookLinesByGame(game.ID)
	if err != nil {
		slog.Error("failed to load money lines for bet edit", "game", game.ID, "error", err)
		odds = nil
	}

	options := make([]BetLineOption, 0, len(odds))
	for _, o := range odds {
		options = append(options, BetLineOption{
			OddsID: o.ID,
			Label: fmt.Sprintf("%s: %s %s / %s %s",
				sourceLabel(o.Source),
				game.HomeTeam.Abbreviation, formatOdds(o.HomeOdds),
				game.AwayTeam.Abbreviation, formatOdds(o.AwayOdds),
			),
		})
	}

	c.moneyLines[game.ID] = options
	return options
}

func (c *lineOptionCache) overUnder(game models.Game) []BetLineOption {
	if cached, ok := c.overUnders[game.ID]; ok {
		return cached
	}

	odds, err := c.service.overUnderOddsRepo.FindBookLinesByGame(game.ID)
	if err != nil {
		slog.Error("failed to load totals for bet edit", "game", game.ID, "error", err)
		odds = nil
	}

	options := make([]BetLineOption, 0, len(odds))
	for _, o := range odds {
		options = append(options, BetLineOption{
			OddsID: o.ID,
			Label: fmt.Sprintf("%s: %s (O %s / U %s)",
				sourceLabel(o.Source), o.Total.String(),
				formatOdds(o.OverOdds), formatOdds(o.UnderOdds),
			),
		})
	}

	c.overUnders[game.ID] = options
	return options
}

// sourceLabels gives each odds source its usual casing for display. A source
// missing here falls back to its raw value rather than being hidden.
var sourceLabels = map[models.OddsSource]string{
	models.OddsSourceDraftKings: "DraftKings",
	models.OddsSourceFanDuel:    "FanDuel",
	models.OddsSourceBetMGM:     "BetMGM",
	models.OddsSourceCaesars:    "Caesars",
	models.OddsSourceESPN:       "ESPN",
	models.OddsSourceBovada:     "Bovada",
	models.OddsSourceCustom:     "Custom",
}

func sourceLabel(source models.OddsSource) string {
	if label, ok := sourceLabels[source]; ok {
		return label
	}
	return string(source)
}
