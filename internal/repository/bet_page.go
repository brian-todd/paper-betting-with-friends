package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BetRef names one bet in the merged, newest-first ordering of a user's bets,
// without loading it. BetType says which table to load it from.
type BetRef struct {
	BetID   uuid.UUID
	BetType string
}

// BetPageRepository answers which bets fall on one page of a filtered listing.
// It spans all three bet tables, so it is its own type rather than a method on
// any one of them -- the same shape as BetPeriodRepository.
//
// It exists because the three tables cannot be paged independently. Taking 100
// rows from each and merging gives the right first page and the wrong second
// one: page 2 wants rows 101-200 of the merged order, which no single table's
// query can see. So the ordering and the slicing happen here, in SQL, across
// all three at once, and the caller then loads only the bets the page names.
type BetPageRepository struct {
	db *gorm.DB
}

// NewBetPageRepository creates a new BetPageRepository.
func NewBetPageRepository(db *gorm.DB) *BetPageRepository {
	return &BetPageRepository{db: db}
}

// betRefs flattens the identifying and filterable columns of the three bet
// tables. The bet's own contents are deliberately absent: this query decides
// which bets a page holds, and nothing else.
//
// The branch order is load-bearing: Postgres takes a UNION's output column
// names from the first branch alone, and only that branch aliases bet_type.
// Promote either of the others to the top and b.bet_type comes back as
// "?column?". Add a branch at the end, or carry the alias with whichever leads.
const betRefs = `
	SELECT id, user_id, game_id, league_id, status, created_at, 'spread' AS bet_type FROM spread_bets
	UNION ALL
	SELECT id, user_id, game_id, league_id, status, created_at, 'moneyline' FROM money_line_bets
	UNION ALL
	SELECT id, user_id, game_id, league_id, status, created_at, 'overunder' FROM over_under_bets`

// refQuery builds the filtered set. CountFiltered and FindRefs share it so the
// two can never disagree about what they are counting, which is what would put
// a pager on the page offering more results than exist.
//
// The joins and predicates mirror the three FindFiltered methods, because they
// have to select the same bets those do -- season and week live on games and
// weeks, not on the bet.
func (r *BetPageRepository) refQuery(filter BetFilter) *gorm.DB {
	query := r.db.
		Table("(?) AS b", r.db.Raw(betRefs)).
		Joins("JOIN games ON games.id = b.game_id").
		Joins("LEFT JOIN weeks ON weeks.id = games.week_id")

	if filter.UserID != nil {
		query = query.Where("b.user_id = ?", *filter.UserID)
	}
	if filter.Season != nil {
		query = query.Where("games.season = ?", *filter.Season)
	}
	if filter.Week != nil {
		query = query.Where("weeks.number = ?", *filter.Week)
	}
	if filter.LeagueID != nil {
		query = query.Where("b.league_id = ?", *filter.LeagueID)
	}
	if filter.Status != nil {
		query = query.Where("b.status = ?", *filter.Status)
	}
	return query
}

// CountFiltered returns how many bets match the filter, across all three
// tables. The pager needs this before the page itself, since a page number
// past the end has to be clamped before it becomes an offset.
func (r *BetPageRepository) CountFiltered(filter BetFilter) (int, error) {
	var total int64
	if err := r.refQuery(filter).Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

// FindRefs returns one page of the filtered bets, newest first.
//
// The order carries b.id as a tiebreak because created_at alone is not a total
// order: two bets placed in the same instant could otherwise sort one way for
// the page 1 query and the other way for page 2, so one bet would appear on
// both pages and another on neither.
func (r *BetPageRepository) FindRefs(filter BetFilter, limit, offset int) ([]BetRef, error) {
	var refs []BetRef
	err := r.refQuery(filter).
		Select("b.id AS bet_id, b.bet_type").
		Order("b.created_at DESC, b.id").
		Limit(limit).
		Offset(offset).
		Scan(&refs).Error
	if err != nil {
		return nil, err
	}
	return refs, nil
}
