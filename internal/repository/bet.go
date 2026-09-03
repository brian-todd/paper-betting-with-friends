package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SpreadBetRepository provides methods for interacting with spread bets.
type SpreadBetRepository struct {
	db *gorm.DB
}

// NewSpreadBetRepository creates a new SpreadBetRepository.
func NewSpreadBetRepository(db *gorm.DB) *SpreadBetRepository {
	return &SpreadBetRepository{db: db}
}

// Create inserts a new spread bet into the database.
func (r *SpreadBetRepository) Create(bet *models.SpreadBet) error {
	return r.db.Create(bet).Error
}

// FindByID retrieves a spread bet by ID.
func (r *SpreadBetRepository) FindByID(id uuid.UUID) (*models.SpreadBet, error) {
	var bet models.SpreadBet
	if err := r.db.Preload("Game").Preload("SpreadOdds").Preload("League").First(&bet, id).Error; err != nil {
		return nil, err
	}
	return &bet, nil
}

// FindByUser retrieves all spread bets for a user.
func (r *SpreadBetRepository) FindByUser(userID uuid.UUID) ([]models.SpreadBet, error) {
	var bets []models.SpreadBet
	if err := r.db.Where("user_id = ?", userID).
		Preload("Game.HomeTeam").Preload("Game.AwayTeam").
		Preload("League").
		Order("created_at DESC").
		Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// FindPendingByGame retrieves all pending spread bets for a game.
func (r *SpreadBetRepository) FindPendingByGame(gameID uuid.UUID) ([]models.SpreadBet, error) {
	var bets []models.SpreadBet
	if err := r.db.Where("game_id = ? AND status = ?", gameID, models.BetStatusPending).Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// Update saves changes to an existing spread bet.
//
// Associations are omitted deliberately. FindByID preloads SpreadOdds, and a
// plain Save would write the preloaded row's ID back over SpreadOddsID -- so
// moving a bet to a different line silently kept it pointing at the old one.
func (r *SpreadBetRepository) Update(bet *models.SpreadBet) error {
	return r.db.Omit(clause.Associations).Save(bet).Error
}

// BetFilter contains optional filters for querying bets. Every field is
// optional; a zero BetFilter matches every bet, which is what the admin bet
// browser starts from.
type BetFilter struct {
	Season   *int
	Week     *int
	LeagueID *uuid.UUID
	UserID   *uuid.UUID
	Status   *models.BetStatus
}

// FindFiltered retrieves spread bets matching filter, newest first. Every filter
// field is optional, so this serves both a single user's bet list and the
// admin browser over all users.
func (r *SpreadBetRepository) FindFiltered(filter BetFilter) ([]models.SpreadBet, error) {
	var bets []models.SpreadBet
	query := r.db.
		Joins("JOIN games ON games.id = spread_bets.game_id").
		Joins("LEFT JOIN weeks ON weeks.id = games.week_id").
		Preload("Game.HomeTeam").Preload("Game.AwayTeam").Preload("Game.Venue").
		Preload("Game.Week").Preload("Game.Result").
		Preload("League").Preload("User")

	if filter.UserID != nil {
		query = query.Where("spread_bets.user_id = ?", *filter.UserID)
	}
	if filter.Season != nil {
		query = query.Where("games.season = ?", *filter.Season)
	}
	if filter.Week != nil {
		query = query.Where("weeks.number = ?", *filter.Week)
	}
	if filter.LeagueID != nil {
		query = query.Where("spread_bets.league_id = ?", *filter.LeagueID)
	}
	if filter.Status != nil {
		query = query.Where("spread_bets.status = ?", *filter.Status)
	}

	if err := query.Order("spread_bets.created_at DESC").Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// FindByUserFiltered retrieves spread bets for one user with optional filters.
func (r *SpreadBetRepository) FindByUserFiltered(userID uuid.UUID, filter BetFilter) ([]models.SpreadBet, error) {
	filter.UserID = &userID
	return r.FindFiltered(filter)
}

// MoneyLineBetRepository provides methods for interacting with money line bets.
type MoneyLineBetRepository struct {
	db *gorm.DB
}

// NewMoneyLineBetRepository creates a new MoneyLineBetRepository.
func NewMoneyLineBetRepository(db *gorm.DB) *MoneyLineBetRepository {
	return &MoneyLineBetRepository{db: db}
}

// Create inserts a new money line bet into the database.
func (r *MoneyLineBetRepository) Create(bet *models.MoneyLineBet) error {
	return r.db.Create(bet).Error
}

// FindByID retrieves a money line bet by ID.
func (r *MoneyLineBetRepository) FindByID(id uuid.UUID) (*models.MoneyLineBet, error) {
	var bet models.MoneyLineBet
	if err := r.db.Preload("Game").Preload("MoneyLineOdds").Preload("League").First(&bet, id).Error; err != nil {
		return nil, err
	}
	return &bet, nil
}

// FindByUser retrieves all money line bets for a user.
func (r *MoneyLineBetRepository) FindByUser(userID uuid.UUID) ([]models.MoneyLineBet, error) {
	var bets []models.MoneyLineBet
	if err := r.db.Where("user_id = ?", userID).
		Preload("Game.HomeTeam").Preload("Game.AwayTeam").
		Preload("League").
		Order("created_at DESC").
		Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// FindPendingByGame retrieves all pending money line bets for a game.
func (r *MoneyLineBetRepository) FindPendingByGame(gameID uuid.UUID) ([]models.MoneyLineBet, error) {
	var bets []models.MoneyLineBet
	if err := r.db.Where("game_id = ? AND status = ?", gameID, models.BetStatusPending).Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// Update saves changes to an existing money line bet. See SpreadBetRepository
// .Update for why associations are omitted.
func (r *MoneyLineBetRepository) Update(bet *models.MoneyLineBet) error {
	return r.db.Omit(clause.Associations).Save(bet).Error
}

// FindFiltered retrieves money line bets matching filter, newest first. Every filter
// field is optional, so this serves both a single user's bet list and the
// admin browser over all users.
func (r *MoneyLineBetRepository) FindFiltered(filter BetFilter) ([]models.MoneyLineBet, error) {
	var bets []models.MoneyLineBet
	query := r.db.
		Joins("JOIN games ON games.id = money_line_bets.game_id").
		Joins("LEFT JOIN weeks ON weeks.id = games.week_id").
		Preload("Game.HomeTeam").Preload("Game.AwayTeam").Preload("Game.Venue").
		Preload("Game.Week").Preload("Game.Result").
		Preload("League").Preload("User")

	if filter.UserID != nil {
		query = query.Where("money_line_bets.user_id = ?", *filter.UserID)
	}
	if filter.Season != nil {
		query = query.Where("games.season = ?", *filter.Season)
	}
	if filter.Week != nil {
		query = query.Where("weeks.number = ?", *filter.Week)
	}
	if filter.LeagueID != nil {
		query = query.Where("money_line_bets.league_id = ?", *filter.LeagueID)
	}
	if filter.Status != nil {
		query = query.Where("money_line_bets.status = ?", *filter.Status)
	}

	if err := query.Order("money_line_bets.created_at DESC").Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// FindByUserFiltered retrieves money line bets for one user with optional filters.
func (r *MoneyLineBetRepository) FindByUserFiltered(userID uuid.UUID, filter BetFilter) ([]models.MoneyLineBet, error) {
	filter.UserID = &userID
	return r.FindFiltered(filter)
}

// OverUnderBetRepository provides methods for interacting with over/under bets.
type OverUnderBetRepository struct {
	db *gorm.DB
}

// NewOverUnderBetRepository creates a new OverUnderBetRepository.
func NewOverUnderBetRepository(db *gorm.DB) *OverUnderBetRepository {
	return &OverUnderBetRepository{db: db}
}

// Create inserts a new over/under bet into the database.
func (r *OverUnderBetRepository) Create(bet *models.OverUnderBet) error {
	return r.db.Create(bet).Error
}

// FindByID retrieves an over/under bet by ID.
func (r *OverUnderBetRepository) FindByID(id uuid.UUID) (*models.OverUnderBet, error) {
	var bet models.OverUnderBet
	if err := r.db.Preload("Game").Preload("OverUnderOdds").Preload("League").First(&bet, id).Error; err != nil {
		return nil, err
	}
	return &bet, nil
}

// FindByUser retrieves all over/under bets for a user.
func (r *OverUnderBetRepository) FindByUser(userID uuid.UUID) ([]models.OverUnderBet, error) {
	var bets []models.OverUnderBet
	if err := r.db.Where("user_id = ?", userID).
		Preload("Game.HomeTeam").Preload("Game.AwayTeam").
		Preload("League").
		Order("created_at DESC").
		Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// FindPendingByGame retrieves all pending over/under bets for a game.
func (r *OverUnderBetRepository) FindPendingByGame(gameID uuid.UUID) ([]models.OverUnderBet, error) {
	var bets []models.OverUnderBet
	if err := r.db.Where("game_id = ? AND status = ?", gameID, models.BetStatusPending).Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// Update saves changes to an existing over/under bet. See SpreadBetRepository
// .Update for why associations are omitted.
func (r *OverUnderBetRepository) Update(bet *models.OverUnderBet) error {
	return r.db.Omit(clause.Associations).Save(bet).Error
}

// FindFiltered retrieves over/under bets matching filter, newest first. Every filter
// field is optional, so this serves both a single user's bet list and the
// admin browser over all users.
func (r *OverUnderBetRepository) FindFiltered(filter BetFilter) ([]models.OverUnderBet, error) {
	var bets []models.OverUnderBet
	query := r.db.
		Joins("JOIN games ON games.id = over_under_bets.game_id").
		Joins("LEFT JOIN weeks ON weeks.id = games.week_id").
		Preload("Game.HomeTeam").Preload("Game.AwayTeam").Preload("Game.Venue").
		Preload("Game.Week").Preload("Game.Result").
		Preload("League").Preload("User")

	if filter.UserID != nil {
		query = query.Where("over_under_bets.user_id = ?", *filter.UserID)
	}
	if filter.Season != nil {
		query = query.Where("games.season = ?", *filter.Season)
	}
	if filter.Week != nil {
		query = query.Where("weeks.number = ?", *filter.Week)
	}
	if filter.LeagueID != nil {
		query = query.Where("over_under_bets.league_id = ?", *filter.LeagueID)
	}
	if filter.Status != nil {
		query = query.Where("over_under_bets.status = ?", *filter.Status)
	}

	if err := query.Order("over_under_bets.created_at DESC").Find(&bets).Error; err != nil {
		return nil, err
	}
	return bets, nil
}

// FindByUserFiltered retrieves over/under bets for one user with optional filters.
func (r *OverUnderBetRepository) FindByUserFiltered(userID uuid.UUID, filter BetFilter) ([]models.OverUnderBet, error) {
	filter.UserID = &userID
	return r.FindFiltered(filter)
}

// BetRecord holds win/loss/push counts for a user in a league. The Lock*
// counts are the same bets counted again over just the Holy Locks, since the
// marked pick is the one people actually argue about.
type BetRecord struct {
	UserID     uuid.UUID
	Wins       int
	Losses     int
	Pushes     int
	LockWins   int
	LockLosses int
	LockPushes int
}

// BetRecordRepository provides methods for querying aggregated bet statistics.
type BetRecordRepository struct {
	db *gorm.DB
}

// NewBetRecordRepository creates a new BetRecordRepository.
func NewBetRecordRepository(db *gorm.DB) *BetRecordRepository {
	return &BetRecordRepository{db: db}
}

// GetRecordsByLeague returns win/loss/push records for all users in a league,
// counted both overall and over the Holy Locks alone.
//
// The three bet tables are queried in a loop rather than one block each: the
// grouping is identical, and a per-table copy is a place for the two records to
// drift apart.
func (r *BetRecordRepository) GetRecordsByLeague(leagueID uuid.UUID) (map[uuid.UUID]BetRecord, error) {
	records := make(map[uuid.UUID]BetRecord)

	for _, model := range []any{&models.SpreadBet{}, &models.MoneyLineBet{}, &models.OverUnderBet{}} {
		var results []struct {
			UserID     uuid.UUID
			Status     string
			IsHolyLock bool
			Count      int
		}
		if err := r.db.Model(model).
			Select("user_id, status, is_holy_lock, COUNT(*) as count").
			Where("league_id = ? AND status IN ?", leagueID, []string{"won", "lost", "push"}).
			Group("user_id, status, is_holy_lock").
			Scan(&results).Error; err != nil {
			return nil, err
		}

		for _, res := range results {
			rec := records[res.UserID]
			rec.UserID = res.UserID
			switch res.Status {
			case "won":
				rec.Wins += res.Count
				if res.IsHolyLock {
					rec.LockWins += res.Count
				}
			case "lost":
				rec.Losses += res.Count
				if res.IsHolyLock {
					rec.LockLosses += res.Count
				}
			case "push":
				rec.Pushes += res.Count
				if res.IsHolyLock {
					rec.LockPushes += res.Count
				}
			}
			records[res.UserID] = rec
		}
	}

	return records, nil
}

// LeagueBetRow is one bet in a league, flattened across the three bet tables
// and tagged with the game's season and week. Season and Week are nil when the
// game is missing that calendar data.
type LeagueBetRow struct {
	UserID       uuid.UUID
	Username     string
	Season       *int
	Week         *int
	Status       string
	Stake        decimal.Decimal
	OddsSnapshot decimal.Decimal
}

// FindLeagueBets returns every bet in a league as flat rows, for aggregation
// into per-week, per-user statistics. Payout math stays in Go
// (models.PayoutForOdds) so the sums match what settlement actually credited.
func (r *BetRecordRepository) FindLeagueBets(leagueID uuid.UUID) ([]LeagueBetRow, error) {
	const leagueBets = `
		SELECT user_id, game_id, status, stake, odds_snapshot FROM spread_bets WHERE league_id = ?
		UNION ALL
		SELECT user_id, game_id, status, stake, odds_snapshot FROM money_line_bets WHERE league_id = ?
		UNION ALL
		SELECT user_id, game_id, status, stake, odds_snapshot FROM over_under_bets WHERE league_id = ?`

	var rows []LeagueBetRow
	err := r.db.
		Table("(?) AS b", r.db.Raw(leagueBets, leagueID, leagueID, leagueID)).
		Select("b.user_id, users.username, games.season AS season, weeks.number AS week, b.status, b.stake, b.odds_snapshot").
		Joins("JOIN users ON users.id = b.user_id").
		Joins("JOIN games ON games.id = b.game_id").
		Joins("LEFT JOIN weeks ON weeks.id = games.week_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// BetPeriod is a season and week the user has at least one bet in. Week is nil
// for a bet on a game the calendar has no week row for.
type BetPeriod struct {
	Season int
	Week   *int
}

// BetPeriodRepository answers which seasons and weeks a user has bets in. It
// spans all three bet tables, so it is its own type rather than a method on
// any one of them.
type BetPeriodRepository struct {
	db *gorm.DB
}

// NewBetPeriodRepository creates a new BetPeriodRepository.
func NewBetPeriodRepository(db *gorm.DB) *BetPeriodRepository {
	return &BetPeriodRepository{db: db}
}

// FindByUser returns the distinct season/week pairs the user has bets in,
// newest season first.
//
// The bets page builds its filter dropdowns from this rather than from the
// whole calendar, so it only offers periods that actually hold a bet --
// otherwise someone with a single week 1 bet picks among sixteen weeks, fifteen
// of which come back empty.
func (r *BetPeriodRepository) FindByUser(userID uuid.UUID) ([]BetPeriod, error) {
	const userBets = `
		SELECT game_id FROM spread_bets WHERE user_id = ?
		UNION ALL
		SELECT game_id FROM money_line_bets WHERE user_id = ?
		UNION ALL
		SELECT game_id FROM over_under_bets WHERE user_id = ?`

	var periods []BetPeriod
	err := r.db.
		Table("(?) AS b", r.db.Raw(userBets, userID, userID, userID)).
		Select("DISTINCT games.season AS season, weeks.number AS week").
		Joins("JOIN games ON games.id = b.game_id").
		Joins("LEFT JOIN weeks ON weeks.id = games.week_id").
		Where("games.season IS NOT NULL").
		Order("season DESC, week").
		Scan(&periods).Error
	if err != nil {
		return nil, err
	}
	return periods, nil
}
