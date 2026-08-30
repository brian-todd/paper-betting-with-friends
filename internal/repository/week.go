package repository

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WeekRepository provides methods for interacting with weeks in the database.
type WeekRepository struct {
	db *gorm.DB
}

// NewWeekRepository creates a new WeekRepository.
func NewWeekRepository(db *gorm.DB) *WeekRepository {
	return &WeekRepository{db: db}
}

// Create inserts a new week into the database.
func (r *WeekRepository) Create(week *models.Week) error {
	return r.db.Create(week).Error
}

// FindByID retrieves a week by its ID.
func (r *WeekRepository) FindByID(id uuid.UUID) (*models.Week, error) {
	var week models.Week
	if err := r.db.First(&week, id).Error; err != nil {
		return nil, err
	}
	return &week, nil
}

// FindBySeasonAndNumber retrieves a week by its season and week number.
// Deprecated: Use FindBySeasonNumberAndType instead for correct postseason handling.
func (r *WeekRepository) FindBySeasonAndNumber(season, number int) (*models.Week, error) {
	var week models.Week
	if err := r.db.Where("season = ? AND number = ?", season, number).First(&week).Error; err != nil {
		return nil, err
	}
	return &week, nil
}

// FindBySeasonNumberAndType retrieves a week by season, week number, and season type.
func (r *WeekRepository) FindBySeasonNumberAndType(season, number int, seasonType models.SeasonType) (*models.Week, error) {
	var week models.Week
	if err := r.db.Where("season = ? AND number = ? AND season_type = ?", season, number, seasonType).First(&week).Error; err != nil {
		return nil, err
	}
	return &week, nil
}

// FindBySeason retrieves all weeks for a given season.
func (r *WeekRepository) FindBySeason(season int) ([]models.Week, error) {
	var weeks []models.Week
	if err := r.db.Where("season = ?", season).Order("number").Find(&weeks).Error; err != nil {
		return nil, err
	}
	return weeks, nil
}

// FindAll retrieves all weeks ordered by season and number.
func (r *WeekRepository) FindAll() ([]models.Week, error) {
	var weeks []models.Week
	if err := r.db.Order("season DESC, number").Find(&weeks).Error; err != nil {
		return nil, err
	}
	return weeks, nil
}

// Update saves changes to an existing week.
func (r *WeekRepository) Update(week *models.Week) error {
	return r.db.Save(week).Error
}

// Delete removes a week from the database.
func (r *WeekRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.Week{}, id).Error
}

// Upsert creates or updates a week based on season, number, and season_type.
func (r *WeekRepository) Upsert(week *models.Week) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "season"}, {Name: "number"}, {Name: "season_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"start_date", "end_date", "updated_at"}),
	}).Create(week).Error
}

// FindSeasonContainingDate returns the season year that contains the given date.
// Returns 0 if no season contains the date.
//
// Rows whose span is too long to be a real week are excluded -- see
// models.Week.Plausible. Without that filter a single bad row spanning a year
// matches every date, so the caller resolves the wrong season for months and
// the sync fetches a year that holds none of the games being watched.
func (r *WeekRepository) FindSeasonContainingDate(t time.Time) (int, error) {
	var season int
	err := r.db.Model(&models.Week{}).
		Select("season").
		Where("start_date <= ? AND end_date >= ?", t, t).
		Where("end_date > start_date").
		Where("end_date <= start_date + make_interval(secs => ?)", models.MaxWeekSpan.Seconds()).
		Order("season DESC").
		Limit(1).
		Pluck("season", &season).Error
	if err != nil {
		return 0, err
	}
	return season, nil
}
