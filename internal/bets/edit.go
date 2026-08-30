package bets

import (
	"errors"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Editing a pending bet is cancel-and-replace without the round trip: the same
// gates apply as placing one, and the purse moves by the difference in stake
// rather than out and back in full.

// UpdateSpreadBetInput contains the input for editing a pending spread bet.
type UpdateSpreadBetInput struct {
	BetID  uuid.UUID
	UserID uuid.UUID
	Pick   models.SpreadPick
	Stake  decimal.Decimal
	// For existing odds.
	OddsID *uuid.UUID
	// For custom odds.
	CustomSpread *decimal.Decimal
	CustomOdds   *decimal.Decimal
}

// UpdateSpreadBet changes the stake, pick, or line of a pending spread bet.
func (s *Service) UpdateSpreadBet(input UpdateSpreadBetInput) (*models.SpreadBet, error) {
	bet, err := s.spreadBetRepo.FindByID(input.BetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBetNotFound
		}
		return nil, err
	}

	if err := s.authorizeEdit(bet.UserID, input.UserID, bet.Status, bet.GameID, input.Stake); err != nil {
		return nil, err
	}

	selection, err := s.resolveSpreadOdds(bet.GameID, input.Pick, input.OddsID, input.CustomSpread, input.CustomOdds)
	if err != nil {
		return nil, err
	}

	previousStake := bet.Stake
	if err := s.adjustStake(bet.UserID, bet.LeagueID, previousStake, input.Stake); err != nil {
		return nil, err
	}

	bet.Pick = input.Pick
	bet.SpreadOddsID = selection.OddsID
	bet.SpreadSnapshot = selection.Spread
	bet.OddsSnapshot = selection.Odds
	bet.Stake = input.Stake

	if err := s.spreadBetRepo.Update(bet); err != nil {
		// Put the purse back: the stake moved but the bet did not.
		_ = s.adjustStake(bet.UserID, bet.LeagueID, input.Stake, previousStake)
		return nil, err
	}

	return bet, nil
}

// UpdateMoneyLineBetInput contains the input for editing a pending money line bet.
type UpdateMoneyLineBetInput struct {
	BetID  uuid.UUID
	UserID uuid.UUID
	Pick   models.MoneyLinePick
	Stake  decimal.Decimal
	// For existing odds.
	OddsID *uuid.UUID
	// For custom odds.
	CustomHomeOdds *decimal.Decimal
	CustomAwayOdds *decimal.Decimal
}

// UpdateMoneyLineBet changes the stake, pick, or line of a pending money line bet.
func (s *Service) UpdateMoneyLineBet(input UpdateMoneyLineBetInput) (*models.MoneyLineBet, error) {
	bet, err := s.moneyLineBetRepo.FindByID(input.BetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBetNotFound
		}
		return nil, err
	}

	if err := s.authorizeEdit(bet.UserID, input.UserID, bet.Status, bet.GameID, input.Stake); err != nil {
		return nil, err
	}

	selection, err := s.resolveMoneyLineOdds(bet.GameID, input.Pick, input.OddsID, input.CustomHomeOdds, input.CustomAwayOdds)
	if err != nil {
		return nil, err
	}

	previousStake := bet.Stake
	if err := s.adjustStake(bet.UserID, bet.LeagueID, previousStake, input.Stake); err != nil {
		return nil, err
	}

	bet.Pick = input.Pick
	bet.MoneyLineOddsID = selection.OddsID
	bet.OddsSnapshot = selection.Odds
	bet.Stake = input.Stake

	if err := s.moneyLineBetRepo.Update(bet); err != nil {
		_ = s.adjustStake(bet.UserID, bet.LeagueID, input.Stake, previousStake)
		return nil, err
	}

	return bet, nil
}

// UpdateOverUnderBetInput contains the input for editing a pending over/under bet.
type UpdateOverUnderBetInput struct {
	BetID  uuid.UUID
	UserID uuid.UUID
	Pick   models.OverUnderPick
	Stake  decimal.Decimal
	// For existing odds.
	OddsID *uuid.UUID
	// For custom odds.
	CustomTotal     *decimal.Decimal
	CustomOverOdds  *decimal.Decimal
	CustomUnderOdds *decimal.Decimal
}

// UpdateOverUnderBet changes the stake, pick, or total of a pending over/under bet.
func (s *Service) UpdateOverUnderBet(input UpdateOverUnderBetInput) (*models.OverUnderBet, error) {
	bet, err := s.overUnderBetRepo.FindByID(input.BetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBetNotFound
		}
		return nil, err
	}

	if err := s.authorizeEdit(bet.UserID, input.UserID, bet.Status, bet.GameID, input.Stake); err != nil {
		return nil, err
	}

	selection, err := s.resolveOverUnderOdds(
		bet.GameID, input.Pick, input.OddsID,
		input.CustomTotal, input.CustomOverOdds, input.CustomUnderOdds,
	)
	if err != nil {
		return nil, err
	}

	previousStake := bet.Stake
	if err := s.adjustStake(bet.UserID, bet.LeagueID, previousStake, input.Stake); err != nil {
		return nil, err
	}

	bet.Pick = input.Pick
	bet.OverUnderOddsID = selection.OddsID
	bet.TotalSnapshot = selection.Total
	bet.OddsSnapshot = selection.Odds
	bet.Stake = input.Stake

	if err := s.overUnderBetRepo.Update(bet); err != nil {
		_ = s.adjustStake(bet.UserID, bet.LeagueID, input.Stake, previousStake)
		return nil, err
	}

	return bet, nil
}

// authorizeEdit reports whether a bet may still be changed: the caller owns it,
// it has not been settled, the stake is positive, and the game has not started.
//
// The kickoff gate reads ScheduledAt rather than Game.Status, matching bet
// creation. Status only advances when the sync runs, so between kickoff and the
// next sync it still says "scheduled" -- close enough for a badge, not for
// deciding whether someone may still move a line.
func (s *Service) authorizeEdit(ownerID, userID uuid.UUID, status models.BetStatus, gameID uuid.UUID, stake decimal.Decimal) error {
	if ownerID != userID {
		return ErrNotBetOwner
	}
	if status != models.BetStatusPending {
		return ErrBetNotPending
	}
	if stake.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidStake
	}

	game, err := s.gameRepo.FindByID(gameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGameNotFound
		}
		return err
	}
	if !game.ScheduledAt.After(time.Now()) {
		return ErrGameStarted
	}

	return nil
}

// adjustStake moves the difference between an old and a new stake in or out of
// the purse.
//
// Only the delta moves, so raising a $10 bet to $15 needs $5 free rather than
// the full $15 that a refund-and-recharge would briefly require.
func (s *Service) adjustStake(userID, leagueID uuid.UUID, from, to decimal.Decimal) error {
	delta := to.Sub(from)

	switch {
	case delta.IsPositive():
		if err := s.purseRepo.DeductStake(userID, leagueID, delta); err != nil {
			if errors.Is(err, repository.ErrInsufficientBalance) {
				return ErrInsufficientFunds
			}
			return err
		}
	case delta.IsNegative():
		return s.purseRepo.CreditWinnings(userID, leagueID, delta.Neg())
	}

	return nil
}
