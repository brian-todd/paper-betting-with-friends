package database

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connection pool limits.
//
// database/sql defaults to an unlimited number of open connections and only two
// idle ones, which is wrong in both directions against a managed Postgres: a
// burst of traffic opens connections until the server hits its own cap and
// starts refusing them outright, while the low idle ceiling means the steady
// state is a constant churn of handshakes.
//
// maxIdleConns matches the open limit so a connection returned to the pool is
// kept rather than closed and immediately reopened; the lifetimes below are
// what stop that from pinning connections forever.
const (
	maxIdleConns = 20

	// connMaxLifetime retires a connection regardless of how busy it is. It
	// matters most behind a connection pooler or a managed database that
	// rotates backends: without it a client can hold a connection to a server
	// that has gone away and only find out mid-query.
	connMaxLifetime = 30 * time.Minute

	// connMaxIdleTime releases connections opened for a burst, so an idle app
	// is not sitting on the database's whole connection budget overnight.
	connMaxIdleTime = 5 * time.Minute
)

// Connect establishes a connection to the PostgreSQL database.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	logLevel := logger.Silent
	if cfg.IsDevelopment() {
		logLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// The open limit is configurable because the right value is a property of
	// the database, not the app: it has to stay under the server's max
	// connections with room left for migrations, psql sessions and any other
	// client, and that ceiling changes with the host and the plan.
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(min(maxIdleConns, cfg.DBMaxOpenConns))
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connMaxIdleTime)

	slog.Info("database connection established", "max_open_conns", cfg.DBMaxOpenConns)
	return db, nil
}

// Close closes the database connection.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}
