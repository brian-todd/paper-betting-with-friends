package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/database"
	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/logging"
)

func main() {
	year := flag.Int("year", time.Now().Year(), "Season year to seed")
	week := flag.Int("week", 0, "Specific week to seed (0 = all weeks)")
	seasonType := flag.String("seasonType", "", "Season type: regular or postseason (empty = both)")
	useFixtures := flag.Bool("fixtures", false,
		"Seed from the captured fixtures in internal/fixtures instead of the live API. No API key, no network, no metered requests.")
	flag.Parse()

	cfg := config.Load()
	logger := logging.Setup(cfg.Env)

	if err := run(cfg, *year, *week, *seasonType, *useFixtures); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}

	logger.Info("seed completed successfully")
}

// run performs the seed. Keeping the work out of main means deferred cleanup
// still runs when the command fails.
func run(cfg *config.Config, year, week int, seasonType string, useFixtures bool) error {
	if useFixtures {
		return runFixtures(cfg, year, week, seasonType)
	}

	if cfg.CFBDataAPIKey == "" {
		return errors.New("CFB_DATA_API_KEY environment variable is required")
	}

	// Convert week to pointer (nil if 0 means all weeks).
	var weekPtr *int
	if week > 0 {
		weekPtr = &week
	}

	// Convert seasonType to pointer (nil if empty means all).
	var seasonTypePtr *string
	if seasonType != "" {
		if seasonType != "regular" && seasonType != "postseason" {
			return fmt.Errorf("seasonType must be 'regular' or 'postseason', got %q", seasonType)
		}
		seasonTypePtr = &seasonType
	}

	slog.Info("seeding data", scopeAttrs(year, weekPtr, seasonTypePtr)...)

	// Connect to database.
	db, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	defer database.Close(db)

	// Create API client and sync service.
	client := cfbdata.NewClient(cfg.CFBDataAPIKey)
	syncService := cfbdata.NewSyncService(client, db)

	// Run full seed.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	return syncService.SeedAll(ctx, year, weekPtr, seasonTypePtr)
}

// runFixtures seeds from the captured responses rather than the live API.
//
// A fresh clone has no CFB_DATA_API_KEY, and until this existed it could not
// produce a single game: `make seed` refused without a key, and seedtestdata
// only adds users, leagues and bets on top of games already loaded.
//
// The fake upstream runs in this process on a loopback port, so there is no
// background service to start and no port to configure. Asking for a year or
// week the fixtures do not cover is not silently empty -- the fixture server
// answers with a 500 naming the directory to capture.
func runFixtures(cfg *config.Config, year, week int, seasonType string) error {
	// The captures were taken without a seasonType, because `seed` sends none
	// unless asked, and a query present in one and absent in the other slugs to
	// two different directories. Accepting the flag and ignoring it would be
	// the quiet kind of wrong this whole package exists to avoid.
	if seasonType != "" {
		return fmt.Errorf("-seasonType=%s cannot be combined with -fixtures: "+
			"the captures were taken without one, and adding it would look for "+
			"fixtures that were never recorded", seasonType)
	}

	// -year defaults to the current year, which is right for the live API and
	// wrong here: the fixtures cover one captured season, so the default would
	// start failing the January after they were taken. An explicit -year is
	// still honoured, and asking for a season with no fixtures fails loudly.
	if !flagWasSet("year") {
		year = fixtureseed.DefaultYear
	}
	if week == 0 {
		week = fixtureseed.DefaultWeek
	}
	slog.Info("seeding from fixtures", "year", year, "week", week)

	db, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	defer database.Close(db)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err = fixtureseed.Football(ctx, db, year, week)
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

// scopeAttrs returns structured attributes describing the seed's scope.
// A nil week or seasonType means "all".
func scopeAttrs(year int, week *int, seasonType *string) []any {
	attrs := []any{"year", year}
	if week != nil {
		attrs = append(attrs, "week", *week)
	}
	if seasonType != nil {
		attrs = append(attrs, "season_type", *seasonType)
	}
	return attrs
}
