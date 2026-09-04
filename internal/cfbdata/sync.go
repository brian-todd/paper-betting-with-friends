package cfbdata

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
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

// SyncService handles synchronization between the CFB Data API and the database.
type SyncService struct {
	client            *Client
	db                *gorm.DB
	venueRepo         *repository.VenueRepository
	teamRepo          *repository.TeamRepository
	weekRepo          *repository.WeekRepository
	gameRepo          *repository.GameRepository
	gameResultRepo    *repository.GameResultRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	spreadOddsRepo    *repository.SpreadOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository
	rankingRepo       *repository.RankingRepository
	betEvaluator      BetEvaluator
	logger            *slog.Logger
}

// NewSyncService creates a new SyncService.
func NewSyncService(client *Client, db *gorm.DB) *SyncService {
	return &SyncService{
		logger:            slog.Default().With("component", "cfb-sync"),
		client:            client,
		db:                db,
		venueRepo:         repository.NewVenueRepository(db),
		teamRepo:          repository.NewTeamRepository(db),
		weekRepo:          repository.NewWeekRepository(db),
		gameRepo:          repository.NewGameRepository(db),
		gameResultRepo:    repository.NewGameResultRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
		rankingRepo:       repository.NewRankingRepository(db),
	}
}

// SetBetEvaluator sets the bet evaluator for evaluating bets when games complete.
func (s *SyncService) SetBetEvaluator(evaluator BetEvaluator) {
	s.betEvaluator = evaluator
}

// GetCurrentSeasonYear determines the correct season year for syncing based on calendar data.
// CFB seasons span calendar years (e.g., 2025 season runs Aug 2025 - Jan 2026).
// Falls back to the current calendar year if no matching season is found.
func (s *SyncService) GetCurrentSeasonYear() int {
	now := time.Now()
	season, err := s.weekRepo.FindSeasonContainingDate(now)
	if err == nil && season > 0 {
		return season
	}
	// Fallback to current calendar year.
	return now.Year()
}

// SeedAll performs a full seed of all data for a given year, optionally filtered by week and season type.
func (s *SyncService) SeedAll(ctx context.Context, year int, week *int, seasonType *string) error {
	s.logger.Info("starting full seed", syncScope(year, week, seasonType)...)

	// Sync in dependency order.
	if err := s.syncVenues(ctx); err != nil {
		return fmt.Errorf("syncing venues: %w", err)
	}

	if err := s.syncTeams(ctx); err != nil {
		return fmt.Errorf("syncing teams: %w", err)
	}

	if err := s.syncCalendar(ctx, year); err != nil {
		return fmt.Errorf("syncing calendar: %w", err)
	}

	if err := s.syncGames(ctx, year, week, seasonType); err != nil {
		return fmt.Errorf("syncing games: %w", err)
	}

	if err := s.syncRankings(ctx, year, week, seasonType); err != nil {
		return fmt.Errorf("syncing rankings: %w", err)
	}

	if err := s.syncLines(ctx, year, week, seasonType); err != nil {
		return fmt.Errorf("syncing lines: %w", err)
	}

	s.logger.Info("full seed completed", syncScope(year, week, seasonType)...)
	return nil
}

// SyncGamesAndLines performs an incremental sync of games and lines.
func (s *SyncService) SyncGamesAndLines(ctx context.Context, year int, week *int, seasonType *string) error {
	s.logger.Info("starting incremental sync", syncScope(year, week, seasonType)...)

	if err := s.syncGames(ctx, year, week, seasonType); err != nil {
		return fmt.Errorf("syncing games: %w", err)
	}

	if err := s.syncLines(ctx, year, week, seasonType); err != nil {
		return fmt.Errorf("syncing lines: %w", err)
	}

	s.logger.Info("incremental sync completed", syncScope(year, week, seasonType)...)
	return nil
}

// SyncAllCalendars syncs calendar data for all years from 2002 until the API returns empty.
func (s *SyncService) SyncAllCalendars(ctx context.Context) error {
	s.logger.Info("starting calendar sync for all years")

	const startYear = 2002
	syncedYears := 0

	for year := startYear; ; year++ {
		weeks, err := s.client.GetCalendar(ctx, year)
		if err != nil {
			return fmt.Errorf("fetching calendar for year %d: %w", year, err)
		}

		// Stop when we get an empty response.
		if len(weeks) == 0 {
			break
		}

		for _, w := range weeks {
			seasonType := models.SeasonTypeRegular
			if w.SeasonType == "postseason" {
				seasonType = models.SeasonTypePostseason
			}

			week := &models.Week{
				Season:     w.Season,
				Number:     w.Week,
				SeasonType: seasonType,
				StartDate:  w.StartDate,
				EndDate:    w.EndDate,
			}

			if err := s.weekRepo.Upsert(week); err != nil {
				s.logger.Error("failed to upsert week", "season", w.Season, "week", w.Week, "error", err)
			}
		}

		syncedYears++
	}

	s.logger.Info("calendar sync completed", "years_synced", syncedYears)
	return nil
}

func (s *SyncService) syncVenues(ctx context.Context) error {
	s.logger.Info("syncing venues")

	venues, err := s.client.GetVenues(ctx)
	if err != nil {
		return err
	}

	for _, v := range venues {
		venue := &models.Venue{
			ExternalID: &v.ID,
			Sport:      models.SportFootball,
			Name:       v.Name,
			City:       v.City,
			State:      v.State,
			Capacity:   v.Capacity,
			Timezone:   strPtr(v.Timezone),
			Dome:       v.Dome,
			Grass:      v.Grass,
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
		// Find or create home venue from team location.
		var venueID *int64
		if t.Location != nil && t.Location.ID != 0 {
			venueID = &t.Location.ID
			// Ensure venue exists.
			venue := &models.Venue{
				ExternalID: venueID,
				Sport:      models.SportFootball,
				Name:       t.Location.Name,
				City:       t.Location.City,
				State:      t.Location.State,
				Capacity:   t.Location.Capacity,
				Timezone:   strPtr(t.Location.Timezone),
				Dome:       t.Location.Dome,
				Grass:      t.Location.Grass,
			}
			if err := s.venueRepo.Upsert(venue); err != nil {
				s.logger.Error("failed to upsert venue for team", "team", t.School, "error", err)
				continue
			}
		}

		// Skip teams without a venue.
		if venueID == nil {
			s.logger.Warn("skipping team: no venue", "team", t.School)
			continue
		}

		// Look up venue by external ID.
		dbVenue, err := s.venueRepo.FindByExternalID(*venueID, models.SportFootball)
		if err != nil {
			s.logger.Error("failed to find venue for team", "team", t.School, "error", err)
			continue
		}

		// Get first logo if available.
		var logoURL *string
		if len(t.Logos) > 0 {
			logoURL = &t.Logos[0]
		}

		team := &models.Team{
			ExternalID:     &t.ID,
			Sport:          models.SportFootball,
			Name:           t.School,
			Abbreviation:   t.Abbreviation,
			Mascot:         strPtr(t.Mascot),
			Conference:     t.Conference,
			Classification: strPtr(t.Classification),
			HomeVenueID:    dbVenue.ID,
			LogoURL:        logoURL,
			PrimaryColor:   formatColor(t.Color),
			SecondaryColor: formatColor(t.AlternateColor),
		}

		if err := s.teamRepo.Upsert(team); err != nil {
			s.logger.Error("failed to upsert team", "team", t.School, "error", err)
		}
	}

	s.logger.Info("synced teams", "synced", len(teams))
	return nil
}

func (s *SyncService) syncCalendar(ctx context.Context, year int) error {
	s.logger.Info("syncing calendar for year", "year", year)

	weeks, err := s.client.GetCalendar(ctx, year)
	if err != nil {
		return err
	}

	for _, w := range weeks {
		seasonType := models.SeasonTypeRegular
		if w.SeasonType == "postseason" {
			seasonType = models.SeasonTypePostseason
		}

		week := &models.Week{
			Season:     w.Season,
			Number:     w.Week,
			SeasonType: seasonType,
			StartDate:  w.StartDate,
			EndDate:    w.EndDate,
		}

		if err := s.weekRepo.Upsert(week); err != nil {
			s.logger.Error("failed to upsert week", "season", w.Season, "week", w.Week, "error", err)
		}
	}

	s.logger.Info("synced weeks", "synced", len(weeks))
	return nil
}

func (s *SyncService) syncGames(ctx context.Context, year int, week *int, seasonType *string) error {
	s.logger.Info("syncing games", syncScope(year, week, seasonType)...)

	games, err := s.client.GetGames(ctx, year, week, seasonType)
	if err != nil {
		return err
	}

	syncedCount := 0
	for _, g := range games {
		// Look up home team.
		homeTeam, err := s.teamRepo.FindByExternalID(g.HomeID, models.SportFootball)
		if err != nil {
			s.logger.Warn("skipping game: home team not found", "game", g.ID, "home_team_id", g.HomeID)
			continue
		}

		// Look up away team.
		awayTeam, err := s.teamRepo.FindByExternalID(g.AwayID, models.SportFootball)
		if err != nil {
			s.logger.Warn("skipping game: away team not found", "game", g.ID, "away_team_id", g.AwayID)
			continue
		}

		// Look up venue.
		venue, err := s.venueRepo.FindByExternalID(g.VenueID, models.SportFootball)
		if err != nil {
			// Use home team's venue as fallback.
			venue = &homeTeam.HomeVenue
		}

		// Determine season type for week lookup.
		gameSeasonType := models.SeasonTypeRegular
		if g.SeasonType == "postseason" {
			gameSeasonType = models.SeasonTypePostseason
		}

		// Look up week.
		week, err := s.weekRepo.FindBySeasonNumberAndType(g.Season, g.Week, gameSeasonType)
		if err != nil {
			s.logger.Warn("skipping game: week not found", "game", g.ID, "season", g.Season, "week", g.Week, "season_type", g.SeasonType)
			continue
		}

		// Determine game status.
		status := models.GameStatusScheduled
		if g.Completed {
			status = models.GameStatusFinal
		} else if time.Now().After(g.StartDate.Add(5 * time.Minute)) {
			status = models.GameStatusInProgress
		}

		game := &models.Game{
			ExternalID:     &g.ID,
			Sport:          models.SportFootball,
			HomeTeamID:     homeTeam.ID,
			AwayTeamID:     awayTeam.ID,
			VenueID:        venue.ID,
			WeekID:         &week.ID,
			Season:         g.Season,
			SeasonType:     g.SeasonType,
			ScheduledAt:    g.StartDate,
			Status:         status,
			NeutralSite:    g.NeutralSite,
			ConferenceGame: g.ConferenceGame,
			Completed:      g.Completed,
		}

		if err := s.gameRepo.Upsert(game); err != nil {
			s.logger.Error("failed to upsert game", "game", g.ID, "error", err)
			continue
		}

		// Sync the score whenever the provider reports one, rather than only
		// for a completed game.
		//
		// In practice this endpoint returns null points until `completed`
		// flips true, so the live branch does not fire today -- live scores
		// live on /scoreboard, which is FBS-only and not synced. The condition
		// stays this shape because it is the honest one: write what the
		// provider gives us, and let FinalizedAt say whether it is final.
		if g.HomePoints != nil && g.AwayPoints != nil {
			// Get the game from DB to get its UUID.
			dbGame, err := s.gameRepo.FindByExternalID(g.ID, models.SportFootball)
			if err != nil {
				s.logger.Error("failed to find game for result", "error", err)
				continue
			}

			result := gameResultFrom(dbGame.ID, g, time.Now())
			if err := s.gameResultRepo.Upsert(result); err != nil {
				s.logger.Error("failed to upsert game result for game", "game", g.ID, "error", err)
			}

			// Evaluate bets only once the game is over. A live score would
			// settle every pending bet against a partial result.
			if g.Completed && s.betEvaluator != nil {
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

// SyncRankings performs an incremental sync of poll rankings.
func (s *SyncService) SyncRankings(ctx context.Context, year int, week *int, seasonType *string) error {
	return s.syncRankings(ctx, year, week, seasonType)
}

func (s *SyncService) syncRankings(ctx context.Context, year int, week *int, seasonType *string) error {
	s.logger.Info("syncing rankings", syncScope(year, week, seasonType)...)

	weeks, err := s.client.GetRankings(ctx, year, week, seasonType)
	if err != nil {
		return err
	}

	syncedCount := 0
	for _, w := range weeks {
		rankingSeasonType := models.SeasonTypeRegular
		if w.SeasonType == "postseason" {
			rankingSeasonType = models.SeasonTypePostseason
		}

		dbWeek, err := s.weekRepo.FindBySeasonNumberAndType(w.Season, w.Week, rankingSeasonType)
		if err != nil {
			s.logger.Warn("skipping rankings: week not found", "season", w.Season, "week", w.Week, "season_type", w.SeasonType)
			continue
		}

		for _, poll := range w.Polls {
			rankings := s.rankingsFromPoll(dbWeek.ID, poll)

			// Nothing to write means no data, never "nobody is ranked": a
			// published poll always names 25 teams, so an empty one is a
			// truncated response or a resolve that failed across the board.
			// Replacing on it would delete a good week's rankings, and since
			// the periodic job re-syncs the whole season every run, one bad
			// response would clear every week while still logging success.
			if len(rankings) == 0 {
				s.logger.Warn("skipping empty poll", "week", dbWeek.ID, "poll", poll.Poll, "ranks_returned", len(poll.Ranks))
				continue
			}

			if err := s.rankingRepo.ReplaceWeekPoll(dbWeek.ID, poll.Poll, rankings); err != nil {
				s.logger.Error("failed to replace rankings", "week", dbWeek.ID, "poll", poll.Poll, "error", err)
				continue
			}
			syncedCount += len(rankings)
		}
	}

	s.logger.Info("synced rankings", "synced", syncedCount)
	return nil
}

// rankingsFromPoll resolves poll's ranks against the team repository, in the
// scope of one SyncService.
//
// Teams resolve on the provider's team id, not the school name. The name would
// work today, but it is the one identifier the provider is free to restyle --
// and every other lookup in this sync already goes through external_id.
func (s *SyncService) rankingsFromPoll(weekID uuid.UUID, poll APIPoll) []models.TeamRanking {
	return rankingsFromPoll(weekID, poll, func(externalID int64) (uuid.UUID, bool) {
		team, err := s.teamRepo.FindByExternalID(externalID, models.SportFootball)
		if err != nil {
			return uuid.Nil, false
		}
		return team.ID, true
	}, s.logger)
}

// rankingsFromPoll converts one poll's ranks into rows for weekID, resolving
// each team through resolve. A team resolve cannot find is skipped and logged
// rather than failing the whole poll -- one team the feed knows and we do not
// should not cost the other 24 rankings in it.
//
// Kept independent of SyncService so it can be tested without a database: the
// interesting behavior (an unresolvable team drops out, a team missing from
// poll.Ranks produces no row for it) lives entirely in this function.
func rankingsFromPoll(weekID uuid.UUID, poll APIPoll, resolve func(externalID int64) (uuid.UUID, bool), logger *slog.Logger) []models.TeamRanking {
	rankings := make([]models.TeamRanking, 0, len(poll.Ranks))
	for _, rank := range poll.Ranks {
		teamID, ok := resolve(rank.TeamID)
		if !ok {
			logger.Warn("skipping ranking: team not found",
				"team_id", rank.TeamID, "school", rank.School, "poll", poll.Poll)
			continue
		}
		rankings = append(rankings, models.TeamRanking{
			WeekID:          weekID,
			TeamID:          teamID,
			Poll:            poll.Poll,
			Rank:            rank.Rank,
			FirstPlaceVotes: rank.FirstPlaceVotes,
			Points:          rank.Points,
		})
	}
	return rankings
}

// gameResultFrom builds the score row for a game the provider has reported
// points for.
//
// FinalizedAt is set only for a completed game. It is the flag bet settlement
// reads, so a game still in progress must leave it nil however plausible its
// score looks -- a 21-0 lead at halftime is not a 21-0 result.
func gameResultFrom(gameID uuid.UUID, g APIGame, now time.Time) *models.GameResult {
	var excitementIndex *decimal.Decimal
	if g.ExcitementIndex != nil {
		ei := decimal.NewFromFloat(*g.ExcitementIndex)
		excitementIndex = &ei
	}

	var finalizedAt *time.Time
	if g.Completed {
		finalizedAt = &now
	}

	return &models.GameResult{
		GameID:          gameID,
		HomeScore:       *g.HomePoints,
		AwayScore:       *g.AwayPoints,
		HomeLineScores:  models.IntSlice(g.HomeLineScores),
		AwayLineScores:  models.IntSlice(g.AwayLineScores),
		ExcitementIndex: excitementIndex,
		FinalizedAt:     finalizedAt,
	}
}

func (s *SyncService) syncLines(ctx context.Context, year int, week *int, seasonType *string) error {
	s.logger.Info("syncing lines", syncScope(year, week, seasonType)...)

	lines, err := s.client.GetLines(ctx, year, week, seasonType)
	if err != nil {
		return err
	}

	syncedCount := 0
	for _, l := range lines {
		// Look up game by external ID.
		game, err := s.gameRepo.FindByExternalID(l.ID, models.SportFootball)
		if err != nil {
			// Game might not exist yet; skip.
			continue
		}

		// Look up home and away team names for spread parsing.
		homeTeam, _ := s.teamRepo.FindByExternalID(l.HomeTeamID, models.SportFootball)
		awayTeam, _ := s.teamRepo.FindByExternalID(l.AwayTeamID, models.SportFootball)

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
					HomeOdds: decimal.NewFromInt(int64(*line.HomeMoneyline)),
					AwayOdds: decimal.NewFromInt(int64(*line.AwayMoneyline)),
				}
				if err := s.moneyLineOddsRepo.Upsert(mlOdds); err != nil {
					s.logger.Error("failed to upsert money line odds", "error", err)
				}
			}

			// Sync spread odds.
			if line.Spread != nil && line.FormattedSpread != "" {
				homeSpread, awaySpread := parseSpread(line.FormattedSpread, *line.Spread, homeTeam, awayTeam)
				spreadOdds := &models.SpreadOdds{
					GameID:     game.ID,
					Source:     source,
					HomeSpread: homeSpread,
					AwaySpread: awaySpread,
					// API doesn't provide individual spread odds, use standard -110.
					HomeOdds: decimal.NewFromInt(-110),
					AwayOdds: decimal.NewFromInt(-110),
				}
				if err := s.spreadOddsRepo.Upsert(spreadOdds); err != nil {
					s.logger.Error("failed to upsert spread odds", "error", err)
				}
			}

			// Sync over/under odds.
			if line.OverUnder != nil {
				ouOdds := &models.OverUnderOdds{
					GameID: game.ID,
					Source: source,
					Total:  decimal.NewFromFloat(*line.OverUnder),
					// API doesn't provide individual over/under odds, use standard -110.
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

// parseSpread parses the formatted spread and returns separate home and away spreads.
// Example: "Georgia -7" means Georgia is favored by 7 points.
// If Georgia is the home team: homeSpread = -7, awaySpread = +7
// If Georgia is the away team: homeSpread = +7, awaySpread = -7
func parseSpread(formatted string, spreadValue float64, homeTeam, awayTeam *models.Team) (homeSpread, awaySpread decimal.Decimal) {
	// Extract team name from formatted spread (e.g., "Georgia -7" -> "Georgia").
	re := regexp.MustCompile(`^(.+?)\s+[+-]?\d+\.?\d*$`)
	matches := re.FindStringSubmatch(formatted)

	if len(matches) < 2 {
		// Fallback: assume home team is favored by spreadValue.
		absSpread := math.Abs(spreadValue)
		return decimal.NewFromFloat(-absSpread), decimal.NewFromFloat(absSpread)
	}

	favoredTeamName := strings.TrimSpace(matches[1])

	// Determine if home or away team is favored.
	homeIsFavored := false
	if homeTeam != nil && strings.EqualFold(favoredTeamName, homeTeam.Name) {
		homeIsFavored = true
	}

	// Use absolute value since spreadValue might already be negative.
	absSpread := math.Abs(spreadValue)

	// Assign spreads: favored team gets negative spread, underdog gets positive.
	if homeIsFavored {
		return decimal.NewFromFloat(-absSpread), decimal.NewFromFloat(absSpread)
	}
	return decimal.NewFromFloat(absSpread), decimal.NewFromFloat(-absSpread)
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
		// Unknown provider, skip.
		return ""
	}
}

// syncScope returns structured attributes describing the scope of a sync.
// A nil week or seasonType means "all".
func syncScope(year int, week *int, seasonType *string) []any {
	attrs := []any{"year", year}
	if week != nil {
		attrs = append(attrs, "week", *week)
	}
	if seasonType != nil {
		attrs = append(attrs, "season_type", *seasonType)
	}
	return attrs
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
	// Add # prefix if missing.
	if !strings.HasPrefix(color, "#") {
		color = "#" + color
	}
	// Truncate to 7 chars (#RRGGBB).
	if len(color) > 7 {
		color = color[:7]
	}
	return &color
}
