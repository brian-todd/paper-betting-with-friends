package repository

import (
	"errors"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// HolyLockSlot is a (league, week) a user currently holds a Holy Lock in,
// carrying the kickoff of the game the marked bet sits on. That kickoff is what
// decides whether the designation can still move.
type HolyLockSlot struct {
	LeagueID    uuid.UUID
	WeekID      uuid.UUID
	BetID       uuid.UUID
	BetType     string
	ScheduledAt time.Time
}

// LeagueHolyLockRow is one member's Holy Lock for one week, with enough of the
// game attached to render the pick. Season and Week are plain ints rather than
// pointers: a Holy Lock can only sit on a game the calendar has a week for,
// which the inner join to weeks enforces.
type LeagueHolyLockRow struct {
	UserID       uuid.UUID
	Username     string
	Season       int
	Week         int
	SeasonType   string
	BetType      string
	Status       string
	Pick         string
	LineValue    *string
	OddsSnapshot decimal.Decimal
	Stake        decimal.Decimal
	HomeAbbr     string
	AwayAbbr     string
	ScheduledAt  time.Time
}

// HolyLockRepository answers which bets carry the Holy Lock flag. Like
// BetPeriodRepository it spans all three bet tables, so it is its own type
// rather than a method on any one of them.
//
// Every query here filters status <> 'void'. That one predicate is what lets a
// cancelled or admin-voided bet release its week's slot without the admin
// correction path needing to know the feature exists.
type HolyLockRepository struct {
	db *gorm.DB
}

// NewHolyLockRepository creates a new HolyLockRepository.
func NewHolyLockRepository(db *gorm.DB) *HolyLockRepository {
	return &HolyLockRepository{db: db}
}

// userLocks selects every live Holy Lock a user holds, across the three tables.
//
// The branch order is load-bearing: Postgres takes a UNION's output column
// names from the first branch alone, and only that branch aliases bet_type.
// Promote either of the others to the top and slotQuery's b.bet_type fails at
// runtime. Add a branch at the end, or carry the alias with whichever leads.
const userLocks = `
	SELECT user_id, league_id, game_id, id, 'spread' AS bet_type FROM spread_bets WHERE user_id = ? AND is_holy_lock AND status <> 'void'
	UNION ALL
	SELECT user_id, league_id, game_id, id, 'moneyline' FROM money_line_bets WHERE user_id = ? AND is_holy_lock AND status <> 'void'
	UNION ALL
	SELECT user_id, league_id, game_id, id, 'overunder' FROM over_under_bets WHERE user_id = ? AND is_holy_lock AND status <> 'void'`

// slotQuery builds the shared shape behind FindSlotsByUser and FindSlot.
//
// The order is deliberate rather than cosmetic: a schedule sync can move a game
// between weeks (GameRepository.Upsert rewrites week_id), which can transiently
// leave a user holding two locks in the destination week. Without a total order
// FindSlot would return an arbitrary one of them and the clear-and-set path
// would depend on which.
func (r *HolyLockRepository) slotQuery(userID uuid.UUID) *gorm.DB {
	return r.db.
		Table("(?) AS b", r.db.Raw(userLocks, userID, userID, userID)).
		Select("b.league_id, games.week_id AS week_id, b.id AS bet_id, b.bet_type, games.scheduled_at").
		Joins("JOIN games ON games.id = b.game_id").
		Where("games.week_id IS NOT NULL").
		Order("games.scheduled_at DESC, b.id")
}

// FindSlotsByUser returns every Holy Lock the user currently holds, across all
// their leagues. The bets page loads the lot in one query rather than asking
// per bet: whether a week is still open is a property of the week, not of the
// row being rendered.
func (r *HolyLockRepository) FindSlotsByUser(userID uuid.UUID) ([]HolyLockSlot, error) {
	var slots []HolyLockSlot
	if err := r.slotQuery(userID).Scan(&slots).Error; err != nil {
		return nil, err
	}
	return slots, nil
}

// FindSlot returns the lock holding one (user, league, week) slot, or nil when
// the slot is free.
func (r *HolyLockRepository) FindSlot(userID, leagueID, weekID uuid.UUID) (*HolyLockSlot, error) {
	var slot HolyLockSlot
	err := r.slotQuery(userID).
		Where("b.league_id = ? AND games.week_id = ?", leagueID, weekID).
		Limit(1).
		Scan(&slot).Error
	if err != nil {
		return nil, err
	}
	if slot.BetID == uuid.Nil {
		return nil, nil
	}
	return &slot, nil
}

// ClearWeek unsets the flag on every bet the user holds in one league week.
//
// It clears the whole week rather than one known bet so that a transient double
// lock -- the week reclassification above -- collapses to a single designation
// the next time one is set, instead of persisting forever.
func (r *HolyLockRepository) ClearWeek(userID, leagueID, weekID uuid.UUID) error {
	weekGames := r.db.Model(&models.Game{}).Select("id").Where("week_id = ?", weekID)

	for _, model := range []any{&models.SpreadBet{}, &models.MoneyLineBet{}, &models.OverUnderBet{}} {
		err := r.db.Model(model).
			Where("is_holy_lock AND user_id = ? AND league_id = ?", userID, leagueID).
			Where("game_id IN (?)", weekGames).
			Update("is_holy_lock", false).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// Set marks one bet as its owner's Holy Lock.
func (r *HolyLockRepository) Set(betType string, betID uuid.UUID) error {
	var model any
	switch betType {
	case "spread":
		model = &models.SpreadBet{}
	case "moneyline":
		model = &models.MoneyLineBet{}
	case "overunder":
		model = &models.OverUnderBet{}
	default:
		return errors.New("unknown bet type: " + betType)
	}

	result := r.db.Model(model).Where("id = ?", betID).Update("is_holy_lock", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// leagueLocks selects every live Holy Lock in a league. The three tables carry
// different snapshot columns, so the spread and the total are normalized to
// text under one name and the money line contributes a NULL -- there is no line
// to show for one.
const leagueLocks = `
	SELECT user_id, game_id, 'spread' AS bet_type, status, pick, stake, odds_snapshot, spread_snapshot::text AS line_value
	FROM spread_bets WHERE league_id = ? AND is_holy_lock AND status <> 'void'
	UNION ALL
	SELECT user_id, game_id, 'moneyline', status, pick, stake, odds_snapshot, NULL
	FROM money_line_bets WHERE league_id = ? AND is_holy_lock AND status <> 'void'
	UNION ALL
	SELECT user_id, game_id, 'overunder', status, pick, stake, odds_snapshot, total_snapshot::text
	FROM over_under_bets WHERE league_id = ? AND is_holy_lock AND status <> 'void'`

// FindLeagueLocks returns every Holy Lock in a league, with the game and teams
// joined on for display.
//
// This is a separate query from FindLeagueBets rather than an extension of it:
// FindLeagueBets returns every bet in the league to feed a pure aggregation and
// deliberately carries no team or pick data. WHERE is_holy_lock is what makes
// the result here small enough to afford five joins.
//
// Season comes from games and week from weeks, matching the split FindLeagueBets
// uses, so the two league-page sections group under identical headings even in
// the corner case where the two disagree.
func (r *HolyLockRepository) FindLeagueLocks(leagueID uuid.UUID) ([]LeagueHolyLockRow, error) {
	var rows []LeagueHolyLockRow
	if err := r.lockRowQuery(leagueID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindLockInWeek returns the Holy Lock a user already holds in one league week,
// or nil when the slot is free.
//
// It carries the display columns rather than the bare slot FindSlot returns,
// because its caller is the message that has to name the bet the reader would
// be replacing.
func (r *HolyLockRepository) FindLockInWeek(userID, leagueID, weekID uuid.UUID) (*LeagueHolyLockRow, error) {
	var row LeagueHolyLockRow
	err := r.lockRowQuery(leagueID).
		Where("b.user_id = ? AND games.week_id = ?", userID, weekID).
		Order("games.scheduled_at DESC").
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.UserID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

// lockRowQuery is the display projection shared by the two readers above.
func (r *HolyLockRepository) lockRowQuery(leagueID uuid.UUID) *gorm.DB {
	return r.db.
		Table("(?) AS b", r.db.Raw(leagueLocks, leagueID, leagueID, leagueID)).
		Select("b.user_id, users.username, games.season AS season, weeks.number AS week, " +
			"weeks.season_type, b.bet_type, b.status, b.pick, b.line_value, " +
			"b.odds_snapshot, b.stake, home.abbreviation AS home_abbr, " +
			"away.abbreviation AS away_abbr, games.scheduled_at").
		Joins("JOIN users ON users.id = b.user_id").
		Joins("JOIN games ON games.id = b.game_id").
		// Inner join: no week, no Holy Lock. Basketball games carry no week row.
		Joins("JOIN weeks ON weeks.id = games.week_id").
		Joins("JOIN teams AS home ON home.id = games.home_team_id").
		Joins("JOIN teams AS away ON away.id = games.away_team_id")
}
