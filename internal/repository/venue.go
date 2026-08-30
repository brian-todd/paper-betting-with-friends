package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VenueRepository provides methods for interacting with venues in the database.
type VenueRepository struct {
	db *gorm.DB
}

// NewVenueRepository creates a new VenueRepository.
func NewVenueRepository(db *gorm.DB) *VenueRepository {
	return &VenueRepository{db: db}
}

// Create inserts a new venue into the database.
func (r *VenueRepository) Create(venue *models.Venue) error {
	return r.db.Create(venue).Error
}

// FindByID retrieves a venue by its ID.
func (r *VenueRepository) FindByID(id uuid.UUID) (*models.Venue, error) {
	var venue models.Venue
	if err := r.db.First(&venue, id).Error; err != nil {
		return nil, err
	}
	return &venue, nil
}

// FindAll retrieves all venues ordered by name.
func (r *VenueRepository) FindAll() ([]models.Venue, error) {
	var venues []models.Venue
	if err := r.db.Order("name").Find(&venues).Error; err != nil {
		return nil, err
	}
	return venues, nil
}

// Update saves changes to an existing venue.
func (r *VenueRepository) Update(venue *models.Venue) error {
	return r.db.Save(venue).Error
}

// Delete removes a venue from the database.
func (r *VenueRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.Venue{}, id).Error
}

// FindByExternalID retrieves a venue by its external API ID and sport.
func (r *VenueRepository) FindByExternalID(externalID int64, sport string) (*models.Venue, error) {
	var venue models.Venue
	if err := r.db.Where("external_id = ? AND sport = ?", externalID, sport).First(&venue).Error; err != nil {
		return nil, err
	}
	return &venue, nil
}

// Upsert creates or updates a venue based on (external_id, sport).
func (r *VenueRepository) Upsert(venue *models.Venue) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "external_id"}, {Name: "sport"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "city", "state", "capacity", "timezone", "dome", "grass", "updated_at"}),
	}).Create(venue).Error
}
