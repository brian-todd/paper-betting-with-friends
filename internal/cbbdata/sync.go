package cbbdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/syncerr"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
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

	// clock is the incremental sync's date window and every result's fetchedAt.
	// Not GetCurrentSeason, which deliberately keeps the real clock -- see its
	// own comment. See cfbdata.SyncService for why this is a settable field.
	clock timeutil.Clock
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

// SetClock overrides the time source. The zero value is time.Now, so only a
// test replaying a recorded response needs to call this.
func (s *SyncService) SetClock(now func() time.Time) {
	s.clock.Set(now)
}

// GetCurrentSeason determines the basketball season year.
//
// This one keeps the real clock rather than taking a SyncService's: both callers
// are a cmd/ flag default resolved before any service exists. SeasonFor is the
// testable half, in the shape the other pure helpers in these packages use --
// now as a parameter, the call site deciding where it comes from.
func GetCurrentSeason() int {
	return SeasonFor(time.Now())
}

// SeasonFor is the season year CBBD would label an instant's games with.
//
// A basketball season spans two calendar years and is named for the later one:
// season 2026 is November 2025 through April 2026, which is what SeedAll fetches
// and what the feed's own `season` field says on every game in that range --
// verified against the committed captures, where every game from 2025-11-03 to
// 2026-04-07 carries season 2026.
//
// This was wrong by a year for every month of the season until it was tested.
// The docstring above it claimed "2025 season runs Nov 2025 - April 2026",
// contradicting the correct comment in SeedAll thirty lines below, and the
// arithmetic was written against the wrong one: through the whole of November to
// April it named the season that had ended the previous spring. Only the manual
// `cbb-seed` job uses it -- the incremental sync asks for a date window and
// names no season -- so the symptom was a seed triggered without an explicit
// year fetching a season nobody was watching, and reporting success.
//
// Outside the season, July onward resolves to the season about to start, which
// is the one a seed run in the autumn wants.
func SeasonFor(now time.Time) int {
	if now.Month() >= time.July {
		return now.Year() + 1
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

	// A month whose lines would not all save should not cost the seed the five
	// months after it -- the games are still there to fetch. The failure is
	// still the seed's outcome, so the first one is held and returned at the
	// end; anything that is not a partial write is the API or the database
	// being unavailable, and there is no point walking the rest of the season
	// through that.
	var incomplete error

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
			err = fmt.Errorf("syncing lines for %s: %w", start.Format("Jan 2006"), err)
			if !errors.Is(err, syncerr.ErrIncomplete) {
				return err
			}
			if incomplete == nil {
				incomplete = err
			}
		}
	}

	s.logger.Info("full seed completed for season", "season", season)
	return incomplete
}

// SyncGamesAndLines performs an incremental sync of games and lines for a date window.
func (s *SyncService) SyncGamesAndLines(ctx context.Context) error {
	now := s.clock.Now()
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

			result := gameResultFrom(dbGame.ID, g, status, s.clock.Now())
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
	// One line that will not save is not a reason to drop the rest of the
	// slate, but the run has to end up reporting it -- see syncerr.
	var failed syncerr.Tally

	// One warning a run per unrecognised book, not one a quote. A new
	// sportsbook pricing the slate would otherwise bury the log, and this feed
	// syncs a month of basketball at a time.
	unknownBooks := make(map[string]bool)

	for _, l := range lines {
		// Look up game by external ID.
		game, err := s.gameRepo.FindByExternalID(int64(l.GameID), models.SportBasketball)
		if err != nil {
			continue
		}

		for _, line := range l.Lines {
			source, known := mapProviderToSource(line.Provider)
			if !known && !unknownBooks[line.Provider] {
				unknownBooks[line.Provider] = true
				s.logger.Warn("skipping odds from an unrecognised sportsbook", "provider", line.Provider)
			}
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
					s.logger.Error("failed to upsert money line odds", "game", l.GameID, "source", source, "error", err)
					failed.Add(err)
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
					s.logger.Error("failed to upsert spread odds", "game", l.GameID, "source", source, "error", err)
					failed.Add(err)
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
					s.logger.Error("failed to upsert over/under odds", "game", l.GameID, "source", source, "error", err)
					failed.Add(err)
				}
			}
		}

		syncedCount++
	}

	s.logger.Info("synced lines for games", "for", syncedCount, "failed_writes", failed.Count())
	return failed.Err("odds")
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
//
// "draft kings" is accepted here and refused by the football mapping, and the
// asymmetry is the feeds', not ours. CBBD spells it that way and no other:
// across a captured season it is 4,516 of 10,732 quotes, carrying 4,514
// spreads and 1,639 moneylines, and it is the only book quoting 471 games at
// all -- which without this case have no odds and read as unbettable.
//
// CFBD sends both spellings, and there they disagree: 43 of 278 shared games
// on the spread, 33 on the total. Since both would map here to one source and
// the odds tables are keyed on (game_id, source), cfbdata drops the spaced one
// rather than store whichever the feed listed last. CBBD sends one spelling,
// so it has no such choice to make. Changing either to match the other loses
// real data.
//
// The second return distinguishes a provider we decline to store from one we
// have never seen, so a new sportsbook is not silently discarded.
func mapProviderToSource(provider string) (source models.OddsSource, known bool) {
	switch strings.ToLower(provider) {
	case "draftkings", "draft kings":
		return models.OddsSourceDraftKings, true
	case "fanduel":
		return models.OddsSourceFanDuel, true
	case "betmgm":
		return models.OddsSourceBetMGM, true
	case "caesars":
		return models.OddsSourceCaesars, true
	case "espn bet", "espn":
		return models.OddsSourceESPN, true
	case "bovada":
		return models.OddsSourceBovada, true
	default:
		return "", false
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
