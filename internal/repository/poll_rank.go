package repository

import (
	"database/sql"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PollRankRepository stores basketball poll positions, which are read by date.
type PollRankRepository struct {
	db *gorm.DB
}

// NewPollRankRepository creates a new PollRankRepository.
func NewPollRankRepository(db *gorm.DB) *PollRankRepository {
	return &PollRankRepository{db: db}
}

// Upsert writes ranks in one statement, so a run stores a whole response or none
// of it. A statement cannot touch one row twice, so ranks must hold one entry per
// team, poll and date.
func (r *PollRankRepository) Upsert(ranks []models.PollRank) error {
	if len(ranks) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "team_id"}, {Name: "poll"}, {Name: "poll_date"}},
		DoUpdates: clause.Assignments(map[string]any{
			"season":     gorm.Expr("excluded.season"),
			"rank":       gorm.Expr("excluded.rank"),
			"updated_at": gorm.Expr("excluded.updated_at"),
		}),
	}).Create(&ranks).Error
}

// AsOf returns each team's rank, by team ID, in the season's latest poll dated at
// or before the given instant.
//
// The latest rather than the current week's, because a poll can skip a week: AP
// has no rows for 2025-12-29, and neither poll publishes between Selection Sunday
// and the final one. A team keeps the last position it was given until a newer
// poll says otherwise. An empty map means no poll has come out yet.
func (r *PollRankRepository) AsOf(season int, poll string, at time.Time) (map[uuid.UUID]int, error) {
	latest := r.db.Model(&models.PollRank{}).
		Select("MAX(poll_date)").
		Where("season = ? AND poll = ? AND poll_date <= ?", season, poll, at)

	var rows []models.PollRank
	err := r.db.
		Where("season = ? AND poll = ? AND poll_date = (?)", season, poll, latest).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	ranks := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		ranks[row.TeamID] = row.Rank
	}
	return ranks, nil
}

// LatestPollDate returns the newest poll date stored for the season, and false
// when it holds none.
func (r *PollRankRepository) LatestPollDate(season int, poll string) (time.Time, bool, error) {
	// NullTime because an aggregate over no rows is NULL, not no row.
	var latest sql.NullTime
	err := r.db.Model(&models.PollRank{}).
		Select("MAX(poll_date)").
		Where("season = ? AND poll = ?", season, poll).
		Scan(&latest).Error
	if err != nil || !latest.Valid {
		return time.Time{}, false, err
	}
	return latest.Time, true, nil
}
