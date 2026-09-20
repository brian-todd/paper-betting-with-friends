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
// # Both sports seed into a transaction, since migration 000023
//
// Basketball did not, and the reason was not the fixtures. `cbbdata.syncTeams`
// truncates the feed's abbreviation to ten characters, 79 of the 1,519
// basketball abbreviations then collide, and the teams table carried a unique
// index on (abbreviation, sport) that Upsert does not arbitrate on -- so each
// collision was a unique violation the sync logged and continued past. On a
// connection each statement autocommits, so 107 teams were silently dropped and
// the season landed anyway. Inside a transaction the first violation poisons
// every statement after it and the seed collapsed into a cascade of "current
// transaction is aborted".
//
// Dropping the unique index fixed both halves: no violation to poison the
// transaction, and the 107 teams -- plus the 49 games that had been skipped as
// "team not found" -- are written. A basketball seed is the most expensive test
// in the suite at roughly 24 seconds for a whole season, so it wants to stay one
// test rather than one per assertion.
//
// # Two checks, because neither covers the other
//
// `syncGames` skips a game whose home or away team it cannot find, logs a
// warning and returns nil, so a fixture set missing /teams seeds nothing and
// exits 0. Silent success is the natural symptom of an incomplete fixture set
// and the one symptom this whole effort exists to refuse, so every seed here
// ends by checking two things.
//
// **No request went unanswered.** The fake upstream counts the requests it had
// no fixture for, and a seed refuses to succeed with any. This holds whatever
// was already in the database, which is what makes it the stronger of the two.
//
// **The tables are not empty.** This catches a fixture that is present but
// hollow -- a /teams capture of `[]` fetches fine and writes nothing. Its
// limit is worth knowing: it counts rows that are *there*, not rows this run
// wrote, so against a database that already holds a season it cannot fail. It
// is a real check on a fresh clone, which is the case it was added for, and
// informational elsewhere.
package fixtureseed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
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

// An Option adjusts a seed. There is one, and it exists because a fixture
// replayed against a real clock is only half a recording: the bodies are what
// the feed sent, but the status every unplayed game is filed under, and the
// instant every score is stamped with, come from whenever the seed happened to
// run. `seed -fixtures` wants that -- a developer's database should look like
// today -- and a test asserting on either wants to say which day it is.
type Option func(*options)

type options struct {
	now func() time.Time
}

// At replays a seed as though it were running at a chosen instant.
//
// The instants to choose from are in the fixture file names, which is the whole
// reason they are the file names: fixtures.Set.Sequence reports the capture
// instant of every response a route will serve.
func At(now time.Time) Option {
	return func(o *options) { o.now = timeutil.Fixed(now) }
}

func resolve(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Football seeds venues, teams, the calendar, and one week of games, rankings
// and lines, from the embedded fixtures.
func Football(ctx context.Context, db *gorm.DB, year, week int, opts ...Option) ([]Count, error) {
	if err := requireSchema(db); err != nil {
		return nil, err
	}

	base, fake, stop, err := fixtureserver.Listen(fixtures.CFBD)
	if err != nil {
		return nil, err
	}
	defer stop()

	// No API key. The fake upstream does not look at the header, and a seed
	// that cannot reach the network is the entire point.
	client := cfbdata.NewClientAt(base, "")
	sync := cfbdata.NewSyncService(client, db)
	sync.SetClock(resolve(opts).now)

	if err := sync.SeedAll(ctx, year, &week, nil); err != nil {
		return nil, fmt.Errorf("seeding football from fixtures: %w", err)
	}
	if err := requireNoRefusals(fake); err != nil {
		return nil, err
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
func Basketball(ctx context.Context, db *gorm.DB, season int, opts ...Option) ([]Count, error) {
	if err := requireSchema(db); err != nil {
		return nil, err
	}

	base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		return nil, err
	}
	defer stop()

	client := cbbdata.NewClientAt(base, "")
	sync := cbbdata.NewSyncService(client, db)
	sync.SetClock(resolve(opts).now)

	if err := sync.SeedAll(ctx, season); err != nil {
		return nil, fmt.Errorf("seeding basketball from fixtures: %w", err)
	}
	if err := requireNoRefusals(fake); err != nil {
		return nil, err
	}

	return count(db, []table{
		{"venues", &models.Venue{}, sport(models.SportBasketball)},
		{"teams", &models.Team{}, sport(models.SportBasketball)},
		{"games", &models.Game{}, sport(models.SportBasketball)},
	})
}

// requireSchema refuses to start against an unmigrated database.
//
// Without it the seed walks the whole fixture set writing nothing, because
// every upsert is logged and continued past, and the first eight hundred lines
// of output are "relation \"venues\" does not exist" before anything says what
// to do about it.
func requireSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Game{}) {
		return errors.New("the database has no schema: run `make migrate-up` first")
	}
	return nil
}

// requireNoRefusals fails a seed in which the fake upstream had no fixture for
// something the sync asked for.
//
// A sync that tolerates a failed fetch -- logging it and carrying on -- would
// otherwise leave the gap invisible, and the fixture set is exactly the thing
// most likely to be missing a path.
func requireNoRefusals(fake *fixtureserver.Server) error {
	if n := fake.Refusals(); n > 0 {
		return fmt.Errorf("the fixture set has no answer for %d of the %d requests this seed made; "+
			"the server logged which paths, and `scripts/capture.sh` records them", n, len(fake.Requests()))
	}
	return nil
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
