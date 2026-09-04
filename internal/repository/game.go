package repository

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameRepository provides methods for interacting with games in the database.
type GameRepository struct {
	db *gorm.DB
}

// NewGameRepository creates a new GameRepository.
func NewGameRepository(db *gorm.DB) *GameRepository {
	return &GameRepository{db: db}
}

// Create inserts a new game into the database.
func (r *GameRepository) Create(game *models.Game) error {
	return r.db.Create(game).Error
}

// FindByID retrieves a game by its ID with all relationships.
func (r *GameRepository) FindByID(id uuid.UUID) (*models.Game, error) {
	var game models.Game
	err := r.db.
		Preload("HomeTeam.HomeVenue").
		Preload("AwayTeam.HomeVenue").
		Preload("Venue").
		Preload("Week").
		Preload("Result").
		First(&game, id).Error
	if err != nil {
		return nil, err
	}
	return &game, nil
}

// GameFilter narrows a week's games. The zero value matches every game in the
// week. Slice fields are OR'd within themselves and AND'd against each other,
// and the team-based ones match if *either* side qualifies -- an FBS team
// hosting an FCS one is an FBS game for filtering purposes.
type GameFilter struct {
	Conferences  []string // home or away team conference
	Tiers        []string // home or away team classification: fbs, fcs, ii, iii
	Status       string   // exact models.GameStatus, empty for any
	Team         string   // case-insensitive substring of either team's name or abbreviation
	BettableOnly bool     // only games that have at least one odds row
	Weekdays     []int    // kickoff weekday in Location, 0=Sunday .. 6=Saturday
	StartHour    *int     // kickoff at or after this hour in Location
	EndHour      *int     // kickoff before the end of this hour in Location

	// Odds ranges. A game matches when any of its odds rows for that market
	// falls in range, so a game survives if some book priced it that way.
	SpreadMin *decimal.Decimal // magnitude, so 7 matches a 7-point line either way
	SpreadMax *decimal.Decimal
	TotalMin  *decimal.Decimal
	TotalMax  *decimal.Decimal
	// MoneyLineMin/Max match if *either* side's price is in range, which is
	// what makes "anything paying +200 or better" work.
	MoneyLineMin *decimal.Decimal
	MoneyLineMax *decimal.Decimal

	// RankedTeam and RankedMatchup narrow on poll position. Both read against
	// RankingPoll, which the service fills in per week -- CFP committee
	// rankings when the week has them, else AP Top 25. An empty RankingPoll
	// (a week with no poll synced) matches nothing, which is correct: no game
	// in that week is a ranked matchup.
	RankedTeam    bool // at least one side ranked in RankingPoll
	RankedMatchup bool // both sides ranked in RankingPoll
	RankingPoll   string

	// Location resolves Weekdays and StartHour/EndHour. Those are calendar
	// questions, not instants, so they are meaningless without it -- see the
	// timezone rules in AGENTS.md. Required only when one of them is set.
	Location *time.Location
}

// WeekConference is one entry in the conference filter's option list, carrying
// enough context for the UI to group by division and show a game count.
type WeekConference struct {
	Conference     string
	Classification string
	Games          int
}

// FindWeekGames retrieves one page of a week's games, narrowed by filter, along
// with the total number of games matching that filter.
//
// The whole filter runs in SQL rather than in the browser because a busy week
// holds over 400 games, most of them lower-division fixtures that carry no odds
// and can never be bet on. Rendering all of them and hiding the unwanted ones
// with CSS still ships every card.
//
// Games sort live first, then scheduled, then everything settled, and by kickoff
// within each group. The id tiebreaker keeps paging stable when several games
// share a kickoff time.
func (r *GameRepository) FindWeekGames(weekID uuid.UUID, filter GameFilter, offset, limit int) ([]models.Game, int, error) {
	// Count and fetch have to run off separate statements, so the shared parts
	// are rebuilt rather than reused -- Count mutates the query it runs on.
	total, err := r.CountWeekGames(weekID, filter)
	if err != nil {
		return nil, 0, err
	}

	var games []models.Game
	err = r.weekGamesQuery(weekID, filter).
		Preload("HomeTeam").
		Preload("AwayTeam").
		Preload("Venue").
		Preload("Week").
		Preload("Result").
		Order("CASE games.status WHEN 'in_progress' THEN 0 WHEN 'scheduled' THEN 1 ELSE 2 END").
		Order("games.scheduled_at").
		Order("games.id").
		Offset(offset).
		Limit(limit).
		Find(&games).Error
	if err != nil {
		return nil, 0, err
	}
	return games, total, nil
}

// CountWeekGames reports how many of a week's games match filter.
func (r *GameRepository) CountWeekGames(weekID uuid.UUID, filter GameFilter) (int, error) {
	var total int64
	if err := r.weekGamesQuery(weekID, filter).Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

// weekGamesQuery builds the shared, unordered base for the week queries.
func (r *GameRepository) weekGamesQuery(weekID uuid.UUID, filter GameFilter) *gorm.DB {
	q := r.db.Model(&models.Game{}).
		Joins("JOIN teams home_team ON home_team.id = games.home_team_id").
		Joins("JOIN teams away_team ON away_team.id = games.away_team_id").
		Where("games.week_id = ?", weekID)
	return applyGameFilter(q, filter)
}

// applyGameFilter adds filter's conditions to a query that has already joined
// teams as home_team and away_team.
func applyGameFilter(q *gorm.DB, filter GameFilter) *gorm.DB {
	// Every OR group is parenthesised explicitly. Leaving that to the query
	// builder is a good way to discover that one unmatched conference has
	// quietly widened the whole WHERE clause.
	if len(filter.Conferences) > 0 {
		q = q.Where("(home_team.conference IN ? OR away_team.conference IN ?)", filter.Conferences, filter.Conferences)
	}
	if len(filter.Tiers) > 0 {
		q = q.Where("(home_team.classification IN ? OR away_team.classification IN ?)", filter.Tiers, filter.Tiers)
	}
	if filter.Status != "" {
		q = q.Where("games.status = ?", filter.Status)
	}
	if filter.Team != "" {
		term := "%" + filter.Team + "%"
		q = q.Where(
			"(home_team.name ILIKE ? OR home_team.abbreviation ILIKE ? OR away_team.name ILIKE ? OR away_team.abbreviation ILIKE ?)",
			term, term, term, term)
	}
	if filter.BettableOnly {
		q = q.Where(`(EXISTS (SELECT 1 FROM spread_odds WHERE spread_odds.game_id = games.id)
			OR EXISTS (SELECT 1 FROM money_line_odds WHERE money_line_odds.game_id = games.id)
			OR EXISTS (SELECT 1 FROM over_under_odds WHERE over_under_odds.game_id = games.id))`)
	}

	// Spreads are stored from the home side and away_spread is its negation,
	// so the absolute value is the line's magnitude whichever team is favoured.
	if filter.SpreadMin != nil || filter.SpreadMax != nil {
		q = q.Where("EXISTS (?)", oddsInRange(q, "spread_odds", "ABS(spread_odds.home_spread)", filter.SpreadMin, filter.SpreadMax))
	}
	if filter.TotalMin != nil || filter.TotalMax != nil {
		q = q.Where("EXISTS (?)", oddsInRange(q, "over_under_odds", "over_under_odds.total", filter.TotalMin, filter.TotalMax))
	}
	if filter.MoneyLineMin != nil || filter.MoneyLineMax != nil {
		home := oddsInRange(q, "money_line_odds", "money_line_odds.home_odds", filter.MoneyLineMin, filter.MoneyLineMax)
		away := oddsInRange(q, "money_line_odds", "money_line_odds.away_odds", filter.MoneyLineMin, filter.MoneyLineMax)
		q = q.Where("(EXISTS (?) OR EXISTS (?))", home, away)
	}
	if filter.RankedMatchup || filter.RankedTeam {
		home := rankedSide(q, "home_team.id", filter.RankingPoll)
		away := rankedSide(q, "away_team.id", filter.RankingPoll)
		if filter.RankedMatchup {
			q = q.Where("(EXISTS (?) AND EXISTS (?))", home, away)
		} else {
			q = q.Where("(EXISTS (?) OR EXISTS (?))", home, away)
		}
	}

	// scheduled_at is a TIMESTAMPTZ, so shifting it into the display zone is
	// what makes "Saturday" mean the reader's Saturday and not UTC's.
	if filter.Location != nil {
		zone := filter.Location.String()
		if len(filter.Weekdays) > 0 {
			q = q.Where("EXTRACT(DOW FROM games.scheduled_at AT TIME ZONE ?) IN ?", zone, filter.Weekdays)
		}
		if filter.StartHour != nil {
			q = q.Where("EXTRACT(HOUR FROM games.scheduled_at AT TIME ZONE ?) >= ?", zone, *filter.StartHour)
		}
		if filter.EndHour != nil {
			q = q.Where("EXTRACT(HOUR FROM games.scheduled_at AT TIME ZONE ?) <= ?", zone, *filter.EndHour)
		}
	}

	return q
}

// oddsInRange builds the correlated subquery behind an odds range filter: does
// this game have a row in the given market whose value sits in range.
//
// The bounds stay decimal.Decimal the whole way down rather than becoming
// float64 -- a 2.5 spread has to compare exactly against a stored 2.5.
func oddsInRange(q *gorm.DB, table, column string, low, high *decimal.Decimal) *gorm.DB {
	sub := q.Session(&gorm.Session{NewDB: true}).
		Table(table).
		Select("1").
		Where(table + ".game_id = games.id")
	if low != nil {
		sub = sub.Where(column+" >= ?", *low)
	}
	if high != nil {
		sub = sub.Where(column+" <= ?", *high)
	}
	return sub
}

// rankedSide builds the correlated subquery behind a ranked-team filter: does
// the team named by teamColumn (a fixed "home_team.id"/"away_team.id"
// literal, never caller input) have a row in the week's effective poll.
func rankedSide(q *gorm.DB, teamColumn, poll string) *gorm.DB {
	return q.Session(&gorm.Session{NewDB: true}).
		Table("team_rankings").
		Select("1").
		Where("team_rankings.week_id = games.week_id").
		Where("team_rankings.team_id = "+teamColumn).
		Where("team_rankings.poll = ?", poll)
}

// FindWeekConferences lists every conference with a team playing in the week,
// so the filter offers only conferences that can actually match something.
func (r *GameRepository) FindWeekConferences(weekID uuid.UUID) ([]WeekConference, error) {
	const sides = `
		SELECT g.id AS game_id, t.conference, COALESCE(t.classification, '') AS classification
		FROM games g JOIN teams t ON t.id = g.home_team_id WHERE g.week_id = ?
		UNION
		SELECT g.id, t.conference, COALESCE(t.classification, '')
		FROM games g JOIN teams t ON t.id = g.away_team_id WHERE g.week_id = ?`

	var conferences []WeekConference
	err := r.db.
		Table("(?) AS sides", r.db.Raw(sides, weekID, weekID)).
		Select("sides.conference, sides.classification, COUNT(DISTINCT sides.game_id) AS games").
		Where("sides.conference <> ''").
		Group("sides.conference, sides.classification").
		Order("sides.classification, sides.conference").
		Scan(&conferences).Error
	if err != nil {
		return nil, err
	}
	return conferences, nil
}

// FindByTeam retrieves all games where a team is playing (home or away).
func (r *GameRepository) FindByTeam(teamID uuid.UUID) ([]models.Game, error) {
	var games []models.Game
	err := r.db.
		Preload("HomeTeam").
		Preload("AwayTeam").
		Preload("Venue").
		Preload("Week").
		Where("home_team_id = ? OR away_team_id = ?", teamID, teamID).
		Order("scheduled_at DESC").
		Find(&games).Error
	if err != nil {
		return nil, err
	}
	return games, nil
}

// FindBySeasonAndWeek retrieves all games for a given season and week number.
func (r *GameRepository) FindBySeasonAndWeek(season, weekNumber int) ([]models.Game, error) {
	var games []models.Game
	err := r.db.
		Preload("HomeTeam").
		Preload("AwayTeam").
		Preload("Venue").
		Preload("Week").
		Joins("JOIN weeks ON weeks.id = games.week_id").
		Where("weeks.season = ? AND weeks.number = ?", season, weekNumber).
		Order("scheduled_at").
		Find(&games).Error
	if err != nil {
		return nil, err
	}
	return games, nil
}

// Update saves changes to an existing game.
func (r *GameRepository) Update(game *models.Game) error {
	return r.db.Save(game).Error
}

// Delete removes a game from the database.
func (r *GameRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&models.Game{}, id).Error
}

// FindByExternalID retrieves a game by its external API ID and sport.
func (r *GameRepository) FindByExternalID(externalID int64, sport string) (*models.Game, error) {
	var game models.Game
	if err := r.db.Preload("HomeTeam").Preload("AwayTeam").Preload("Venue").Preload("Week").Where("external_id = ? AND sport = ?", externalID, sport).First(&game).Error; err != nil {
		return nil, err
	}
	return &game, nil
}

// Upsert creates or updates a game based on (external_id, sport).
func (r *GameRepository) Upsert(game *models.Game) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "external_id"}, {Name: "sport"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"home_team_id", "away_team_id", "venue_id", "week_id",
			"season", "season_type", "tournament", "home_seed", "away_seed",
			"scheduled_at", "status", "neutral_site", "conference_game", "completed", "updated_at",
		}),
	}).Create(game).Error
}

// FindByDateRangeAndSport retrieves all games for a sport within a date range.
func (r *GameRepository) FindByDateRangeAndSport(sport string, start, end time.Time) ([]models.Game, error) {
	var games []models.Game
	err := r.db.
		Preload("HomeTeam").
		Preload("AwayTeam").
		Preload("Venue").
		Preload("Result").
		Where("sport = ? AND scheduled_at >= ? AND scheduled_at < ?", sport, start, end).
		Order("scheduled_at").
		Find(&games).Error
	if err != nil {
		return nil, err
	}
	return games, nil
}

// SearchByTeamName finds games where either side's team name matches query,
// newest first. It is the admin game lookup: one query with both sides joined,
// rather than resolving teams and then fanning out over FindByTeam.
func (r *GameRepository) SearchByTeamName(query string, limit int) ([]models.Game, error) {
	var games []models.Game
	pattern := "%" + query + "%"

	err := r.db.
		Preload("HomeTeam").Preload("AwayTeam").Preload("Venue").
		Preload("Week").Preload("Result").
		Joins("JOIN teams home_team ON home_team.id = games.home_team_id").
		Joins("JOIN teams away_team ON away_team.id = games.away_team_id").
		Where("home_team.name ILIKE ? OR away_team.name ILIKE ?", pattern, pattern).
		Order("games.scheduled_at DESC").
		Limit(limit).
		Find(&games).Error
	if err != nil {
		return nil, err
	}
	return games, nil
}
