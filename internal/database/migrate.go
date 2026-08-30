package database

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	// The postgres database driver, registered for the postgres:// and
	// postgresql:// schemes that hosted Postgres add-ons hand out.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migrate applies every pending migration in source.
//
// It opens its own short-lived connection from databaseURL rather than reusing
// the pool from Connect. golang-migrate's Close closes the *sql.DB it was given,
// so handing it the application's pool would tear down the connection the
// server is about to run on.
//
// Concurrent callers are safe: the postgres driver takes a pg_advisory_lock for
// the duration of the run, so a second instance booting at the same time waits
// and then finds nothing left to do.
func Migrate(databaseURL string, source fs.FS) error {
	sourceDriver, err := iofs.New(source, ".")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, databaseURL)
	if err != nil {
		return fmt.Errorf("preparing migrations: %w", err)
	}
	defer func() {
		// The source error is not worth reporting -- it is a closed embed.FS --
		// but failing to release the connection is.
		if _, dbErr := m.Close(); dbErr != nil {
			slog.Error("failed to close migration connection", "error", dbErr)
		}
	}()

	version, dirty, err := m.Version()
	switch {
	case errors.Is(err, migrate.ErrNilVersion):
		slog.Info("no migrations applied yet")
	case err != nil:
		return fmt.Errorf("reading schema version: %w", err)
	case dirty:
		// A previous run failed part way. Continuing would apply the next
		// migration on top of a half-applied one, so refuse and say which
		// version needs looking at.
		return fmt.Errorf("schema version %d is dirty: a previous migration failed and needs to be resolved by hand", version)
	default:
		slog.Info("schema version", "version", version)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("schema is up to date")
			return nil
		}
		return fmt.Errorf("applying migrations: %w", err)
	}

	newVersion, _, err := m.Version()
	if err != nil {
		return fmt.Errorf("reading schema version after migrating: %w", err)
	}
	slog.Info("migrations applied", "version", newVersion)

	return nil
}
