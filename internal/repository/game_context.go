package repository

import (
	"errors"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GamePregameWinProbabilityRepository provides access to pre-game win
// probabilities.
type GamePregameWinProbabilityRepository struct {
	db *gorm.DB
}

// NewGamePregameWinProbabilityRepository creates a new
// GamePregameWinProbabilityRepository.
func NewGamePregameWinProbabilityRepository(db *gorm.DB) *GamePregameWinProbabilityRepository {
	return &GamePregameWinProbabilityRepository{db: db}
}

// Upsert writes one game's pre-game win probability, replacing any previous
// value.
//
// Plain assignment rather than keeping the first row: the metric moves with the
// line it is derived from, and a probability computed against a spread that has
// since moved three points is not a historical record of anything -- it is a
// wrong answer to the only question this page asks, which is what the market
// thinks now.
func (r *GamePregameWinProbabilityRepository) Upsert(wp *models.GamePregameWinProbability) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"fetched_at", "home_win_probability", "spread", "updated_at",
		}),
	}).Create(wp).Error
}

// FindByGameID returns one game's pre-game win probability, or nil when the
// provider has published none.
//
// Nil rather than an error, because for most games there genuinely is none: the
// metric comes off a posted line, so its coverage tracks the market and not the
// schedule.
func (r *GamePregameWinProbabilityRepository) FindByGameID(gameID uuid.UUID) (*models.GamePregameWinProbability, error) {
	var wp models.GamePregameWinProbability
	err := r.db.Where("game_id = ?", gameID).First(&wp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wp, nil
}

// GameForecastRepository provides access to kickoff weather forecasts.
type GameForecastRepository struct {
	db *gorm.DB
}

// NewGameForecastRepository creates a new GameForecastRepository.
func NewGameForecastRepository(db *gorm.DB) *GameForecastRepository {
	return &GameForecastRepository{db: db}
}

// Upsert writes one game's forecast, replacing any previous value. A forecast
// is only ever the current one; yesterday's is of no interest to anybody.
func (r *GameForecastRepository) Upsert(forecast *models.GameForecast) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"fetched_at", "indoors",
			"temperature", "humidity", "precipitation", "snowfall", "wind_speed",
			"weather_condition", "weather_condition_code",
			"updated_at",
		}),
	}).Create(forecast).Error
}

// FindByGameID returns one game's forecast, or nil when there is none.
func (r *GameForecastRepository) FindByGameID(gameID uuid.UUID) (*models.GameForecast, error) {
	var forecast models.GameForecast
	err := r.db.Where("game_id = ?", gameID).First(&forecast).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &forecast, nil
}
