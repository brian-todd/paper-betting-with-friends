// Package fixtureseed populates a database from captured API responses.
//
// It is the same sync code the server runs, pointed at a fake upstream rather
// than at a metered one. Two callers want that:
//
//   - `seed -fixtures`, so a fresh clone can produce a populated database with
//     no API key. Until now `make seed` needed one and `seedtestdata` only
//     added users and bets on top of games already loaded, so a clone without
//     a key could not produce a single game.
//   - Any test wanting rows the feed really sent. `internal/testdb` hands out
//     a transaction; pass it here and the rows arrive shaped the way CFBD
//     shapes them -- nulls where it sends nulls, a classification in the case
//     it uses -- rather than the way whoever wrote InsertGame imagined.
//
// # Football seeds into a transaction; basketball does not
//
// Football is safe to run against a `testdb` transaction. Basketball is not,
// and the reason is not the fixtures: `cbbdata.syncTeams` meets a duplicate
// team abbreviation in the real feed, logs it and continues. On a connection
// each statement autocommits, so one bad row is skipped and the season lands.
// Inside a transaction the first error poisons every statement after it, and
// the seed collapses into a cascade of "current transaction is aborted".
//
// So Basketball is for `seedcbb -fixtures` against a connection. A test
// wanting basketball rows needs that constraint lifted first, which means
// savepoints around the writes the sync deliberately tolerates.
//
// # Zero is a failure, not a result
//
// Every seed here ends by counting what it wrote and refusing a zero. That is
// not belt and braces: `syncGames` skips a game whose home or away team it
// cannot find, logs a warning and returns nil, so a fixture set missing
// /teams seeds no games at all and exits 0. An incomplete fixture set is the
// most likely thing to go wrong with this package and its natural symptom is
// silent success, which is the one symptom the whole testing effort exists to
// refuse.
package fixtureseed

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
)

// DefaultYear and DefaultWeek are what the committed football fixtures cover.
// Asking for anything else is not silently wrong -- the fixture server answers
// an uncaptured week with a 500 naming the directory to capture.
const (
	DefaultYear   = 2026
	DefaultWeek   = 1
	DefaultSeason = 2026
)

// A Count is one table's row count after a seed.
type Count struct {
	Table string
	Rows  int64
}

// Football seeds venues, teams, the calendar, and one week of games, rankings
// and lines, from the embedded fixtures.
func Football(ctx context.Context, db *gorm.DB, year, week int) ([]Count, error) {
	base, _, stop, err := fixtureserver.Listen(fixtures.CFBD)
	if err != nil {
		return nil, err
	}
	defer stop()

	// No API key. The fake upstream does not look at the header, and a seed
	// that cannot reach the network is the entire point.
	client := cfbdata.NewClientAt(base, "")
	sync := cfbdata.NewSyncService(client, db)

	if err := sync.SeedAll(ctx, year, &week, nil); err != nil {
		return nil, fmt.Errorf("seeding football from fixtures: %w", err)
	}

	return count(db, []table{
		{"venues", &models.Venue{}, sport(models.SportFootball)},
		{"teams", &models.Team{}, sport(models.SportFootball)},
		{"weeks", &models.Week{}, nil},
		{"games", &models.Game{}, sport(models.SportFootball)},
		{"rankings", &models.TeamRanking{}, nil},
	})
}

// Basketball seeds venues, teams, games and lines for a season.
func Basketball(ctx context.Context, db *gorm.DB, season int) ([]Count, error) {
	base, _, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		return nil, err
	}
	defer stop()

	client := cbbdata.NewClientAt(base, "")
	sync := cbbdata.NewSyncService(client, db)

	if err := sync.SeedAll(ctx, season); err != nil {
		return nil, fmt.Errorf("seeding basketball from fixtures: %w", err)
	}

	return count(db, []table{
		{"venues", &models.Venue{}, sport(models.SportBasketball)},
		{"teams", &models.Team{}, sport(models.SportBasketball)},
		{"games", &models.Game{}, sport(models.SportBasketball)},
	})
}

// A table is one row count to take after a seed.
type table struct {
	name  string
	model any
	// scope narrows the count. Venues, teams and games are shared between the
	// two sports, so an unscoped count would let football's rows satisfy the
	// non-zero check on a basketball seed that wrote nothing.
	scope func(*gorm.DB) *gorm.DB
}

func sport(name string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB { return db.Where("sport = ?", name) }
}

// count takes each row count and refuses a zero.
func count(db *gorm.DB, tables []table) ([]Count, error) {
	counts := make([]Count, 0, len(tables))
	for _, t := range tables {
		q := db.Model(t.model)
		if t.scope != nil {
			q = t.scope(q)
		}
		var rows int64
		if err := q.Count(&rows).Error; err != nil {
			return nil, fmt.Errorf("counting %s: %w", t.name, err)
		}
		counts = append(counts, Count{Table: t.name, Rows: rows})
		slog.Info("seeded from fixtures", "table", t.name, "rows", rows)
	}

	for _, c := range counts {
		if c.Rows == 0 {
			return counts, fmt.Errorf(
				"seed wrote no %s: the fixture set is incomplete, and a sync over "+
					"missing fixtures reports success while writing nothing", c.Table)
		}
	}
	return counts, nil
}
