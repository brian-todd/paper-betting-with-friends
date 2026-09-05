package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameLiveStateRepository provides methods for interacting with the scoreboard
// state attached to a game.
type GameLiveStateRepository struct {
	db *gorm.DB
}

// NewGameLiveStateRepository creates a new GameLiveStateRepository.
func NewGameLiveStateRepository(db *gorm.DB) *GameLiveStateRepository {
	return &GameLiveStateRepository{db: db}
}

// FindByGameID retrieves the live state for a game.
func (r *GameLiveStateRepository) FindByGameID(gameID uuid.UUID) (*models.GameLiveState, error) {
	var state models.GameLiveState
	if err := r.db.Where("game_id = ?", gameID).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

// Upsert creates or updates the live state for a game.
//
// Every column is overwritten, including with nulls: the scoreboard clears the
// clock and the situation once a game ends, and keeping the last values from
// while it was running would leave a finished game showing "3rd & 7" forever.
func (r *GameLiveStateRepository) Upsert(state *models.GameLiveState) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"period", "clock", "situation", "possession", "last_play", "tv",
			"weather_description", "temperature", "wind_speed", "wind_direction",
			"home_win_probability", "updated_at",
		}),
	}).Create(state).Error
}
