package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TeamRepository provides methods for interacting with teams in the database.
type TeamRepository struct {
	db *gorm.DB
}

// NewTeamRepository creates a new TeamRepository.
func NewTeamRepository(db *gorm.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

// Create inserts a new team into the database.
func (r *TeamRepository) Create(team *models.Team) error {
	return r.db.Create(team).Error
}

// FindByID retrieves a team by its ID with its home venue.
func (r *TeamRepository) FindByID(id uuid.UUID) (*models.Team, error) {
	var team models.Team
	if err := r.db.Preload("HomeVenue").First(&team, id).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

// FindByAbbreviation retrieves a team by its abbreviation.
func (r *TeamRepository) FindByAbbreviation(abbr string) (*models.Team, error) {
	var team models.Team
	if err := r.db.Preload("HomeVenue").Where("abbreviation = ?", abbr).First(&team).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

// FindAll retrieves all teams ordered by name.
func (r *TeamRepository) FindAll() ([]models.Team, error) {
	var teams []models.Team
	if err := r.db.Preload("HomeVenue").Order("name").Find(&teams).Error; err != nil {
		return nil, err
	}
	return teams, nil
}

// FindByConference retrieves all teams in a conference.
func (r *TeamRepository) FindByConference(conference string) ([]models.Team, error) {
	var teams []models.Team
	if err := r.db.Preload("HomeVenue").Where("conference = ?", conference).Order("name").Find(&teams).Error; err != nil {
		return nil, err
	}
	return teams, nil
}

// Update saves changes to an existing team.
func (r *TeamRepository) Update(team *models.Team) error {
	return r.db.Save(team).Error
}

// Delete removes a team from the database.
func (r *TeamRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.Team{}, id).Error
}

// FindByExternalID retrieves a team by its external API ID and sport.
func (r *TeamRepository) FindByExternalID(externalID int64, sport string) (*models.Team, error) {
	var team models.Team
	if err := r.db.Preload("HomeVenue").Where("external_id = ? AND sport = ?", externalID, sport).First(&team).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

// FindByName retrieves a team by its name.
func (r *TeamRepository) FindByName(name string) (*models.Team, error) {
	var team models.Team
	if err := r.db.Preload("HomeVenue").Where("name = ?", name).First(&team).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

// FindByNameAndSport retrieves a team by its name and sport.
func (r *TeamRepository) FindByNameAndSport(name, sport string) (*models.Team, error) {
	var team models.Team
	if err := r.db.Where("name = ? AND sport = ?", name, sport).First(&team).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

// SearchByName searches teams by name for a given sport using case-insensitive matching.
func (r *TeamRepository) SearchByName(sport, query string) ([]models.Team, error) {
	var teams []models.Team
	if err := r.db.Preload("HomeVenue").
		Where("sport = ? AND name ILIKE ?", sport, "%"+query+"%").
		Order("name").
		Find(&teams).Error; err != nil {
		return nil, err
	}
	return teams, nil
}

// Upsert creates or updates a team based on (external_id, sport).
func (r *TeamRepository) Upsert(team *models.Team) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "external_id"}, {Name: "sport"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "abbreviation", "mascot", "conference", "classification", "home_venue_id", "logo_url", "primary_color", "secondary_color", "updated_at"}),
	}).Create(team).Error
}
