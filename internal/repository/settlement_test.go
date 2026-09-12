package repository

import (
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"gorm.io/gorm/schema"
)

// The settlement sweep asks for pending bets in raw SQL, because an EXISTS
// subquery does not go through a model the way the rest of the repository
// does. That leaves the table names written out by hand, and a bet model
// renamed without them would still compile and still run -- the query would
// simply stop matching, every finalized game would look settled, and bets
// would sit pending forever while the job reported success every five minutes.
func TestPendingBetTablesMatchTheBetModels(t *testing.T) {
	var naming schema.NamingStrategy

	want := []string{
		naming.TableName("SpreadBet"),
		naming.TableName("MoneyLineBet"),
		naming.TableName("OverUnderBet"),
	}

	if len(pendingBetTables) != len(want) {
		t.Fatalf("pendingBetTables has %d entries, want %d: %v", len(pendingBetTables), len(want), pendingBetTables)
	}

	for i, table := range want {
		if pendingBetTables[i] != table {
			t.Errorf("pendingBetTables[%d] = %q, want %q", i, pendingBetTables[i], table)
		}
	}
}

// Every bet table has to be represented, or a bet type quietly stops settling.
// The status is bound rather than interpolated; the table name is not, so it
// must never come from anywhere but the constant above.
func TestPendingBetExistsCoversEveryBetTable(t *testing.T) {
	clause, args := pendingBetExists()

	for _, table := range pendingBetTables {
		if !strings.Contains(clause, " FROM "+table+" ") {
			t.Errorf("clause does not select from %q: %s", table, clause)
		}
	}

	if got, want := strings.Count(clause, "?"), len(pendingBetTables); got != want {
		t.Errorf("clause has %d placeholders, want %d: %s", got, want, clause)
	}
	if len(args) != len(pendingBetTables) {
		t.Fatalf("got %d args, want %d", len(args), len(pendingBetTables))
	}
	for i, arg := range args {
		if arg != models.BetStatusPending {
			t.Errorf("args[%d] = %v, want %v", i, arg, models.BetStatusPending)
		}
	}
}
