package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameResultRepository provides methods for interacting with game results.
type GameResultRepository struct {
	db *gorm.DB
}

// NewGameResultRepository creates a new GameResultRepository.
func NewGameResultRepository(db *gorm.DB) *GameResultRepository {
	return &GameResultRepository{db: db}
}

// Create inserts a new game result into the database.
func (r *GameResultRepository) Create(result *models.GameResult) error {
	return r.db.Create(result).Error
}

// FindByID retrieves a game result by its ID.
func (r *GameResultRepository) FindByID(id uuid.UUID) (*models.GameResult, error) {
	var result models.GameResult
	if err := r.db.Preload("Game").First(&result, id).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

// FindByGameID retrieves a game result by its game ID.
func (r *GameResultRepository) FindByGameID(gameID uuid.UUID) (*models.GameResult, error) {
	var result models.GameResult
	if err := r.db.Where("game_id = ?", gameID).First(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

// Upsert creates or updates a game result based on game_id.
func (r *GameResultRepository) Upsert(result *models.GameResult) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"home_score", "away_score", "home_line_scores", "away_line_scores", "excitement_index", "finalized_at", "updated_at"}),
	}).Create(result).Error
}

// Update saves changes to an existing game result.
func (r *GameResultRepository) Update(result *models.GameResult) error {
	return r.db.Save(result).Error
}

// Delete removes a game result from the database.
func (r *GameResultRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.GameResult{}, id).Error
}
