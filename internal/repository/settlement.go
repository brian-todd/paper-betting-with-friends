package repository

import (
	"fmt"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// pendingBetTables are the tables a pending bet can be sitting in.
//
// They are spelled out rather than derived because the query below is raw SQL:
// gorm resolves a model to a table name, but an EXISTS subquery does not go
// through a model. TestPendingBetTablesMatchTheBetModels keeps the two in step,
// since renaming a bet model would otherwise leave this query compiling,
// running, and silently finding nothing to settle.
var pendingBetTables = []string{"spread_bets", "money_line_bets", "over_under_bets"}

// SettlementRepository finds the work bet settlement has left to do.
//
// It exists because finalizing a game and settling its bets are no longer the
// same act. Whichever feed sees the game end writes finalized_at and stops
// there -- for football that is now usually the scoreboard, minutes after the
// whistle -- and this is what a periodic sweep asks to find the games that
// leaves owing a payout. Keeping the question in SQL means the sweep costs one
// round trip whether the answer is no games or fifty.
type SettlementRepository struct {
	db *gorm.DB
}

// NewSettlementRepository creates a new SettlementRepository.
func NewSettlementRepository(db *gorm.DB) *SettlementRepository {
	return &SettlementRepository{db: db}
}

// FindGamesAwaitingSettlement returns the games that have a finalized result,
// kicked off no later than kickoffBefore, and still carry at least one pending
// bet.
//
// The order is oldest finalization first, so a backlog drains in the order it
// formed and a run cut short by its timeout still makes progress on whatever
// has been waiting longest.
func (r *SettlementRepository) FindGamesAwaitingSettlement(kickoffBefore time.Time) ([]uuid.UUID, error) {
	pending, args := pendingBetExists()

	var ids []uuid.UUID
	err := r.db.Model(&models.Game{}).
		Joins("JOIN game_results ON game_results.game_id = games.id").
		Where("game_results.finalized_at IS NOT NULL").
		Where("games.scheduled_at <= ?", kickoffBefore).
		Where(pending, args...).
		Order("game_results.finalized_at").
		Pluck("games.id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// pendingBetExists builds the "this game still has a bet waiting on it" clause
// as one EXISTS per bet table, with the placeholder arguments to match.
//
// EXISTS rather than a join because the three tables are independent and a
// join across them would multiply rows; and because it stops at the first
// pending bet it finds, which for a game that has already been settled is the
// common case of finding none at all.
func pendingBetExists() (string, []any) {
	clauses := make([]string, 0, len(pendingBetTables))
	args := make([]any, 0, len(pendingBetTables))

	for _, table := range pendingBetTables {
		// The table name is a package constant, never anything a caller
		// supplies; only the status is a bound parameter.
		clauses = append(clauses, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.game_id = games.id AND %s.status = ?)", table, table, table))
		args = append(args, models.BetStatusPending)
	}

	return strings.Join(clauses, " OR "), args
}
