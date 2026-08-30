package admin

import (
	"log/slog"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

// auditLogLimit is how many entries the audit page shows.
const auditLogLimit = 200

// audit records one admin action.
//
// A failure to write the trail is logged and swallowed rather than returned:
// the operation it describes has already been committed, so surfacing the error
// would report a failure that did not happen and tempt the operator into
// repeating a mutation that already landed.
func (s *Service) audit(actor *models.User, action, targetType string, targetID *uuid.UUID, detail string) {
	entry := &models.AuditLog{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
	}
	if actor != nil {
		entry.ActorID = &actor.ID
		entry.ActorUsername = actor.Username
	} else {
		entry.ActorUsername = "system"
	}

	if err := s.auditRepo.Create(entry); err != nil {
		slog.Error("failed to write admin audit entry",
			"action", action, "target_type", targetType, "error", err)
	}
}

// ListAuditLog returns the most recent admin actions, newest first.
func (s *Service) ListAuditLog() ([]models.AuditLog, error) {
	return s.auditRepo.FindRecent(auditLogLimit)
}
