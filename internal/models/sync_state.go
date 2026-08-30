package models

import "time"

// SyncState records when a background sync last succeeded.
//
// Unlike the rest of the models this has no UUID primary key: the job name is
// already a natural key, there is exactly one row per job, and nothing
// references it. A surrogate key here would only be ceremony.
type SyncState struct {
	// Job is the scheduler job name, e.g. "cfb-games-and-lines".
	Job string `gorm:"primaryKey"`

	// LastSuccessAt is when the job last completed without error. A run that
	// failed does not update it, because a failed run refreshed nothing.
	LastSuccessAt time.Time `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for SyncState.
func (SyncState) TableName() string {
	return "sync_state"
}
