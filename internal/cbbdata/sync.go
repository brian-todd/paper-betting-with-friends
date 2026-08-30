package cbbdata

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// BetEvaluator is called when a game becomes final to evaluate pending bets.
type BetEvaluator interface {
	EvaluateBetsForGame(gameID uuid.UUID) error
}

// SyncService handles synchronization between the CBB Data API and the database.
type SyncService struct {
	client            *Client
	db                *gorm.DB
	venueRepo         *repository.VenueRepository
	teamRepo          *repository.TeamRepository
	gameRepo          *repository.GameRepository
	gameResultRepo    *repository.GameResultRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	spreadOddsRepo    *repository.SpreadOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository
	betEvaluator      BetEvaluator
	logger            *slog.Logger
}

// NewSyncService creates a new SyncService.
func NewSyncService(client *Client, db *gorm.DB) *SyncService {
	return &SyncService{
		logger:            slog.Default().With("component", "cbb-sync"),
		client:            client,
		db:                db,
		venueRepo:         repository.NewVenueRepository(db),
		teamRepo:          repository.NewTeamRepository(db),
		gameRepo:          repository.NewGameRepository(db),
		gameResultRepo:    repository.NewGameResultRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
	}
}

// SetBetEvaluator sets the bet evaluator for evaluating bets when games complete.
func (s *SyncService) SetBetEvaluator(evaluator BetEvaluator) {
	s.betEvaluator = evaluator
}

// GetCurrentSeason determines the basketball season year.
// Basketball seasons span calendar years (e.g., 2025 season runs Nov 2025 - April 2026).
func GetCurrentSeason() int {
	now := time.Now()
	if now.Month() <= time.June {
		return now.Year() - 1
	}
	return now.Year()
}

// SeedAll performs a full seed of all data for a given season.
// The API caps results at 3000, so we chunk by month to get all games.
func (s *SyncService) SeedAll(ctx context.Context, season int) error {
	s.logger.Info("starting full seed for season", "season", season)

	if err := s.syncVenues(ctx); err != nil {
		return fmt.Errorf("syncing venues: %w", err)
	}

	if err := s.syncTeams(ctx); err != nil {
		return fmt.Errorf("syncing teams: %w", err)
	}

	// Basketball season 2025 = Nov 2024 through Apr 2025.
	// Chunk by month to avoid the API's 3000 result cap.
	startYear := season - 1
	months := []time.Time{
		time.Date(startYear, time.November, 1, 0, 0, 0, 0, time.UTC),
		time.Date(startYear, time.December, 1, 0, 0, 0, 0, time.UTC),
		time.Date(season, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(season, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(season, time.March, 1, 0, 0, 0, 0, time.UTC),
		time.Date(season, time.April, 1, 0, 0, 0, 0, time.UTC),
	}

	for i, start := range months {
		var end time.Time
		if i+1 < len(months) {
			end = months[i+1]
		} else {
			end = time.Date(season, time.May, 1, 0, 0, 0, 0, time.UTC)
		}

		startStr := start.Format("2006-01-02T15:04:05.000Z")
		endStr := end.Format("2006-01-02T15:04:05.000Z")
		s.logger.Info("syncing games", "from", start.Format("Jan 2006"), "to", end.Format("Jan 2006"))

		if err := s.syncGames(ctx, GameQueryOpts{Season: &season, StartDateRange: &startStr, EndDateRange: &endStr}); err != nil {
			return fmt.Errorf("syncing games for %s: %w", start.Format("Jan 2006"), err)
		}

		if err := s.syncLines(ctx, LineQueryOpts{Season: &season, StartDateRange: &startStr, EndDateRange: &endStr}); err != nil {
			return fmt.Errorf("syncing lines for %s: %w", start.Format("Jan 2006"), err)
		}
	}

	s.logger.Info("full seed completed for season", "season", season)
	return nil
}

// SyncGamesAndLines performs an incremental sync of games and lines for a date window.
func (s *SyncService) SyncGamesAndLines(ctx context.Context) error {
	now := time.Now()
	start := now.AddDate(0, 0, -1).Format("2006-01-02T00:00:00.000Z")
	end := now.AddDate(0, 0, 3).Format("2006-01-02T23:59:59.000Z")

	s.logger.Info("starting incremental sync", "from", start, "to", end)

	if err := s.syncGames(ctx, GameQueryOpts{StartDateRange: &start, EndDateRange: &end}); err != nil {
		return fmt.Errorf("syncing games: %w", err)
	}

	if err := s.syncLines(ctx, LineQueryOpts{StartDateRange: &start, EndDateRange: &end}); err != nil {
		return fmt.Errorf("syncing lines: %w", err)
	}

	s.logger.Info("incremental sync completed")
	return nil
}

func (s *SyncService) syncVenues(ctx context.Context) error {
	s.logger.Info("syncing venues")

	venues, err := s.client.GetVenues(ctx)
	if err != nil {
		return err
	}

	for _, v := range venues {
		state := v.State
		if state == "" {
			state = v.Country
		}

		venue := &models.Venue{
			ExternalID: &v.ID,
			Sport:      models.SportBasketball,
			Name:       v.Name,
			City:       v.City,
			State:      state,
			Capacity:   0,
			Dome:       false,
			Grass:      false,
		}

		if err := s.venueRepo.Upsert(venue); err != nil {
			s.logger.Error("failed to upsert venue", "venue", v.Name, "error", err)
		}
	}

	s.logger.Info("synced venues", "synced", len(venues))
	return nil
}

func (s *SyncService) syncTeams(ctx context.Context) error {
	s.logger.Info("syncing teams")

	teams, err := s.client.GetTeams(ctx)
	if err != nil {
		return err
	}

	for _, t := range teams {
		// Resolve venue.
		var venueID uuid.UUID
		if t.CurrentVenueID != nil && *t.CurrentVenueID != 0 {
			dbVenue, err := s.venueRepo.FindByExternalID(*t.CurrentVenueID, models.SportBasketball)
			if err != nil {
				// Create a placeholder venue from team data.
				city := t.CurrentCity
				state := t.CurrentState
				venueName := t.CurrentVenue
				if venueName == "" {
					venueName = t.School + " Arena"
				}
				if city == "" {
					city = "Unknown"
				}
				if state == "" {
					state = "Unknown"
				}
				placeholder := &models.Venue{
					ExternalID: t.CurrentVenueID,
					Sport:      models.SportBasketball,
					Name:       venueName,
					City:       city,
					State:      state,
					Capacity:   0,
				}
				if err := s.venueRepo.Upsert(placeholder); err != nil {
					s.logger.Error("failed to upsert placeholder venue for team", "team", t.School, "error", err)
					continue
				}
				dbVenue, err = s.venueRepo.FindByExternalID(*t.CurrentVenueID, models.SportBasketball)
				if err != nil {
					s.logger.Error("failed to find venue for team after upsert", "team", t.School, "error", err)
					continue
				}
			}
			venueID = dbVenue.ID
		} else {
			// No venue — create a placeholder.
			placeholderExtID := int64(-t.ID) // Negative to avoid conflicts.
			placeholder := &models.Venue{
				ExternalID: &placeholderExtID,
				Sport:      models.SportBasketball,
				Name:       t.School + " Arena",
				City:       "Unknown",
				State:      "Unknown",
				Capacity:   0,
			}
			if err := s.venueRepo.Upsert(placeholder); err != nil {
				s.logger.Error("failed to upsert placeholder venue for team", "team", t.School, "error", err)
				continue
			}
			dbVenue, err := s.venueRepo.FindByExternalID(placeholderExtID, models.SportBasketball)
			if err != nil {
				s.logger.Error("failed to find placeholder venue for team", "team", t.School, "error", err)
				continue
			}
			venueID = dbVenue.ID
		}

		abbr := t.Abbreviation
		if abbr == "" {
			abbr = t.School
		}
		// Truncate abbreviation to 10 chars max.
		if len(abbr) > 10 {
			abbr = abbr[:10]
		}

		conference := t.Conference
		if conference == "" {
			conference = "Independent"
		}

		// Try to get logo from matching football team.
		var logoURL *string
		footballTeam, err := s.teamRepo.FindByNameAndSport(t.School, models.SportFootball)
		if err == nil && footballTeam.LogoURL != nil {
			logoURL = footballTeam.LogoURL
		}

		team := &models.Team{
			ExternalID:     &t.ID,
			Sport:          models.SportBasketball,
			Name:           t.School,
			Abbreviation:   abbr,
			Mascot:         strPtr(t.Mascot),
			Conference:     conference,
			HomeVenueID:    venueID,
			LogoURL:        logoURL,
			PrimaryColor:   formatColor(t.PrimaryColor),
			SecondaryColor: formatColor(t.SecondaryColor),
		}

		if err := s.teamRepo.Upsert(team); err != nil {
			s.logger.Error("failed to upsert team", "team", t.School, "error", err)
		}
	}

	s.logger.Info("synced teams", "synced", len(teams))
	return nil
}

func (s *SyncService) syncGames(ctx context.Context, opts GameQueryOpts) error {
	s.logger.Info("syncing games")

	games, err := s.client.GetGames(ctx, opts)
	if err != nil {
		return err
	}

	syncedCount := 0
	for _, g := range games {
		// Look up home team.
		homeTeam, err := s.teamRepo.FindByExternalID(g.HomeTeamID, models.SportBasketball)
		if err != nil {
			s.logger.Warn("skipping game: home team not found", "game", g.ID, "home_team_id", g.HomeTeamID)
			continue
		}

		// Look up away team.
		awayTeam, err := s.teamRepo.FindByExternalID(g.AwayTeamID, models.SportBasketball)
		if err != nil {
			s.logger.Warn("skipping game: away team not found", "game", g.ID, "away_team_id", g.AwayTeamID)
			continue
		}

		// Resolve venue.
		var venueID uuid.UUID
		if g.VenueID != nil && *g.VenueID != 0 {
			dbVenue, err := s.venueRepo.FindByExternalID(*g.VenueID, models.SportBasketball)
			if err == nil {
				venueID = dbVenue.ID
			} else {
				venueID = homeTeam.HomeVenueID
			}
		} else {
			venueID = homeTeam.HomeVenueID
		}

		// Map status.
		status := mapGameStatus(g.Status)

		game := &models.Game{
			ExternalID:     &g.ID,
			Sport:          models.SportBasketball,
			HomeTeamID:     homeTeam.ID,
			AwayTeamID:     awayTeam.ID,
			VenueID:        venueID,
			WeekID:         nil, // Basketball has no weeks.
			Season:         g.Season,
			SeasonType:     g.SeasonType,
			Tournament:     g.Tournament,
			HomeSeed:       g.HomeSeed,
			AwaySeed:       g.AwaySeed,
			ScheduledAt:    g.StartDate,
			Status:         status,
			NeutralSite:    g.NeutralSite,
			ConferenceGame: g.ConferenceGame,
			Completed:      status == models.GameStatusFinal,
		}

		if err := s.gameRepo.Upsert(game); err != nil {
			s.logger.Error("failed to upsert game", "game", g.ID, "error", err)
			continue
		}

		// Sync the score whenever the provider reports one, including for a
		// game still in progress, so a live card can show it. gameResultFrom
		// leaves FinalizedAt nil until the game is over.
		//
		// Unlike the football feed this API reports a real status rather than
		// one inferred from the clock, and takes status=in_progress as a query
		// filter, so it plausibly carries live points -- unconfirmed, since
		// there are no games in progress out of season.
		if g.HomePoints != nil && g.AwayPoints != nil {
			dbGame, err := s.gameRepo.FindByExternalID(g.ID, models.SportBasketball)
			if err != nil {
				s.logger.Error("failed to find game for result", "error", err)
				continue
			}

			result := gameResultFrom(dbGame.ID, g, status, time.Now())
			if err := s.gameResultRepo.Upsert(result); err != nil {
				s.logger.Error("failed to upsert game result for game", "game", g.ID, "error", err)
			}

			// Evaluate bets only once the game is over. A live score would
			// settle every pending bet against a partial result.
			if status == models.GameStatusFinal && s.betEvaluator != nil {
				if err := s.betEvaluator.EvaluateBetsForGame(dbGame.ID); err != nil {
					s.logger.Error("failed to evaluate bets for game", "game", g.ID, "error", err)
				}
			}
		}

		syncedCount++
	}

	s.logger.Info("synced games", "synced", syncedCount)
	return nil
}

func (s *SyncService) syncLines(ctx context.Context, opts LineQueryOpts) error {
	s.logger.Info("syncing lines")

	lines, err := s.client.GetLines(ctx, opts)
	if err != nil {
		return err
	}

	syncedCount := 0
	for _, l := range lines {
		// Look up game by external ID.
		game, err := s.gameRepo.FindByExternalID(int64(l.GameID), models.SportBasketball)
		if err != nil {
			continue
		}

		for _, line := range l.Lines {
			source := mapProviderToSource(line.Provider)
			if source == "" {
				continue
			}

			// Sync money line odds.
			if line.HomeMoneyline != nil && line.AwayMoneyline != nil {
				mlOdds := &models.MoneyLineOdds{
					GameID:   game.ID,
					Source:   source,
					HomeOdds: decimal.NewFromFloat(*line.HomeMoneyline),
					AwayOdds: decimal.NewFromFloat(*line.AwayMoneyline),
				}
				if err := s.moneyLineOddsRepo.Upsert(mlOdds); err != nil {
					s.logger.Error("failed to upsert money line odds", "error", err)
				}
			}

			// Sync spread odds.
			if line.Spread != nil {
				spread := *line.Spread
				homeSpread := decimal.NewFromFloat(spread)
				awaySpread := decimal.NewFromFloat(-spread)

				spreadOdds := &models.SpreadOdds{
					GameID:     game.ID,
					Source:     source,
					HomeSpread: homeSpread,
					AwaySpread: awaySpread,
					HomeOdds:   decimal.NewFromInt(-110),
					AwayOdds:   decimal.NewFromInt(-110),
				}
				if err := s.spreadOddsRepo.Upsert(spreadOdds); err != nil {
					s.logger.Error("failed to upsert spread odds", "error", err)
				}
			}

			// Sync over/under odds.
			if line.OverUnder != nil {
				ouOdds := &models.OverUnderOdds{
					GameID:    game.ID,
					Source:    source,
					Total:     decimal.NewFromFloat(*line.OverUnder),
					OverOdds:  decimal.NewFromInt(-110),
					UnderOdds: decimal.NewFromInt(-110),
				}
				if err := s.overUnderOddsRepo.Upsert(ouOdds); err != nil {
					s.logger.Error("failed to upsert over/under odds", "error", err)
				}
			}
		}

		syncedCount++
	}

	s.logger.Info("synced lines for games", "for", syncedCount)
	return nil
}

// gameResultFrom builds the score row for a game the provider has reported
// points for.
//
// FinalizedAt is set only for a game the provider calls final. It is the flag
// bet settlement reads, so a game still in progress must leave it nil however
// lopsided its score looks.
func gameResultFrom(gameID uuid.UUID, g APIGame, status models.GameStatus, now time.Time) *models.GameResult {
	var excitementIndex *decimal.Decimal
	if g.Excitement != nil {
		ei := decimal.NewFromFloat(*g.Excitement)
		excitementIndex = &ei
	}

	var finalizedAt *time.Time
	if status == models.GameStatusFinal {
		finalizedAt = &now
	}

	return &models.GameResult{
		GameID:          gameID,
		HomeScore:       *g.HomePoints,
		AwayScore:       *g.AwayPoints,
		HomeLineScores:  models.IntSlice(g.HomePeriodPoints),
		AwayLineScores:  models.IntSlice(g.AwayPeriodPoints),
		ExcitementIndex: excitementIndex,
		FinalizedAt:     finalizedAt,
	}
}

func mapGameStatus(status string) models.GameStatus {
	switch strings.ToLower(status) {
	case "scheduled":
		return models.GameStatusScheduled
	case "in_progress":
		return models.GameStatusInProgress
	case "final":
		return models.GameStatusFinal
	case "postponed":
		return models.GameStatusPostponed
	case "cancelled", "canceled":
		return models.GameStatusCancelled
	default:
		return models.GameStatusScheduled
	}
}

// mapProviderToSource maps API provider names to our OddsSource enum.
func mapProviderToSource(provider string) models.OddsSource {
	switch strings.ToLower(provider) {
	case "draftkings":
		return models.OddsSourceDraftKings
	case "fanduel":
		return models.OddsSourceFanDuel
	case "betmgm":
		return models.OddsSourceBetMGM
	case "caesars":
		return models.OddsSourceCaesars
	case "espn bet", "espn":
		return models.OddsSourceESPN
	case "bovada":
		return models.OddsSourceBovada
	default:
		return ""
	}
}

// strPtr returns a pointer to the string, or nil if empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// formatColor ensures color code is in proper format.
func formatColor(color string) *string {
	if color == "" {
		return nil
	}
	if !strings.HasPrefix(color, "#") {
		color = "#" + color
	}
	if len(color) > 7 {
		color = color[:7]
	}
	return &color
}
