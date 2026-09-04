package repository

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// UserBetRow is one live bet a user holds on a game, with enough of the game
// and teams joined on to describe the pick. Deliberately not joined to weeks,
// unlike LeagueHolyLockRow it otherwise mirrors: basketball games carry no
// week, and this has to describe those bets too.
type UserBetRow struct {
	GameID       uuid.UUID
	BetType      string
	Pick         string
	LineValue    *string
	OddsSnapshot decimal.Decimal
	HomeAbbr     string
	AwayAbbr     string
}

// UserBetRepository answers which bets a user holds on a set of games. It
// spans all three bet tables, so it is its own type rather than a method on
// any one of them -- the same shape as BetPeriodRepository and
// HolyLockRepository.
type UserBetRepository struct {
	db *gorm.DB
}

// NewUserBetRepository creates a new UserBetRepository.
func NewUserBetRepository(db *gorm.DB) *UserBetRepository {
	return &UserBetRepository{db: db}
}

const userGameBets = `
	SELECT user_id, game_id, 'spread' AS bet_type, pick, odds_snapshot, spread_snapshot::text AS line_value
	FROM spread_bets WHERE user_id = ? AND status <> 'void'
	UNION ALL
	SELECT user_id, game_id, 'moneyline', pick, odds_snapshot, NULL
	FROM money_line_bets WHERE user_id = ? AND status <> 'void'
	UNION ALL
	SELECT user_id, game_id, 'overunder', pick, odds_snapshot, total_snapshot::text
	FROM over_under_bets WHERE user_id = ? AND status <> 'void'`

// FindByUserAndGames returns every live bet the user holds on the given
// games, with the teams joined on so the caller can describe the pick without
// a query per game. Void is excluded: a cancelled bet should not read back on
// the games grid as one still placed.
func (r *UserBetRepository) FindByUserAndGames(userID uuid.UUID, gameIDs []uuid.UUID) ([]UserBetRow, error) {
	if len(gameIDs) == 0 {
		return nil, nil
	}

	var rows []UserBetRow
	err := r.db.
		Table("(?) AS b", r.db.Raw(userGameBets, userID, userID, userID)).
		Select("b.game_id, b.bet_type, b.pick, b.line_value, b.odds_snapshot, "+
			"home.abbreviation AS home_abbr, away.abbreviation AS away_abbr").
		Joins("JOIN games ON games.id = b.game_id").
		Joins("JOIN teams AS home ON home.id = games.home_team_id").
		Joins("JOIN teams AS away ON away.id = games.away_team_id").
		Where("b.game_id IN ?", gameIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
