package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Audit actions recorded by the admin portal.
const (
	AuditActionUserPasswordReset = "user.password_reset"
	AuditActionUserRenamed       = "user.renamed"
	AuditActionUserDeleted       = "user.deleted"
	AuditActionLeagueCreated     = "league.created"
	AuditActionLeagueDeleted     = "league.deleted"
	AuditActionMemberAdded       = "league.member_added"
	AuditActionMemberRemoved     = "league.member_removed"
	AuditActionPurseSet          = "purse.balance_set"
	AuditActionBetStatusSet      = "bet.status_set"
	AuditActionSyncTriggered     = "sync.triggered"
	AuditActionGameEvaluated     = "game.evaluated"
	AuditActionGameFinalized     = "game.finalized"
)

// Audit target types.
const (
	AuditTargetUser   = "user"
	AuditTargetLeague = "league"
	AuditTargetPurse  = "purse"
	AuditTargetBet    = "bet"
	AuditTargetGame   = "game"
	AuditTargetSync   = "sync"
)

// AuditLog is one mutation performed through the admin portal.
type AuditLog struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

	// ActorID is nulled if the acting user is later deleted, which is why
	// ActorUsername holds a snapshot taken at the time of the action.
	ActorID       *uuid.UUID `gorm:"type:uuid"`
	ActorUsername string     `gorm:"type:varchar(50);not null"`

	Action     string     `gorm:"type:varchar(64);not null"`
	TargetType string     `gorm:"type:varchar(32);not null"`
	TargetID   *uuid.UUID `gorm:"type:uuid"`

	// Detail carries human-readable context, typically a before -> after pair.
	// It must never contain a password or a password hash.
	Detail string `gorm:"type:text;not null;default:''"`

	CreatedAt time.Time
}

// TableName pins the table, which is namespaced to the admin portal rather
// than named after the model.
func (AuditLog) TableName() string {
	return "admin_audit_log"
}

// BeforeCreate sets the UUID before creating a new audit entry.
func (a *AuditLog) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
