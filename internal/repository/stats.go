package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"gorm.io/gorm"
)

// Stats is a set of row counts used by the admin health page to answer "is
// there actually any data in here" without opening a psql session.
type Stats struct {
	Users        int64
	Leagues      int64
	Teams        int64
	Games        int64
	Results      int64
	FinalResults int64
	PendingBets  int64
}

// StatsRepository provides aggregate counts across the schema.
type StatsRepository struct {
	db *gorm.DB
}

// NewStatsRepository creates a new StatsRepository.
func NewStatsRepository(db *gorm.DB) *StatsRepository {
	return &StatsRepository{db: db}
}

// Counts returns the row counts shown on the admin health page.
func (r *StatsRepository) Counts() (Stats, error) {
	var stats Stats

	counts := []struct {
		model any
		where string
		args  []any
		into  *int64
	}{
		{&models.User{}, "", nil, &stats.Users},
		{&models.League{}, "", nil, &stats.Leagues},
		{&models.Team{}, "", nil, &stats.Teams},
		{&models.Game{}, "", nil, &stats.Games},
		{&models.GameResult{}, "", nil, &stats.Results},
		{&models.GameResult{}, "finalized_at IS NOT NULL", nil, &stats.FinalResults},
	}

	for _, c := range counts {
		query := r.db.Model(c.model)
		if c.where != "" {
			query = query.Where(c.where, c.args...)
		}
		if err := query.Count(c.into).Error; err != nil {
			return stats, err
		}
	}

	// Pending bets live in three tables, so they are summed rather than counted.
	for _, model := range []any{&models.SpreadBet{}, &models.MoneyLineBet{}, &models.OverUnderBet{}} {
		var n int64
		if err := r.db.Model(model).Where("status = ?", models.BetStatusPending).Count(&n).Error; err != nil {
			return stats, err
		}
		stats.PendingBets += n
	}

	return stats, nil
}
