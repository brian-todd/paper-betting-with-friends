package admin

import (
	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
)

// ListBets returns one page of the bets matching filter, across all users and
// leagues.
func (s *Service) ListBets(filter repository.BetFilter, page int) ([]bets.BetView, bets.Page, error) {
	return s.bets.ListAllBets(filter, page)
}

// SetBetStatus forces a bet into a status and moves the purse to match.
func (s *Service) SetBetStatus(actor *models.User, betType string, betID uuid.UUID, status models.BetStatus) error {
	if err := s.bets.AdminSetBetStatus(betType, betID, status); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionBetStatusSet, models.AuditTargetBet, &betID,
		betType+" -> "+string(status))
	return nil
}
