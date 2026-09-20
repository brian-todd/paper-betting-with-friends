package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// The filter query's failure mode is the quiet one: a join that drops rows or
// a predicate that matches nothing returns an empty slice and no error, which
// is indistinguishable from having nothing to match. testdb already gets this
// far -- what it could not do was supply rows shaped the way CFBD shapes them.
//
// InsertTeam and InsertGame fill in whatever the caller left zero, so the
// conferences, classifications and odds a filter test runs against are
// whatever the test author thought to write down. Here they are a recording:
// 674 real teams across four divisions, 412 real games, real sportsbook lines.
// A predicate that only works against tidy data fails here and nowhere else.
//
// It is deliberately one test rather than one per assertion. Seeding a week
// writes thousands of rows inside a transaction that then rolls the lot back,
// which is correct and slow, so the seeded tests want to be few and shared.
func TestWeekGamesFilterAgainstRecordedRows(t *testing.T) {
	db := testdb.Open(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := fixtureseed.Football(ctx, db, fixtureseed.DefaultYear, fixtureseed.DefaultWeek); err != nil {
		t.Fatalf("seeding from fixtures: %v", err)
	}

	// FindBySeasonNumberAndType, not FindBySeasonAndNumber: a real calendar has
	// a regular week 1 and a postseason week 1, and the untyped lookup ends in
	// First() with no ORDER BY, so it returns whichever Postgres hands back.
	// Hand-built fixtures never have both, which is why nothing noticed.
	weeks := repository.NewWeekRepository(db)
	week, err := weeks.FindBySeasonNumberAndType(
		fixtureseed.DefaultYear, fixtureseed.DefaultWeek, models.SeasonTypeRegular)
	if err != nil {
		t.Fatalf("finding the seeded week: %v", err)
	}

	games := repository.NewGameRepository(db)

	all, total, err := games.FindWeekGames(week.ID, repository.GameFilter{}, 0, 1000)
	if err != nil {
		t.Fatalf("unfiltered: %v", err)
	}
	if total == 0 {
		t.Fatal("the seeded week has no games; every assertion below would pass vacuously")
	}

	t.Run("every division is represented", func(t *testing.T) {
		// The capture rule -- narrow by week, never by division -- exists to
		// keep this true. A fixture set of FBS alone would let a query that
		// mishandles a null classification pass.
		seen := map[string]int{}
		for _, g := range all {
			if g.HomeTeam.Classification != nil {
				seen[*g.HomeTeam.Classification]++
			}
		}
		for _, want := range []string{"fbs", "fcs", "ii", "iii"} {
			if seen[want] == 0 {
				t.Errorf("no %s game survived the unfiltered query (saw %v)", want, seen)
			}
		}
	})

	t.Run("a division filter narrows and does not empty", func(t *testing.T) {
		fbs, n, err := games.FindWeekGames(week.ID, repository.GameFilter{Tiers: []string{"fbs"}}, 0, 1000)
		if err != nil {
			t.Fatalf("filtering by tier: %v", err)
		}
		if n == 0 {
			t.Fatal("filtering to fbs matched nothing, which is what a case mismatch looks like")
		}
		if n >= total {
			t.Errorf("filtering to fbs kept %d of %d games, so the predicate is not narrowing", n, total)
		}
		for _, g := range fbs {
			if !isTier(g.HomeTeam.Classification, "fbs") && !isTier(g.AwayTeam.Classification, "fbs") {
				t.Errorf("game %s has no fbs side but survived the filter", g.ID)
				break
			}
		}
	})

	t.Run("bettable-only keeps the games that have a line", func(t *testing.T) {
		// Real lines, from real books, on the subset of games books price --
		// which is the property that makes this worth asserting. Hand-built
		// odds are always attached to the game the test is about.
		bettable, n, err := games.FindWeekGames(week.ID, repository.GameFilter{BettableOnly: true}, 0, 1000)
		if err != nil {
			t.Fatalf("filtering to bettable: %v", err)
		}
		if n == 0 {
			t.Fatal("no game in the seeded week has odds, so the EXISTS clauses are untested")
		}
		if n >= total {
			t.Errorf("every one of %d games is bettable; the real feed prices a subset", total)
		}
		if len(bettable) == 0 {
			t.Error("the count is non-zero but the page is empty")
		}
	})

	t.Run("an unmatched conference does not widen the clause", func(t *testing.T) {
		// The parenthesising in applyGameFilter exists for exactly this: an OR
		// group left unbracketed alongside another predicate matches far more
		// than it should, and against uniform fixtures it still looks right.
		_, n, err := games.FindWeekGames(week.ID, repository.GameFilter{
			Conferences: []string{"No Such Conference"},
		}, 0, 1000)
		if err != nil {
			t.Fatalf("filtering by conference: %v", err)
		}
		if n != 0 {
			t.Errorf("a conference no team is in matched %d games", n)
		}

		_, n, err = games.FindWeekGames(week.ID, repository.GameFilter{
			Conferences: []string{"No Such Conference"},
			Status:      string(models.GameStatusFinal),
		}, 0, 1000)
		if err != nil {
			t.Fatalf("filtering by conference and status: %v", err)
		}
		if n != 0 {
			t.Errorf("adding a status made an impossible conference match %d games", n)
		}
	})
}

// isTier reports whether a team's classification, which the feed may omit, is
// the one named.
func isTier(classification *string, want string) bool {
	return classification != nil && *classification == want
}
