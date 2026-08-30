package repository

import (
	"errors"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var ErrInsufficientBalance = errors.New("insufficient balance")

// PurseRepository provides methods for interacting with purses in the database.
type PurseRepository struct {
	db *gorm.DB
}

// NewPurseRepository creates a new PurseRepository.
func NewPurseRepository(db *gorm.DB) *PurseRepository {
	return &PurseRepository{db: db}
}

// Create inserts a new purse into the database.
func (r *PurseRepository) Create(purse *models.Purse) error {
	return r.db.Create(purse).Error
}

// FindByUserAndLeague retrieves a purse by user and league IDs.
func (r *PurseRepository) FindByUserAndLeague(userID, leagueID uuid.UUID) (*models.Purse, error) {
	var purse models.Purse
	err := r.db.Where("user_id = ? AND league_id = ?", userID, leagueID).First(&purse).Error
	if err != nil {
		return nil, err
	}
	return &purse, nil
}

// Update saves changes to an existing purse.
func (r *PurseRepository) Update(purse *models.Purse) error {
	return r.db.Save(purse).Error
}

// DeductStake atomically deducts stake from a purse balance.
// Returns ErrInsufficientBalance if balance is less than amount.
func (r *PurseRepository) DeductStake(userID, leagueID uuid.UUID, amount decimal.Decimal) error {
	result := r.db.Model(&models.Purse{}).
		Where("user_id = ? AND league_id = ? AND balance >= ?", userID, leagueID, amount).
		Update("balance", gorm.Expr("balance - ?", amount))

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

// CreditWinnings adds amount to a purse balance.
func (r *PurseRepository) CreditWinnings(userID, leagueID uuid.UUID, amount decimal.Decimal) error {
	return r.db.Model(&models.Purse{}).
		Where("user_id = ? AND league_id = ?", userID, leagueID).
		Update("balance", gorm.Expr("balance + ?", amount)).Error
}

// FindByUser retrieves all purses for a user.
func (r *PurseRepository) FindByUser(userID uuid.UUID) ([]models.Purse, error) {
	var purses []models.Purse
	err := r.db.Where("user_id = ?", userID).Preload("League").Find(&purses).Error
	if err != nil {
		return nil, err
	}
	return purses, nil
}

// FindByLeague retrieves all purses for a league, ordered by balance descending.
func (r *PurseRepository) FindByLeague(leagueID uuid.UUID) ([]models.Purse, error) {
	var purses []models.Purse
	err := r.db.Where("league_id = ?", leagueID).
		Preload("User").
		Order("balance DESC").
		Find(&purses).Error
	if err != nil {
		return nil, err
	}
	return purses, nil
}
