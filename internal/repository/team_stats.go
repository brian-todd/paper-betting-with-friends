package repository

import (
	"slices"

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

// HasSeason reports whether any team holds a rating from this source for a season.
func (r *TeamRatingRepository) HasSeason(season int, source models.RatingSource) (bool, error) {
	var found bool
	err := r.db.Raw(`SELECT EXISTS (SELECT 1 FROM team_ratings WHERE season = ? AND source = ?)`, season, source).
		Scan(&found).Error
	return found, err
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

// TeamSplit is one team's home and away results in a season.
type TeamSplit struct {
	HomeWins, HomeLosses, AwayWins, AwayLosses int
}

// BasketballSplits works out each team's home and away results from the finished
// games stored for a season, since basketball's feed reports only the overall record.
// A neutral-site game is neither, and a level score cannot be final in basketball.
func (r *TeamRecordRepository) BasketballSplits(season int) (map[uuid.UUID]TeamSplit, error) {
	var rows []struct {
		TeamID     uuid.UUID
		HomeWins   int
		HomeLosses int
		AwayWins   int
		AwayLosses int
	}
	err := r.db.Raw(`
		WITH finished AS (
			SELECT g.home_team_id, g.away_team_id, gr.home_score, gr.away_score
			FROM games g
			JOIN game_results gr ON gr.game_id = g.id
			WHERE g.sport = ? AND g.season = ? AND NOT g.neutral_site AND gr.finalized_at IS NOT NULL
		), sides AS (
			SELECT home_team_id AS team_id,
				(home_score > away_score)::int AS home_wins, (home_score < away_score)::int AS home_losses,
				0 AS away_wins, 0 AS away_losses
			FROM finished
			UNION ALL
			SELECT away_team_id,
				0, 0,
				(away_score > home_score)::int, (away_score < home_score)::int
			FROM finished
		)
		SELECT team_id, SUM(home_wins) AS home_wins, SUM(home_losses) AS home_losses,
			SUM(away_wins) AS away_wins, SUM(away_losses) AS away_losses
		FROM sides GROUP BY team_id`, models.SportBasketball, season).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	splits := make(map[uuid.UUID]TeamSplit, len(rows))
	for _, row := range rows {
		splits[row.TeamID] = TeamSplit{
			HomeWins: row.HomeWins, HomeLosses: row.HomeLosses,
			AwayWins: row.AwayWins, AwayLosses: row.AwayLosses,
		}
	}
	return splits, nil
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

// TeamSeasonStatsRepository provides access to basketball season figures.
type TeamSeasonStatsRepository struct {
	db *gorm.DB
}

// NewTeamSeasonStatsRepository creates a new TeamSeasonStatsRepository.
func NewTeamSeasonStatsRepository(db *gorm.DB) *TeamSeasonStatsRepository {
	return &TeamSeasonStatsRepository{db: db}
}

// Upsert writes one team's season figures, replacing any previous value. Every column
// is assigned, as in TeamRatingRepository.Upsert.
func (r *TeamSeasonStatsRepository) Upsert(stats *models.TeamSeasonStats) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "team_id"}, {Name: "season"}},
		DoUpdates: clause.AssignmentColumns(slices.Concat([]string{
			"fetched_at", "games", "pace",
			"offense_rating", "defense_rating",
			"offense_effective_fg_pct", "defense_effective_fg_pct",
			"offense_turnover_pct", "defense_turnover_pct",
			"offense_rebound_pct", "defense_rebound_pct",
			"offense_free_throw_rate", "defense_free_throw_rate",
			"offense_three_pct", "defense_three_pct",
			"updated_at",
		}, models.TeamBoxColumns)),
	}).Create(stats).Error
}

// HasSeason reports whether any team holds figures for a season.
func (r *TeamSeasonStatsRepository) HasSeason(season int) (bool, error) {
	var found bool
	err := r.db.Raw(`SELECT EXISTS (SELECT 1 FROM team_season_stats WHERE season = ?)`, season).Scan(&found).Error
	return found, err
}

// FindForTeams returns the given teams' figures for a season, keyed by team ID. A team
// with no row is absent from the map.
func (r *TeamSeasonStatsRepository) FindForTeams(season int, teamIDs ...uuid.UUID) (map[uuid.UUID]models.TeamSeasonStats, error) {
	if len(teamIDs) == 0 {
		return nil, nil
	}

	var rows []models.TeamSeasonStats
	if err := r.db.Where("season = ? AND team_id IN ?", season, teamIDs).Find(&rows).Error; err != nil {
		return nil, err
	}

	stats := make(map[uuid.UUID]models.TeamSeasonStats, len(rows))
	for _, row := range rows {
		stats[row.TeamID] = row
	}
	return stats, nil
}
