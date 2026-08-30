package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MoneyLineOddsRepository provides methods for interacting with money line odds.
type MoneyLineOddsRepository struct {
	db *gorm.DB
}

// NewMoneyLineOddsRepository creates a new MoneyLineOddsRepository.
func NewMoneyLineOddsRepository(db *gorm.DB) *MoneyLineOddsRepository {
	return &MoneyLineOddsRepository{db: db}
}

// Create inserts new money line odds into the database.
func (r *MoneyLineOddsRepository) Create(odds *models.MoneyLineOdds) error {
	return r.db.Create(odds).Error
}

// FindByID retrieves money line odds by ID.
func (r *MoneyLineOddsRepository) FindByID(id uuid.UUID) (*models.MoneyLineOdds, error) {
	var odds models.MoneyLineOdds
	if err := r.db.Preload("Game").First(&odds, id).Error; err != nil {
		return nil, err
	}
	return &odds, nil
}

// FindByGame retrieves all money line odds for a game.
func (r *MoneyLineOddsRepository) FindByGame(gameID uuid.UUID) ([]models.MoneyLineOdds, error) {
	var odds []models.MoneyLineOdds
	if err := r.db.Where("game_id = ?", gameID).Order("created_at DESC").Find(&odds).Error; err != nil {
		return nil, err
	}
	return odds, nil
}

// FindByGames retrieves money line odds for many games in one query, keyed by game.
//
// The games grid needs a line for every card it renders. Asking per game turned
// one page into hundreds of round trips, so callers with a list of games should
// reach for this instead of looping over FindByGame.
func (r *MoneyLineOddsRepository) FindByGames(gameIDs []uuid.UUID) (map[uuid.UUID][]models.MoneyLineOdds, error) {
	byGame := make(map[uuid.UUID][]models.MoneyLineOdds, len(gameIDs))
	if len(gameIDs) == 0 {
		return byGame, nil
	}

	var odds []models.MoneyLineOdds
	if err := r.db.Where("game_id IN ?", gameIDs).Order("created_at DESC").Find(&odds).Error; err != nil {
		return nil, err
	}
	// Ordering is preserved per game so the first entry stays the same row
	// FindByGame would have put first.
	for _, row := range odds {
		byGame[row.GameID] = append(byGame[row.GameID], row)
	}
	return byGame, nil
}

// FindByGameAndSource retrieves money line odds for a game from a specific source.
func (r *MoneyLineOddsRepository) FindByGameAndSource(gameID uuid.UUID, source models.OddsSource) (*models.MoneyLineOdds, error) {
	var odds models.MoneyLineOdds
	if err := r.db.Where("game_id = ? AND source = ?", gameID, source).Order("created_at DESC").First(&odds).Error; err != nil {
		return nil, err
	}
	return &odds, nil
}

// Update saves changes to existing money line odds.
func (r *MoneyLineOddsRepository) Update(odds *models.MoneyLineOdds) error {
	return r.db.Save(odds).Error
}

// Delete removes money line odds from the database.
func (r *MoneyLineOddsRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.MoneyLineOdds{}, id).Error
}

// Upsert creates or updates money line odds based on game_id and source.
func (r *MoneyLineOddsRepository) Upsert(odds *models.MoneyLineOdds) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "game_id"}, {Name: "source"}},
		DoUpdates: clause.AssignmentColumns([]string{"home_odds", "away_odds", "updated_at"}),
	}).Create(odds).Error
}

// SpreadOddsRepository provides methods for interacting with spread odds.
type SpreadOddsRepository struct {
	db *gorm.DB
}

// NewSpreadOddsRepository creates a new SpreadOddsRepository.
func NewSpreadOddsRepository(db *gorm.DB) *SpreadOddsRepository {
	return &SpreadOddsRepository{db: db}
}

// Create inserts new spread odds into the database.
func (r *SpreadOddsRepository) Create(odds *models.SpreadOdds) error {
	return r.db.Create(odds).Error
}

// FindByID retrieves spread odds by ID.
func (r *SpreadOddsRepository) FindByID(id uuid.UUID) (*models.SpreadOdds, error) {
	var odds models.SpreadOdds
	if err := r.db.Preload("Game").First(&odds, id).Error; err != nil {
		return nil, err
	}
	return &odds, nil
}

// FindByGame retrieves all spread odds for a game.
func (r *SpreadOddsRepository) FindByGame(gameID uuid.UUID) ([]models.SpreadOdds, error) {
	var odds []models.SpreadOdds
	if err := r.db.Where("game_id = ?", gameID).Order("created_at DESC").Find(&odds).Error; err != nil {
		return nil, err
	}
	return odds, nil
}

// FindByGames retrieves spread odds for many games in one query, keyed by game.
//
// The games grid needs a line for every card it renders. Asking per game turned
// one page into hundreds of round trips, so callers with a list of games should
// reach for this instead of looping over FindByGame.
func (r *SpreadOddsRepository) FindByGames(gameIDs []uuid.UUID) (map[uuid.UUID][]models.SpreadOdds, error) {
	byGame := make(map[uuid.UUID][]models.SpreadOdds, len(gameIDs))
	if len(gameIDs) == 0 {
		return byGame, nil
	}

	var odds []models.SpreadOdds
	if err := r.db.Where("game_id IN ?", gameIDs).Order("created_at DESC").Find(&odds).Error; err != nil {
		return nil, err
	}
	// Ordering is preserved per game so the first entry stays the same row
	// FindByGame would have put first.
	for _, row := range odds {
		byGame[row.GameID] = append(byGame[row.GameID], row)
	}
	return byGame, nil
}

// FindByGameAndSource retrieves spread odds for a game from a specific source.
func (r *SpreadOddsRepository) FindByGameAndSource(gameID uuid.UUID, source models.OddsSource) (*models.SpreadOdds, error) {
	var odds models.SpreadOdds
	if err := r.db.Where("game_id = ? AND source = ?", gameID, source).Order("created_at DESC").First(&odds).Error; err != nil {
		return nil, err
	}
	return &odds, nil
}

// Update saves changes to existing spread odds.
func (r *SpreadOddsRepository) Update(odds *models.SpreadOdds) error {
	return r.db.Save(odds).Error
}

// Delete removes spread odds from the database.
func (r *SpreadOddsRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.SpreadOdds{}, id).Error
}

// Upsert creates or updates spread odds based on game_id and source.
func (r *SpreadOddsRepository) Upsert(odds *models.SpreadOdds) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "game_id"}, {Name: "source"}},
		DoUpdates: clause.AssignmentColumns([]string{"home_spread", "away_spread", "home_odds", "away_odds", "updated_at"}),
	}).Create(odds).Error
}

// OverUnderOddsRepository provides methods for interacting with over/under odds.
type OverUnderOddsRepository struct {
	db *gorm.DB
}

// NewOverUnderOddsRepository creates a new OverUnderOddsRepository.
func NewOverUnderOddsRepository(db *gorm.DB) *OverUnderOddsRepository {
	return &OverUnderOddsRepository{db: db}
}

// Create inserts new over/under odds into the database.
func (r *OverUnderOddsRepository) Create(odds *models.OverUnderOdds) error {
	return r.db.Create(odds).Error
}

// FindByID retrieves over/under odds by ID.
func (r *OverUnderOddsRepository) FindByID(id uuid.UUID) (*models.OverUnderOdds, error) {
	var odds models.OverUnderOdds
	if err := r.db.Preload("Game").First(&odds, id).Error; err != nil {
		return nil, err
	}
	return &odds, nil
}

// FindByGame retrieves all over/under odds for a game.
func (r *OverUnderOddsRepository) FindByGame(gameID uuid.UUID) ([]models.OverUnderOdds, error) {
	var odds []models.OverUnderOdds
	if err := r.db.Where("game_id = ?", gameID).Order("created_at DESC").Find(&odds).Error; err != nil {
		return nil, err
	}
	return odds, nil
}

// FindByGames retrieves over/under odds for many games in one query, keyed by game.
//
// The games grid needs a line for every card it renders. Asking per game turned
// one page into hundreds of round trips, so callers with a list of games should
// reach for this instead of looping over FindByGame.
func (r *OverUnderOddsRepository) FindByGames(gameIDs []uuid.UUID) (map[uuid.UUID][]models.OverUnderOdds, error) {
	byGame := make(map[uuid.UUID][]models.OverUnderOdds, len(gameIDs))
	if len(gameIDs) == 0 {
		return byGame, nil
	}

	var odds []models.OverUnderOdds
	if err := r.db.Where("game_id IN ?", gameIDs).Order("created_at DESC").Find(&odds).Error; err != nil {
		return nil, err
	}
	// Ordering is preserved per game so the first entry stays the same row
	// FindByGame would have put first.
	for _, row := range odds {
		byGame[row.GameID] = append(byGame[row.GameID], row)
	}
	return byGame, nil
}

// FindByGameAndSource retrieves over/under odds for a game from a specific source.
func (r *OverUnderOddsRepository) FindByGameAndSource(gameID uuid.UUID, source models.OddsSource) (*models.OverUnderOdds, error) {
	var odds models.OverUnderOdds
	if err := r.db.Where("game_id = ? AND source = ?", gameID, source).Order("created_at DESC").First(&odds).Error; err != nil {
		return nil, err
	}
	return &odds, nil
}

// Update saves changes to existing over/under odds.
func (r *OverUnderOddsRepository) Update(odds *models.OverUnderOdds) error {
	return r.db.Save(odds).Error
}

// Delete removes over/under odds from the database.
func (r *OverUnderOddsRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.OverUnderOdds{}, id).Error
}

// Upsert creates or updates over/under odds based on game_id and source.
func (r *OverUnderOddsRepository) Upsert(odds *models.OverUnderOdds) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "game_id"}, {Name: "source"}},
		DoUpdates: clause.AssignmentColumns([]string{"total", "over_odds", "under_odds", "updated_at"}),
	}).Create(odds).Error
}
