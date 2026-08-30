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
	"github.com/brian/paper-betting-with-friends/internal/logging"
)

func main() {
	year := flag.Int("year", time.Now().Year(), "Season year to seed")
	week := flag.Int("week", 0, "Specific week to seed (0 = all weeks)")
	seasonType := flag.String("seasonType", "", "Season type: regular or postseason (empty = both)")
	flag.Parse()

	cfg := config.Load()
	logger := logging.Setup(cfg.Env)

	if err := run(cfg, *year, *week, *seasonType); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}

	logger.Info("seed completed successfully")
}

// run performs the seed. Keeping the work out of main means deferred cleanup
// still runs when the command fails.
func run(cfg *config.Config, year, week int, seasonType string) error {
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
