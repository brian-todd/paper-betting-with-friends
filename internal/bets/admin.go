package bets

import (
	"errors"
	"fmt"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ErrInvalidBetStatus is returned when a bet is asked to move to a status that
// is not one of the five a bet can hold.
var ErrInvalidBetStatus = errors.New("invalid bet status")

// Bet type identifiers shared by the routes and the view models.
const (
	BetTypeSpread    = "spread"
	BetTypeMoneyLine = "moneyline"
	BetTypeOverUnder = "overunder"
)

// creditedFor is how much of a bet has been returned to the purse while it sits
// in a given status. The stake is taken at placement and never comes back while
// the bet is pending or lost; a push or a void refunds it; a win repays it with
// the winnings.
//
// Expressing each status as an absolute position, rather than writing the
// bookkeeping for each transition, is what makes purseDelta a subtraction.
func creditedFor(stake, odds decimal.Decimal, status models.BetStatus) decimal.Decimal {
	switch status {
	case models.BetStatusWon:
		return calculatePayout(stake, odds)
	case models.BetStatusPush, models.BetStatusVoid:
		return stake
	default:
		// Pending and lost: the stake stays with the house.
		return decimal.Zero
	}
}

// purseDelta is the amount to credit a purse (negative to debit it) when a bet
// moves between statuses. Every transition falls out of the difference,
// including corrections that run backwards such as won -> lost.
func purseDelta(stake, odds decimal.Decimal, from, to models.BetStatus) decimal.Decimal {
	return creditedFor(stake, odds, to).Sub(creditedFor(stake, odds, from))
}

// validBetStatus reports whether a status is one an admin may set.
func validBetStatus(status models.BetStatus) bool {
	switch status {
	case models.BetStatusPending, models.BetStatusWon, models.BetStatusLost,
		models.BetStatusPush, models.BetStatusVoid:
		return true
	default:
		return false
	}
}

// AdminSetBetStatus forces a bet into a status and moves the purse to match.
//
// It deliberately skips every check Cancel*Bet and authorizeEdit apply: there
// is no ownership test and no gate on the game having started, because the
// cases this exists for -- a line that was recorded wrong, a game the provider
// never finalized -- only arise after the fact.
func (s *Service) AdminSetBetStatus(betType string, betID uuid.UUID, to models.BetStatus) error {
	if !validBetStatus(to) {
		return ErrInvalidBetStatus
	}

	switch betType {
	case BetTypeSpread:
		bet, err := s.spreadBetRepo.FindByID(betID)
		if err != nil {
			return wrapBetLookup(err)
		}
		from := bet.Status
		if from == to {
			return nil
		}
		bet.Status = to
		if err := s.spreadBetRepo.Update(bet); err != nil {
			return err
		}
		return s.applyPurseDelta(bet.UserID, bet.LeagueID, purseDelta(bet.Stake, bet.OddsSnapshot, from, to))

	case BetTypeMoneyLine:
		bet, err := s.moneyLineBetRepo.FindByID(betID)
		if err != nil {
			return wrapBetLookup(err)
		}
		from := bet.Status
		if from == to {
			return nil
		}
		bet.Status = to
		if err := s.moneyLineBetRepo.Update(bet); err != nil {
			return err
		}
		return s.applyPurseDelta(bet.UserID, bet.LeagueID, purseDelta(bet.Stake, bet.OddsSnapshot, from, to))

	case BetTypeOverUnder:
		bet, err := s.overUnderBetRepo.FindByID(betID)
		if err != nil {
			return wrapBetLookup(err)
		}
		from := bet.Status
		if from == to {
			return nil
		}
		bet.Status = to
		if err := s.overUnderBetRepo.Update(bet); err != nil {
			return err
		}
		return s.applyPurseDelta(bet.UserID, bet.LeagueID, purseDelta(bet.Stake, bet.OddsSnapshot, from, to))

	default:
		return ErrInvalidBetType
	}
}

// AdminVoidBet voids a bet and refunds its stake whatever state it is in.
func (s *Service) AdminVoidBet(betType string, betID uuid.UUID) error {
	return s.AdminSetBetStatus(betType, betID, models.BetStatusVoid)
}

// applyPurseDelta moves a purse by delta.
//
// It credits a negative amount rather than calling DeductStake, whose
// balance >= amount guard would refuse a correction against a purse the user
// has since spent down -- leaving the bet's status and the balance permanently
// disagreeing. An operator correction has to land, and a purse that goes
// negative as a result is visible and fixable; a silent no-op is not.
func (s *Service) applyPurseDelta(userID, leagueID uuid.UUID, delta decimal.Decimal) error {
	if delta.IsZero() {
		return nil
	}
	return s.purseRepo.CreditWinnings(userID, leagueID, delta)
}

// wrapBetLookup turns a missing row into the package's domain error.
func wrapBetLookup(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrBetNotFound
	}
	return err
}

// ListAllBets returns one page of the bets matching filter, across all users
// and leagues, newest first, as the same view model the user-facing list uses.
//
// It pages for the same reason the user's own list does, but with more at
// stake: nothing here is scoped to one user, so an unfiltered call would load
// every bet in the system with seven preloads each and grow linearly forever.
func (s *Service) ListAllBets(filter repository.BetFilter, page int) ([]BetView, Page, error) {
	views, pageInfo, err := s.loadBetPage(filter, page)
	if err != nil {
		return nil, Page{}, fmt.Errorf("loading bets: %w", err)
	}
	return views, pageInfo, nil
}

// FindGameResult returns the recorded score for a game, or nil when the
// provider has not reported one yet.
func (s *Service) FindGameResult(gameID uuid.UUID) (*models.GameResult, error) {
	result, err := s.gameResultRepo.FindByGameID(gameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

// FinalizeGameResult marks a provisional score final and settles the game's
// pending bets against it.
//
// This is the manual escape hatch for a provider that reports a score but never
// calls the game complete. It settles real money, so callers must confirm.
func (s *Service) FinalizeGameResult(gameID uuid.UUID, at time.Time) error {
	result, err := s.gameResultRepo.FindByGameID(gameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGameNotFound
		}
		return err
	}

	if !result.IsFinal() {
		result.FinalizedAt = &at
		if err := s.gameResultRepo.Update(result); err != nil {
			return err
		}
	}

	return s.EvaluateBetsForGame(gameID)
}
