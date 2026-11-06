package repository

import (
	"slices"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameTeamStatsRepository stores basketball box scores.
type GameTeamStatsRepository struct {
	db *gorm.DB
}

// NewGameTeamStatsRepository creates a new GameTeamStatsRepository.
func NewGameTeamStatsRepository(db *gorm.DB) *GameTeamStatsRepository {
	return &GameTeamStatsRepository{db: db}
}

// Upsert writes one team's box score in a game, replacing any previous one, which is how
// a box that disagreed with the final score gets corrected.
func (r *GameTeamStatsRepository) Upsert(stats *models.GameTeamStats) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "game_id"}, {Name: "team_id"}},
		DoUpdates: clause.AssignmentColumns(slices.Concat(
			[]string{"fetched_at", "updated_at"}, models.TeamBoxColumns,
		)),
	}).Create(stats).Error
}

// GamesAwaitingBox returns, keyed by external ID, the basketball games tipping off in
// [start, end) that are finished and have no box score the page would show.
//
// The condition is the page's own: teamStatsPanel requires a final status and a final
// result, then boxAgrees requires both sides' points to match the score. A box that fails
// it has to keep the game waiting, or the page would reject it and nothing would replace it.
func (r *GameTeamStatsRepository) GamesAwaitingBox(start, end time.Time) (map[int64]uuid.UUID, error) {
	var rows []struct {
		ExternalID int64
		ID         uuid.UUID
	}
	err := r.db.Raw(`
		SELECT g.external_id, g.id
		FROM games g
		JOIN game_results gr ON gr.game_id = g.id
		WHERE g.sport = ? AND g.status = ? AND g.external_id IS NOT NULL
			AND gr.finalized_at IS NOT NULL
			AND g.scheduled_at >= ? AND g.scheduled_at < ?
			AND NOT (
				EXISTS (SELECT 1 FROM game_team_stats s WHERE s.game_id = g.id AND s.team_id = g.home_team_id AND s.points = gr.home_score)
				AND EXISTS (SELECT 1 FROM game_team_stats s WHERE s.game_id = g.id AND s.team_id = g.away_team_id AND s.points = gr.away_score)
			)`, models.SportBasketball, models.GameStatusFinal, start, end).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	byExternal := make(map[int64]uuid.UUID, len(rows))
	for _, row := range rows {
		byExternal[row.ExternalID] = row.ID
	}
	return byExternal, nil
}

// FindForGame returns a game's box scores keyed by team ID. A team with no row is
// absent from the map.
func (r *GameTeamStatsRepository) FindForGame(gameID uuid.UUID) (map[uuid.UUID]models.GameTeamStats, error) {
	var rows []models.GameTeamStats
	if err := r.db.Where("game_id = ?", gameID).Find(&rows).Error; err != nil {
		return nil, err
	}

	stats := make(map[uuid.UUID]models.GameTeamStats, len(rows))
	for _, row := range rows {
		stats[row.TeamID] = row
	}
	return stats, nil
}
