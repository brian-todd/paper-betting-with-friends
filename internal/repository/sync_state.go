package repository

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SyncStateRepository provides access to the last successful sync times.
type SyncStateRepository struct {
	db *gorm.DB
}

// NewSyncStateRepository creates a new SyncStateRepository.
func NewSyncStateRepository(db *gorm.DB) *SyncStateRepository {
	return &SyncStateRepository{db: db}
}

// RecordSuccess stores the time a job last succeeded, replacing any previous
// value for that job.
func (r *SyncStateRepository) RecordSuccess(job string, at time.Time) error {
	state := &models.SyncState{Job: job, LastSuccessAt: at}

	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "job"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_success_at", "updated_at"}),
	}).Create(state).Error
}

// FindAll returns the last success time for every job that has ever recorded
// one, keyed by job name.
func (r *SyncStateRepository) FindAll() (map[string]time.Time, error) {
	var states []models.SyncState
	if err := r.db.Find(&states).Error; err != nil {
		return nil, err
	}

	byJob := make(map[string]time.Time, len(states))
	for _, state := range states {
		byJob[state.Job] = state.LastSuccessAt
	}
	return byJob, nil
}
