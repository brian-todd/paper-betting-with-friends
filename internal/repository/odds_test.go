package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"gorm.io/gorm/clause"
)

// The odds upserts arbitrate on a partial unique index, and Postgres picks that
// index while planning by proving the index's predicate from the one ON CONFLICT
// names. A bound parameter only survives that proof under a custom plan, where
// it is folded to a constant -- once gorm's prepared statement flips to a
// generic plan on its sixth execution the proof fails and every remaining upsert
// in the sync errors with SQLSTATE 42P10.
func TestNonCustomSourcePredicateInlinesTheLiteral(t *testing.T) {
	exprs := nonCustomSource().Exprs
	if len(exprs) != 1 {
		t.Fatalf("predicate has %d expressions, want 1", len(exprs))
	}

	expr, ok := exprs[0].(clause.Expr)
	if !ok {
		t.Fatalf("predicate is %T, want clause.Expr -- anything that binds the source as a parameter breaks arbiter inference under a generic plan", exprs[0])
	}
	if len(expr.Vars) != 0 {
		t.Fatalf("predicate binds %d variables, want 0: %q", len(expr.Vars), expr.SQL)
	}
	if want := "'" + string(models.OddsSourceCustom) + "'"; !strings.Contains(expr.SQL, want) {
		t.Errorf("predicate %q does not carry the literal %s", expr.SQL, want)
	}
}

// Postgres matches the two predicates by proof, not by text, but the proof only
// holds while they say the same thing about the same value.
func TestNonCustomSourceMatchesTheMigratedIndexes(t *testing.T) {
	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}

	// e.g. ON over_under_odds (game_id, source) WHERE source <> 'custom';
	partial := regexp.MustCompile(`(?is)CREATE UNIQUE INDEX\s+\S*_game_source\s+ON\s+(\w+)\s*\([^)]*\)\s*WHERE\s+([^;]+);`)

	predicates := map[string]string{}
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range partial.FindAllStringSubmatch(string(sql), -1) {
			predicates[m[1]] = strings.Join(strings.Fields(m[2]), " ")
		}
	}

	if len(predicates) != 3 {
		t.Fatalf("found partial (game_id, source) indexes on %d tables, want 3 (money line, spread, over/under): %v", len(predicates), predicates)
	}

	want := "source <> '" + string(models.OddsSourceCustom) + "'"
	for table, got := range predicates {
		if got != want {
			t.Errorf("%s index predicate is %q, want %q -- the upsert names the latter and will find no arbiter", table, got, want)
		}
	}
}
