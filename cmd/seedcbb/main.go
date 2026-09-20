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
	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/logging"
)

func main() {
	season := flag.Int("season", cbbdata.GetCurrentSeason(), "Basketball season year to seed")
	useFixtures := flag.Bool("fixtures", false,
		"Seed from the captured fixtures in internal/fixtures instead of the live API. No API key, no network, no metered requests.")
	flag.Parse()

	cfg := config.Load()
	logger := logging.Setup(cfg.Env)

	if err := run(cfg, *season, *useFixtures); err != nil {
		logger.Error("basketball seed failed", "error", err)
		os.Exit(1)
	}

	logger.Info("basketball seed completed successfully", "season", *season)
}

// run performs the seed. Keeping the work out of main means deferred cleanup
// still runs when the command fails.
func run(cfg *config.Config, season int, useFixtures bool) error {
	if useFixtures {
		return runFixtures(cfg, season)
	}

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

// runFixtures seeds from the captured responses rather than the live API. See
// the same function in cmd/seed for why this exists.
func runFixtures(cfg *config.Config, season int) error {
	// -season defaults to the current one, which is right for the live API and
	// wrong here: the fixtures cover one captured season.
	if !flagWasSet("season") {
		season = fixtureseed.DefaultSeason
	}
	slog.Info("seeding basketball from fixtures", "season", season)

	db, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	defer database.Close(db)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err = fixtureseed.Basketball(ctx, db, season)
	return err
}

// flagWasSet reports whether a flag was given on the command line, as opposed
// to carrying its default.
func flagWasSet(name string) bool {
	var set bool
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
