package admin

import (
	"strconv"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

// gameSearchLimit caps the admin game search so a broad query cannot pull the
// whole schedule into one page.
const gameSearchLimit = 100

// GameDetail is everything the inspector shows for one game.
type GameDetail struct {
	Game   models.Game
	Result *models.GameResult

	// ResultFinal distinguishes a settled score from a provisional one. The
	// presence of a result row does not mean the game is over: a row is written
	// whenever the provider reports a score, and bets only settle once
	// FinalizedAt is set.
	ResultFinal bool

	// FinalizedAt is the same value flattened out of its pointer, because
	// localTime takes a time.Time and a template cannot dereference one.
	FinalizedAt time.Time

	SpreadOdds    []models.SpreadOdds
	MoneyLineOdds []models.MoneyLineOdds
	OverUnderOdds []models.OverUnderOdds
}

// SearchGames finds games whose home or away team name matches query.
func (s *Service) SearchGames(query string) ([]models.Game, error) {
	return s.gameRepo.SearchByTeamName(query, gameSearchLimit)
}

// GetGameDetail loads a game with its odds and result.
func (s *Service) GetGameDetail(gameID uuid.UUID) (*GameDetail, error) {
	game, err := s.gameRepo.FindByID(gameID)
	if err != nil {
		return nil, err
	}

	detail := &GameDetail{Game: *game}

	result, err := s.bets.FindGameResult(gameID)
	if err != nil {
		return nil, err
	}
	if result != nil {
		detail.Result = result
		detail.ResultFinal = result.IsFinal()
		if result.FinalizedAt != nil {
			detail.FinalizedAt = *result.FinalizedAt
		}
	}

	if detail.SpreadOdds, err = s.spreadOddsRepo.FindByGame(gameID); err != nil {
		return nil, err
	}
	if detail.MoneyLineOdds, err = s.moneyLineOddsRepo.FindByGame(gameID); err != nil {
		return nil, err
	}
	if detail.OverUnderOdds, err = s.overUnderOddsRepo.FindByGame(gameID); err != nil {
		return nil, err
	}

	return detail, nil
}

// EvaluateGame settles the game's pending bets against its recorded score. It
// is a no-op while that score is still provisional.
func (s *Service) EvaluateGame(actor *models.User, gameID uuid.UUID) error {
	if err := s.bets.EvaluateBetsForGame(gameID); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionGameEvaluated, models.AuditTargetGame, &gameID, "")
	return nil
}

// FinalizeGame marks a provisional score final and settles against it.
//
// This exists for a provider that reports a score but never calls the game
// complete. It pays out real bets, so the route behind it confirms first.
func (s *Service) FinalizeGame(actor *models.User, gameID uuid.UUID) error {
	detail, err := s.GetGameDetail(gameID)
	if err != nil {
		return err
	}

	score := ""
	if detail.Result != nil {
		score = detail.Game.HomeTeam.Abbreviation + " " + itoa(detail.Result.HomeScore) +
			" - " + detail.Game.AwayTeam.Abbreviation + " " + itoa(detail.Result.AwayScore)
	}

	if err := s.bets.FinalizeGameResult(gameID, time.Now()); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionGameFinalized, models.AuditTargetGame, &gameID, score)
	return nil
}

// itoa keeps the audit detail readable without dragging fmt into this file.
func itoa(n int) string {
	return strconv.Itoa(n)
}
