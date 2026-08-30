package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/database"
	"github.com/brian/paper-betting-with-friends/internal/logging"
)

func main() {
	season := flag.Int("season", cbbdata.GetCurrentSeason(), "Basketball season year to seed")
	flag.Parse()

	cfg := config.Load()
	logger := logging.Setup(cfg.Env)

	if err := run(cfg, *season); err != nil {
		logger.Error("basketball seed failed", "error", err)
		os.Exit(1)
	}

	logger.Info("basketball seed completed successfully", "season", *season)
}

// run performs the seed. Keeping the work out of main means deferred cleanup
// still runs when the command fails.
func run(cfg *config.Config, season int) error {
	if cfg.CBBDataAPIKey == "" {
		return errors.New("CBB_DATA_API_KEY environment variable is required")
	}

	slog.Info("seeding basketball data", "season", season)

	// Connect to database.
	db, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	defer database.Close(db)

	// Create API client and sync service.
	client := cbbdata.NewClient(cfg.CBBDataAPIKey)
	syncService := cbbdata.NewSyncService(client, db)

	// Run full seed.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	return syncService.SeedAll(ctx, season)
}
