package repository

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GenerateInviteCode creates a random 8-character hex code.
func GenerateInviteCode() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// LeagueRepository provides methods for interacting with leagues in the database.
type LeagueRepository struct {
	db *gorm.DB
}

// NewLeagueRepository creates a new LeagueRepository.
func NewLeagueRepository(db *gorm.DB) *LeagueRepository {
	return &LeagueRepository{db: db}
}

// Create inserts a new league into the database.
func (r *LeagueRepository) Create(league *models.League) error {
	return r.db.Create(league).Error
}

// FindAll retrieves all leagues with their creator info.
func (r *LeagueRepository) FindAll() ([]models.League, error) {
	var leagues []models.League
	if err := r.db.Preload("Creator").Preload("Members.User").Order("name").Find(&leagues).Error; err != nil {
		return nil, err
	}
	return leagues, nil
}

// FindByID retrieves a league by its ID with members.
func (r *LeagueRepository) FindByID(id uuid.UUID) (*models.League, error) {
	var league models.League
	if err := r.db.Preload("Creator").Preload("Members.User").First(&league, id).Error; err != nil {
		return nil, err
	}
	return &league, nil
}

// FindUserLeagues retrieves all leagues a user is a member of.
func (r *LeagueRepository) FindUserLeagues(userID uuid.UUID) ([]models.League, error) {
	var leagues []models.League
	err := r.db.
		Joins("JOIN league_members ON league_members.league_id = leagues.id").
		Where("league_members.user_id = ?", userID).
		Preload("Creator").
		Preload("Members.User").
		Order("leagues.name").
		Find(&leagues).Error
	if err != nil {
		return nil, err
	}
	return leagues, nil
}

// FindPublicLeagues retrieves all public leagues.
func (r *LeagueRepository) FindPublicLeagues() ([]models.League, error) {
	var leagues []models.League
	err := r.db.
		Where("is_public = ?", true).
		Preload("Creator").
		Preload("Members.User").
		Order("name").
		Find(&leagues).Error
	if err != nil {
		return nil, err
	}
	return leagues, nil
}

// FindByInviteCode retrieves a league by its invite code.
func (r *LeagueRepository) FindByInviteCode(code string) (*models.League, error) {
	var league models.League
	err := r.db.
		Where("invite_code = ?", code).
		Preload("Creator").
		Preload("Members.User").
		First(&league).Error
	if err != nil {
		return nil, err
	}
	return &league, nil
}

// IsMember checks if a user is a member of a league.
func (r *LeagueRepository) IsMember(leagueID, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&models.LeagueMember{}).
		Where("league_id = ? AND user_id = ?", leagueID, userID).
		Count(&count).Error
	return count > 0, err
}

// GetMembership retrieves a user's membership in a league.
func (r *LeagueRepository) GetMembership(leagueID, userID uuid.UUID) (*models.LeagueMember, error) {
	var member models.LeagueMember
	err := r.db.Where("league_id = ? AND user_id = ?", leagueID, userID).First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// AddMember adds a user to a league.
func (r *LeagueRepository) AddMember(leagueID, userID uuid.UUID, role string) error {
	member := &models.LeagueMember{
		LeagueID: leagueID,
		UserID:   userID,
		Role:     role,
	}
	return r.db.Create(member).Error
}

// RemoveMember removes a user from a league.
func (r *LeagueRepository) RemoveMember(leagueID, userID uuid.UUID) error {
	return r.db.Where("league_id = ? AND user_id = ?", leagueID, userID).Delete(&models.LeagueMember{}).Error
}

// Delete removes a league from the database.
func (r *LeagueRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.League{}, id).Error
}
