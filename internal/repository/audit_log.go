package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"gorm.io/gorm"
)

// AuditLogRepository provides access to the admin audit trail.
type AuditLogRepository struct {
	db *gorm.DB
}

// NewAuditLogRepository creates a new AuditLogRepository.
func NewAuditLogRepository(db *gorm.DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

// Create records one audit entry.
func (r *AuditLogRepository) Create(entry *models.AuditLog) error {
	return r.db.Create(entry).Error
}

// FindRecent returns the newest entries first, capped at limit.
func (r *AuditLogRepository) FindRecent(limit int) ([]models.AuditLog, error) {
	var entries []models.AuditLog
	if err := r.db.Order("created_at DESC").Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}
