package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/database"
	"github.com/brian/paper-betting-with-friends/internal/logging"
)

func main() {
	cfg := config.Load()
	logger := logging.Setup(cfg.Env)

	if err := run(cfg); err != nil {
		logger.Error("calendar sync failed", "error", err)
		os.Exit(1)
	}

	logger.Info("calendar sync completed successfully")
}

// run performs the sync. Keeping the work out of main means deferred cleanup
// still runs when the command fails.
func run(cfg *config.Config) error {
	if cfg.CFBDataAPIKey == "" {
		return errors.New("CFB_DATA_API_KEY environment variable is required")
	}

	slog.Info("syncing calendar for all years (2002 - present)")

	// Connect to database.
	db, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	defer database.Close(db)

	// Create API client and sync service.
	client := cfbdata.NewClient(cfg.CFBDataAPIKey)
	syncService := cfbdata.NewSyncService(client, db)

	// Run calendar sync.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	return syncService.SyncAllCalendars(ctx)
}
