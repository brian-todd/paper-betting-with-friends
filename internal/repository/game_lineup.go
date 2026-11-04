package repository

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameLineupRepository stores five-man unit totals and tracks which games have
// been asked for.
type GameLineupRepository struct {
	db *gorm.DB
}

// NewGameLineupRepository creates a new GameLineupRepository.
func NewGameLineupRepository(db *gorm.DB) *GameLineupRepository {
	return &GameLineupRepository{db: db}
}

// LineupFetchRules decide which finished games FindDue offers.
type LineupFetchRules struct {
	// FirstAfter is how long after tip-off a game is first asked for.
	FirstAfter time.Duration
	// RetryAfter is how long to wait after an attempt that found nothing.
	RetryAfter time.Duration
	// MaxAttempts is how many times a game is asked for before giving up.
	MaxAttempts int
	// Lookback bounds how old a game can be. Without it, the first run after a
	// past season has been seeded would walk thousands of finished games.
	Lookback time.Duration
}

// DueLineupGame is a game to ask the feed about.
type DueLineupGame struct {
	ID         uuid.UUID
	ExternalID int64
}

// FindDue returns up to limit finished basketball games with a line that are due
// for a request. Games never asked for come first, newest first, so a retry cannot
// hold up a fresh game.
//
// Only games with a line, because those are the only ones anyone can bet on: the
// rest are nearly all non-Division I opponents, whose side the feed does not cover.
func (r *GameLineupRepository) FindDue(now time.Time, rules LineupFetchRules, limit int) ([]DueLineupGame, error) {
	var games []DueLineupGame
	err := r.db.Model(&models.Game{}).
		Select("games.id, games.external_id").
		Joins("LEFT JOIN game_lineup_fetches f ON f.game_id = games.id").
		Where("games.sport = ? AND games.status = ?", models.SportBasketball, models.GameStatusFinal).
		Where("games.external_id IS NOT NULL").
		Where("games.scheduled_at <= ? AND games.scheduled_at >= ?", now.Add(-rules.FirstAfter), now.Add(-rules.Lookback)).
		Where(gameHasOddsClause).
		Where("f.game_id IS NULL OR (f.found_at IS NULL AND f.attempts < ? AND f.last_attempt_at <= ?)",
			rules.MaxAttempts, now.Add(-rules.RetryAfter)).
		Order("COALESCE(f.attempts, 0)").
		Order("games.scheduled_at DESC").
		Order("games.id").
		Limit(limit).
		Scan(&games).Error
	if err != nil {
		return nil, err
	}
	return games, nil
}

// RecordFetch stores what one request found and counts the attempt, together, so a
// failed write cannot use up an attempt on lineups that were never stored. Re-running
// replaces a game's rows. A nil lineups records an answer that found nothing.
func (r *GameLineupRepository) RecordFetch(gameID uuid.UUID, lineups []models.GameLineup, at time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var foundAt *time.Time
		if len(lineups) > 0 {
			foundAt = &at
			if err := tx.Where("game_id = ?", gameID).Delete(&models.GameLineup{}).Error; err != nil {
				return err
			}
			if err := tx.Create(&lineups).Error; err != nil {
				return err
			}
		}

		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "game_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"attempts":        gorm.Expr("game_lineup_fetches.attempts + 1"),
				"last_attempt_at": gorm.Expr("excluded.last_attempt_at"),
				"found_at":        gorm.Expr("COALESCE(excluded.found_at, game_lineup_fetches.found_at)"),
			}),
		}).Create(&models.GameLineupFetch{
			GameID: gameID, Attempts: 1, LastAttemptAt: at, FoundAt: foundAt,
		}).Error
	})
}

// RecordFailure starts the retry wait after a request that got no answer, without
// counting an attempt: the attempts are for answers, which is what they bound.
func (r *GameLineupRepository) RecordFailure(gameID uuid.UUID, at time.Time) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_attempt_at"}),
	}).Create(&models.GameLineupFetch{GameID: gameID, Attempts: 0, LastAttemptAt: at}).Error
}

// TopForGame returns a team's most-used units in one game.
func (r *GameLineupRepository) TopForGame(gameID, teamID uuid.UUID, limit int) ([]models.LineupTotals, error) {
	return r.top(r.db.Where("game_lineups.game_id = ? AND game_lineups.team_id = ?", gameID, teamID), limit)
}

// TopForSeason returns a team's most-used units in one season across games that
// tipped off before the given instant, so a page never shows a game what came after
// it.
func (r *GameLineupRepository) TopForSeason(teamID uuid.UUID, season int, before time.Time, limit int) ([]models.LineupTotals, error) {
	return r.top(r.db.
		Joins("JOIN games ON games.id = game_lineups.game_id").
		Where("game_lineups.team_id = ? AND games.season = ? AND games.scheduled_at < ?", teamID, season, before), limit)
}

func (r *GameLineupRepository) top(scope *gorm.DB, limit int) ([]models.LineupTotals, error) {
	var totals []models.LineupTotals
	err := scope.Model(&models.GameLineup{}).
		Select(`(array_agg(game_lineups.athletes))[1] AS athletes,
			SUM(game_lineups.seconds) AS seconds,
			SUM(game_lineups.points_for) AS points_for,
			SUM(game_lineups.possessions_for) AS possessions_for,
			SUM(game_lineups.points_against) AS points_against,
			SUM(game_lineups.possessions_against) AS possessions_against`).
		Group("game_lineups.id_hash").
		Order("SUM(game_lineups.seconds) DESC").
		Order("game_lineups.id_hash").
		Limit(limit).
		Scan(&totals).Error
	if err != nil {
		return nil, err
	}
	return totals, nil
}
