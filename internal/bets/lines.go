package bets

import (
	"errors"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// A bet stores both the odds row it was taken from and a snapshot of the
// numbers at that moment, so a later line move never changes what was agreed.
// Placing and editing a bet resolve that pair identically, which is what these
// selection types and their resolvers exist to share.

// spreadSelection is the odds row a spread bet points at, plus the values
// frozen onto the bet.
type spreadSelection struct {
	OddsID uuid.UUID
	Spread decimal.Decimal
	Odds   decimal.Decimal
}

// resolveSpreadOdds picks the spread line for a bet, either an existing odds
// row or a newly created custom one.
func (s *Service) resolveSpreadOdds(
	gameID uuid.UUID,
	pick models.SpreadPick,
	oddsID *uuid.UUID,
	customSpread, customOdds *decimal.Decimal,
) (spreadSelection, error) {
	if oddsID != nil {
		odds, err := s.spreadOddsRepo.FindByID(*oddsID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return spreadSelection{}, ErrOddsNotFound
			}
			return spreadSelection{}, err
		}
		// An odds row belongs to one game. Taking it on another would let a
		// caller staple any line onto any matchup.
		if odds.GameID != gameID {
			return spreadSelection{}, ErrOddsNotFound
		}

		selection := spreadSelection{OddsID: odds.ID, Spread: odds.HomeSpread, Odds: odds.HomeOdds}
		if pick == models.SpreadPickAway {
			selection.Spread, selection.Odds = odds.AwaySpread, odds.AwayOdds
		}
		return selection, nil
	}

	if customSpread == nil || customOdds == nil {
		return spreadSelection{}, ErrOddsNotFound
	}

	homeSpread, awaySpread := mirrorSpread(pick, *customSpread)

	custom := &models.SpreadOdds{
		GameID:     gameID,
		Source:     models.OddsSourceCustom,
		HomeSpread: homeSpread,
		AwaySpread: awaySpread,
		HomeOdds:   *customOdds,
		AwayOdds:   *customOdds,
	}
	if err := s.spreadOddsRepo.Create(custom); err != nil {
		return spreadSelection{}, err
	}

	return spreadSelection{OddsID: custom.ID, Spread: *customSpread, Odds: *customOdds}, nil
}

// moneyLineSelection is the odds row a money line bet points at, plus the odds
// frozen onto the bet.
type moneyLineSelection struct {
	OddsID uuid.UUID
	Odds   decimal.Decimal
}

// resolveMoneyLineOdds picks the money line for a bet, either an existing odds
// row or a newly created custom one.
func (s *Service) resolveMoneyLineOdds(
	gameID uuid.UUID,
	pick models.MoneyLinePick,
	oddsID *uuid.UUID,
	customHomeOdds, customAwayOdds *decimal.Decimal,
) (moneyLineSelection, error) {
	if oddsID != nil {
		odds, err := s.moneyLineOddsRepo.FindByID(*oddsID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return moneyLineSelection{}, ErrOddsNotFound
			}
			return moneyLineSelection{}, err
		}
		if odds.GameID != gameID {
			return moneyLineSelection{}, ErrOddsNotFound
		}

		selection := moneyLineSelection{OddsID: odds.ID, Odds: odds.HomeOdds}
		if pick == models.MoneyLinePickAway {
			selection.Odds = odds.AwayOdds
		}
		return selection, nil
	}

	if customHomeOdds == nil || customAwayOdds == nil {
		return moneyLineSelection{}, ErrOddsNotFound
	}

	custom := &models.MoneyLineOdds{
		GameID:   gameID,
		Source:   models.OddsSourceCustom,
		HomeOdds: *customHomeOdds,
		AwayOdds: *customAwayOdds,
	}
	if err := s.moneyLineOddsRepo.Create(custom); err != nil {
		return moneyLineSelection{}, err
	}

	selection := moneyLineSelection{OddsID: custom.ID, Odds: custom.HomeOdds}
	if pick == models.MoneyLinePickAway {
		selection.Odds = custom.AwayOdds
	}
	return selection, nil
}

// overUnderSelection is the odds row an over/under bet points at, plus the
// values frozen onto the bet.
type overUnderSelection struct {
	OddsID uuid.UUID
	Total  decimal.Decimal
	Odds   decimal.Decimal
}

// resolveOverUnderOdds picks the total for a bet, either an existing odds row
// or a newly created custom one.
func (s *Service) resolveOverUnderOdds(
	gameID uuid.UUID,
	pick models.OverUnderPick,
	oddsID *uuid.UUID,
	customTotal, customOverOdds, customUnderOdds *decimal.Decimal,
) (overUnderSelection, error) {
	if oddsID != nil {
		odds, err := s.overUnderOddsRepo.FindByID(*oddsID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return overUnderSelection{}, ErrOddsNotFound
			}
			return overUnderSelection{}, err
		}
		if odds.GameID != gameID {
			return overUnderSelection{}, ErrOddsNotFound
		}

		selection := overUnderSelection{OddsID: odds.ID, Total: odds.Total, Odds: odds.OverOdds}
		if pick == models.OverUnderPickUnder {
			selection.Odds = odds.UnderOdds
		}
		return selection, nil
	}

	if customTotal == nil || customOverOdds == nil || customUnderOdds == nil {
		return overUnderSelection{}, ErrOddsNotFound
	}

	custom := &models.OverUnderOdds{
		GameID:    gameID,
		Source:    models.OddsSourceCustom,
		Total:     *customTotal,
		OverOdds:  *customOverOdds,
		UnderOdds: *customUnderOdds,
	}
	if err := s.overUnderOddsRepo.Create(custom); err != nil {
		return overUnderSelection{}, err
	}

	selection := overUnderSelection{OddsID: custom.ID, Total: custom.Total, Odds: custom.OverOdds}
	if pick == models.OverUnderPickUnder {
		selection.Odds = custom.UnderOdds
	}
	return selection, nil
}

// mirrorSpread turns a spread entered relative to the picked team into the pair
// stored on an odds row.
//
// A custom line is written from the bettor's point of view -- "I'm taking them
// at -7" -- so the opposite side is the negation, whichever team was picked.
func mirrorSpread(pick models.SpreadPick, spread decimal.Decimal) (home, away decimal.Decimal) {
	if pick == models.SpreadPickAway {
		return spread.Neg(), spread
	}
	return spread, spread.Neg()
}
