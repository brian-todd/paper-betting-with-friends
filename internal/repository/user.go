package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserRepository provides methods for interacting with users in the database.
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user into the database.
func (r *UserRepository) Create(user *models.User) error {
	return r.db.Create(user).Error
}

// FindByUsername retrieves a user by their username.
func (r *UserRepository) FindByUsername(username string) (*models.User, error) {
	var user models.User
	if err := r.db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByID retrieves a user by their ID.
func (r *UserRepository) FindByID(id uuid.UUID) (*models.User, error) {
	var user models.User
	if err := r.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// Exists checks if a user with the given username already exists.
func (r *UserRepository) Exists(username string) (bool, error) {
	var count int64
	err := r.db.Model(&models.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}

// FindAll retrieves all users from the database.
func (r *UserRepository) FindAll() ([]models.User, error) {
	var users []models.User
	if err := r.db.Order("username").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// Update saves changes to an existing user.
func (r *UserRepository) Update(user *models.User) error {
	return r.db.Save(user).Error
}

// SetAdminByUsername sets the is_admin flag for a user by username.
func (r *UserRepository) SetAdminByUsername(username string, isAdmin bool) error {
	return r.db.Model(&models.User{}).Where("username = ?", username).Update("is_admin", isAdmin).Error
}

// BumpSessionVersion invalidates every session already issued for a user.
//
// It increments in SQL rather than reading, adding one and saving, so two
// concurrent resets cannot both write the same value and leave one of the two
// compromised sessions alive.
func (r *UserRepository) BumpSessionVersion(id uuid.UUID) error {
	return r.db.Model(&models.User{}).
		Where("id = ?", id).
		Update("session_version", gorm.Expr("session_version + 1")).Error
}

// Delete removes a user. Bets, purses and league memberships all reference
// users with ON DELETE CASCADE, so they go with it.
func (r *UserRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.User{}, id).Error
}
