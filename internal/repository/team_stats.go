package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TeamRatingRepository provides access to model ratings of teams.
type TeamRatingRepository struct {
	db *gorm.DB
}

// NewTeamRatingRepository creates a new TeamRatingRepository.
func NewTeamRatingRepository(db *gorm.DB) *TeamRatingRepository {
	return &TeamRatingRepository{db: db}
}

// Upsert writes one team's rating from one source, replacing any previous value
// for that (team, season, source).
//
// Every column is assigned rather than COALESCEd. One feed writes each source,
// and a field the feed stopped reporting has genuinely stopped being known --
// carrying the old number forward would leave a stale rating on the page with
// nothing to say it was stale.
func (r *TeamRatingRepository) Upsert(rating *models.TeamRating) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "team_id"}, {Name: "season"}, {Name: "source"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"fetched_at",
			"overall", "offense", "defense", "special_teams",
			"overall_rank", "offense_rank", "defense_rank",
			"strength_of_schedule", "strength_of_record",
			"through_week", "model_version",
			"updated_at",
		}),
	}).Create(rating).Error
}

// FindForTeams returns every stored rating for the given teams in one season,
// across all sources.
//
// Both teams are fetched in one query: the detail page always wants both sides
// at once, and at three sources each this is six rows.
func (r *TeamRatingRepository) FindForTeams(season int, teamIDs ...uuid.UUID) ([]models.TeamRating, error) {
	if len(teamIDs) == 0 {
		return nil, nil
	}

	var ratings []models.TeamRating
	err := r.db.
		Where("season = ? AND team_id IN ?", season, teamIDs).
		Order("source").
		Find(&ratings).Error
	if err != nil {
		return nil, err
	}
	return ratings, nil
}

// TeamRecordRepository provides access to season win-loss records.
type TeamRecordRepository struct {
	db *gorm.DB
}

// NewTeamRecordRepository creates a new TeamRecordRepository.
func NewTeamRecordRepository(db *gorm.DB) *TeamRecordRepository {
	return &TeamRecordRepository{db: db}
}

// Upsert writes one team's season record, replacing any previous value.
func (r *TeamRecordRepository) Upsert(record *models.TeamRecord) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "team_id"}, {Name: "season"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"fetched_at",
			"games", "wins", "losses", "ties",
			"home_games", "home_wins", "home_losses", "home_ties",
			"away_games", "away_wins", "away_losses", "away_ties",
			"expected_wins",
			"updated_at",
		}),
	}).Create(record).Error
}

// FindForTeams returns the season records for the given teams, keyed by team ID.
//
// A team with no row is absent from the map rather than present as a zero
// record: 0-0 is a claim about a team that has not played, and "we have not
// synced this team" is a different thing the page renders differently.
func (r *TeamRecordRepository) FindForTeams(season int, teamIDs ...uuid.UUID) (map[uuid.UUID]models.TeamRecord, error) {
	if len(teamIDs) == 0 {
		return nil, nil
	}

	var records []models.TeamRecord
	if err := r.db.Where("season = ? AND team_id IN ?", season, teamIDs).Find(&records).Error; err != nil {
		return nil, err
	}

	byTeam := make(map[uuid.UUID]models.TeamRecord, len(records))
	for _, record := range records {
		byTeam[record.TeamID] = record
	}
	return byTeam, nil
}

// TeamATSRecordRepository provides access to against-the-spread records.
type TeamATSRecordRepository struct {
	db *gorm.DB
}

// NewTeamATSRecordRepository creates a new TeamATSRecordRepository.
func NewTeamATSRecordRepository(db *gorm.DB) *TeamATSRecordRepository {
	return &TeamATSRecordRepository{db: db}
}

// Upsert writes one team's ATS record, replacing any previous value.
func (r *TeamATSRecordRepository) Upsert(record *models.TeamATSRecord) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "team_id"}, {Name: "season"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"fetched_at",
			"games", "ats_wins", "ats_losses", "ats_pushes",
			"avg_cover_margin",
			"updated_at",
		}),
	}).Create(record).Error
}

// FindForTeams returns the ATS records for the given teams, keyed by team ID.
//
// As with TeamRecordRepository.FindForTeams, a team with no row is absent
// rather than present as a zero record.
func (r *TeamATSRecordRepository) FindForTeams(season int, teamIDs ...uuid.UUID) (map[uuid.UUID]models.TeamATSRecord, error) {
	if len(teamIDs) == 0 {
		return nil, nil
	}

	var records []models.TeamATSRecord
	if err := r.db.Where("season = ? AND team_id IN ?", season, teamIDs).Find(&records).Error; err != nil {
		return nil, err
	}

	byTeam := make(map[uuid.UUID]models.TeamATSRecord, len(records))
	for _, record := range records {
		byTeam[record.TeamID] = record
	}
	return byTeam, nil
}

// TeamAdvancedStatsRepository provides access to season efficiency stats.
type TeamAdvancedStatsRepository struct {
	db *gorm.DB
}

// NewTeamAdvancedStatsRepository creates a new TeamAdvancedStatsRepository.
func NewTeamAdvancedStatsRepository(db *gorm.DB) *TeamAdvancedStatsRepository {
	return &TeamAdvancedStatsRepository{db: db}
}

// Upsert writes one team's season efficiency, replacing any previous value.
func (r *TeamAdvancedStatsRepository) Upsert(stats *models.TeamAdvancedStats) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "team_id"}, {Name: "season"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"fetched_at",
			"offense_plays", "offense_drives", "defense_plays", "defense_drives",
			"offense_ppa", "offense_success_rate", "offense_explosiveness",
			"offense_line_yards", "offense_points_per_opportunity", "offense_havoc",
			"defense_ppa", "defense_success_rate", "defense_explosiveness",
			"defense_line_yards", "defense_points_per_opportunity", "defense_havoc",
			"updated_at",
		}),
	}).Create(stats).Error
}

// FindForTeams returns season efficiency for the given teams, keyed by team ID.
func (r *TeamAdvancedStatsRepository) FindForTeams(season int, teamIDs ...uuid.UUID) (map[uuid.UUID]models.TeamAdvancedStats, error) {
	if len(teamIDs) == 0 {
		return nil, nil
	}

	var stats []models.TeamAdvancedStats
	if err := r.db.Where("season = ? AND team_id IN ?", season, teamIDs).Find(&stats).Error; err != nil {
		return nil, err
	}

	byTeam := make(map[uuid.UUID]models.TeamAdvancedStats, len(stats))
	for _, row := range stats {
		byTeam[row.TeamID] = row
	}
	return byTeam, nil
}
