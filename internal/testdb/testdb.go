// Package testdb gives tests a real PostgreSQL database.
//
// Repository code is mostly SQL, and the interesting failures are SQL-level: a
// join that drops rows, a predicate that matches nothing, a comparison against
// a column whose stored case is not the one being compared. None of those are
// reachable from a mock, and an in-memory engine would answer a different
// dialect than the one production runs -- which is how a query that never
// matches ends up looking exactly like a query with nothing to match.
//
// Tests run inside a transaction that is always rolled back, so the schema is
// migrated once and no test can leave a row behind for the next one to trip
// over. That matters more than it sounds: the development database holds real
// seeded seasons, and a fixture that escapes into it is both a failing test
// somewhere else and a unique-index collision that outlives the run.
package testdb

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/database"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// URLEnv names the connection string these tests use. It is deliberately not
// DATABASE_URL: every row written here is rolled back, but pointing the suite
// at whatever the shell already had exported is how a test run ends up against
// something that matters.
const URLEnv = "TEST_DATABASE_URL"

var (
	poolOnce sync.Once
	pool     *gorm.DB
	poolErr  error
)

// Open returns a handle scoped to one test: every write through it is undone
// when the test ends.
//
// With no database configured the test skips locally and fails in CI. The skip
// is what keeps `go test ./...` honest on a laptop with nothing running, and
// the CI half is what stops that convenience from quietly becoming permanent --
// a misspelled variable on the runner would otherwise skip the whole suite and
// report it green, which is the failure this package exists to catch.
func Open(t *testing.T) *gorm.DB {
	t.Helper()

	url := os.Getenv(URLEnv)
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is unset: CI must run these tests, not skip them", URLEnv)
		}
		t.Skipf("%s is unset; `make test-db` starts one and sets it", URLEnv)
	}

	poolOnce.Do(func() { pool, poolErr = connect(url) })
	if poolErr != nil {
		t.Fatalf("test database unavailable: %v", poolErr)
	}

	tx := pool.Begin()
	if tx.Error != nil {
		t.Fatalf("beginning test transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil {
			t.Errorf("rolling back test transaction: %v", err)
		}
	})

	return tx
}

// connect migrates and opens the pool shared by every test in the package.
//
// Migrating here rather than expecting a prepared database keeps the schema
// under test equal to the schema the server boots with, and golang-migrate
// takes an advisory lock, so the packages `go test ./...` runs in parallel
// serialize instead of racing each other through the same migration.
func connect(url string) (*gorm.DB, error) {
	if err := database.Migrate(url, migrations.FS); err != nil {
		return nil, fmt.Errorf("migrating the test database: %w", err)
	}

	db, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to the test database: %w", err)
	}

	if err := requireEmpty(db); err != nil {
		return nil, err
	}

	return db, nil
}

// requireEmpty refuses a database that already holds data.
//
// A test's transaction rolls its own writes back but still reads every
// committed row around it, and the queries here are aggregates over a whole
// table -- "is anything being played", "when is the next kickoff". Pointed at a
// development database they would answer from real seeded seasons, so a test
// would pass or fail on rows it never wrote, and the one that passes for the
// wrong reason is the one nobody notices.
//
// This is checked rather than assumed because the alternative was a convention:
// date every fixture far enough into the future to miss the real data. That
// works right up until someone writes a test without knowing about it, and
// nothing is watching. Failing here costs one query per package and turns a
// wrong answer into a sentence explaining itself.
//
// Games are the proxy for "seeded", being what the seed commands mostly write
// and what these queries mostly read.
func requireEmpty(db *gorm.DB) error {
	var games int64
	if err := db.Model(&models.Game{}).Count(&games).Error; err != nil {
		return fmt.Errorf("checking the test database is empty: %w", err)
	}
	if games > 0 {
		return fmt.Errorf(
			"%s points at a database holding %d games, but these tests read across whole tables "+
				"and need an empty one -- `make test-db` creates a database separate from development's",
			URLEnv, games)
	}
	return nil
}
