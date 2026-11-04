package cbbdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

// gamesResultCap is the most games the API returns for one /games request.
const gamesResultCap = 3000

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
	oddsMovementRepo  *repository.OddsMovementRepository
	gameLiveStateRepo *repository.GameLiveStateRepository
	pollRankRepo      *repository.PollRankRepository
	gameLineupRepo    *repository.GameLineupRepository
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
		oddsMovementRepo:  repository.NewOddsMovementRepository(db),
		gameLiveStateRepo: repository.NewGameLiveStateRepository(db),
		pollRankRepo:      repository.NewPollRankRepository(db),
		gameLineupRepo:    repository.NewGameLineupRepository(db),
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

	// Last, so a poll feed that is down costs the ranks and not the season. This is
	// also the first moment a first deploy has teams to attach ranks to.
	if err := s.syncRankings(ctx, season); err != nil {
		return errors.Join(incomplete, err)
	}

	s.logger.Info("full seed completed for season", "season", season)
	return incomplete
}

// SyncLineups asks for the five-man units of a few finished games.
//
// The number of games is the spend: one request each, capped per run, which is what
// holds this job to its share however many games are waiting. A busy day drains over
// the quieter ones after it.
//
// A failed request says nothing about the game, so it does not use up an attempt:
// an outage would otherwise abandon every game it touched. It does start the retry
// wait, so a game the feed always refuses cannot use up the whole run, every run, and
// the lookback ends it. A run cut short by shutdown records nothing at all.
func (s *SyncService) SyncLineups(ctx context.Context) error {
	now := s.clock.Now()

	due, err := s.gameLineupRepo.FindDue(now, lineupFetchRules, lineupsPerRun)
	if err != nil {
		return fmt.Errorf("finding games due lineups: %w", err)
	}

	var failed syncerr.Tally
	stored := 0
	for _, game := range due {
		if err := ctx.Err(); err != nil {
			return err
		}

		rows, fetchErr := s.client.GetGameLineups(ctx, game.ExternalID)
		if fetchErr != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			s.logger.Error("failed to fetch lineups", "game", game.ExternalID, "error", fetchErr)
			failed.Add(fetchErr)
			if err := s.gameLineupRepo.RecordFailure(game.ID, now); err != nil {
				s.logger.Error("failed to record a failed lineups request", "game", game.ExternalID, "error", err)
				failed.Add(err)
			}
			continue
		}

		lineups, err := s.lineupsFor(game.ID, rows)
		if err != nil {
			return err
		}

		if err := s.gameLineupRepo.RecordFetch(game.ID, lineups, now); err != nil {
			s.logger.Error("failed to store lineups", "game", game.ExternalID, "error", err)
			failed.Add(err)
			continue
		}
		if len(lineups) > 0 {
			stored++
		}
	}

	s.logger.Info("synced lineups", "asked", len(due), "found", stored, "failed", failed.Count())
	return failed.Err("lineups")
}

// lineupsFor resolves one response's teams and builds its rows.
func (s *SyncService) lineupsFor(gameID uuid.UUID, rows []APIGameLineup) ([]models.GameLineup, error) {
	externalIDs := make([]int64, len(rows))
	for i, row := range rows {
		externalIDs[i] = row.TeamID
	}
	teams, err := s.teamRepo.IDsByExternalID(models.SportBasketball, externalIDs)
	if err != nil {
		return nil, fmt.Errorf("looking up lineup teams: %w", err)
	}
	return gameLineupsFrom(gameID, rows, teams), nil
}

// gameLineupsFrom keeps the rows for teams we hold, one per team and unit.
func gameLineupsFrom(gameID uuid.UUID, rows []APIGameLineup, teams map[int64]uuid.UUID) []models.GameLineup {
	type key struct {
		team uuid.UUID
		hash string
	}
	seen := make(map[key]bool, len(rows))

	lineups := make([]models.GameLineup, 0, len(rows))
	for _, row := range rows {
		teamID, ok := teams[row.TeamID]
		if !ok || row.IDHash == "" || len(row.Athletes) == 0 {
			continue
		}
		k := key{teamID, row.IDHash}
		if seen[k] {
			continue
		}
		seen[k] = true
		lineups = append(lineups, models.GameLineup{
			GameID:             gameID,
			TeamID:             teamID,
			IDHash:             row.IDHash,
			Athletes:           row.Athletes,
			Seconds:            int(math.Round(row.TotalSeconds)),
			PointsFor:          row.TeamStats.Points,
			PossessionsFor:     row.TeamStats.Possessions,
			PointsAgainst:      row.Opponent.Points,
			PossessionsAgainst: row.Opponent.Possessions,
		})
	}
	return lineups
}

// SyncRankings stores the AP poll for the current season.
//
// One request returns every poll so far, so a run that fails or is missed is
// repaired by the next. Only AP is kept, since it is the one poll the pages show.
func (s *SyncService) SyncRankings(ctx context.Context) error {
	return s.syncRankings(ctx, SeasonFor(s.clock.Now()))
}

// syncRankings stores one season's AP poll. A run that had ranks to store and could
// place none of them is an error rather than a success: it means the teams were never
// seeded, and the job would otherwise look healthy with every game unranked.
func (s *SyncService) syncRankings(ctx context.Context, season int) error {
	s.logger.Info("syncing rankings", "season", season)

	rows, err := s.client.GetRankings(ctx, season)
	if err != nil {
		return fmt.Errorf("syncing rankings: %w", err)
	}

	externalIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		externalIDs = append(externalIDs, int64(row.TeamID))
	}
	teams, err := s.teamRepo.IDsByExternalID(models.SportBasketball, externalIDs)
	if err != nil {
		return fmt.Errorf("looking up ranked teams: %w", err)
	}

	ranks, unknown := pollRanksFrom(rows, teams)
	if unknown > 0 {
		s.logger.Warn("skipping poll ranks for teams not stored", "ranks", unknown)
		if len(ranks) == 0 {
			return fmt.Errorf("storing rankings: none of the %d ranked teams are stored, so basketball teams need seeding", unknown)
		}
	}
	if err := s.pollRankRepo.Upsert(ranks); err != nil {
		return fmt.Errorf("storing rankings: %w", err)
	}

	s.logger.Info("synced rankings", "ranks", len(ranks))
	return nil
}

// RankingsMissing reports whether a process starting at now should fetch rankings
// rather than wait for the weekly slot: it is in season and this season has no poll
// stored. A database error reads as not missing, since the slot will come round.
//
// How old the newest poll is cannot decide this. AP goes weeks without publishing,
// and the feed's silence would read as stale, costing a request on every restart for
// nothing new. So a slot missed while the process was down waits for the next one, or
// for the admin page's manual run. Until a season's first poll this still spends one
// request a restart, which is what cbb-schedule spends on every one.
func (s *SyncService) RankingsMissing(now time.Time, loc *time.Location) bool {
	if !InSeason(now, loc) {
		return false
	}
	_, ok, err := s.pollRankRepo.LatestPollDate(SeasonFor(now), models.PollAP)
	if err != nil {
		s.logger.Error("failed to read the latest poll date", "error", err)
		return false
	}
	return !ok
}

// pollRanksFrom keeps the ranked AP rows whose team is stored, one per team and
// date, and counts the ranked rows it had to drop for a team it does not know.
func pollRanksFrom(rows []APIPollRanking, teams map[int64]uuid.UUID) (ranks []models.PollRank, unknown int) {
	type key struct {
		team uuid.UUID
		date time.Time
	}
	seen := make(map[key]bool, len(rows))

	for _, row := range rows {
		if row.PollType != models.PollAP || row.Ranking == nil || row.PollDate == nil {
			continue
		}
		teamID, ok := teams[int64(row.TeamID)]
		if !ok {
			unknown++
			continue
		}
		k := key{teamID, row.PollDate.UTC()}
		if seen[k] {
			continue
		}
		seen[k] = true
		ranks = append(ranks, models.PollRank{
			TeamID:   teamID,
			Season:   row.Season,
			Poll:     row.PollType,
			PollDate: *row.PollDate,
			Rank:     int(*row.Ranking),
		})
	}
	return ranks, unknown
}

// SyncSchedule refreshes the games in the wide window: a week back, so an outage
// longer than the incremental window's one-day lookback cannot leave a game
// unfinished for good, and a month ahead, so the day picker has games to show.
//
// One request. The widest window this spans held 1,986 games across the captured
// season, inside the API's 3,000-game cap.
func (s *SyncService) SyncSchedule(ctx context.Context) error {
	start, end := scheduleWindow(s.clock.Now())
	s.logger.Info("syncing schedule", "from", start, "to", end)

	if err := s.syncGames(ctx, GameQueryOpts{StartDateRange: &start, EndDateRange: &end}); err != nil {
		return fmt.Errorf("syncing schedule: %w", err)
	}
	return nil
}

// SyncGames refreshes the games -- schedule, status and score -- in the
// incremental window, and settles any that finished.
//
// It is a job of its own, apart from SyncLines, so that a failure here costs
// the lines nothing. The scoreboard normally gets to a finished game first; this
// is the backstop for what that feed misses. See the cadence notes in schedule.go.
func (s *SyncService) SyncGames(ctx context.Context) error {
	start, end := incrementalWindow(s.clock.Now())
	s.logger.Info("syncing games", "from", start, "to", end)

	if err := s.syncGames(ctx, GameQueryOpts{StartDateRange: &start, EndDateRange: &end}); err != nil {
		return fmt.Errorf("syncing games: %w", err)
	}
	return nil
}

// SyncLines refreshes every book's lines for the games in the incremental
// window, and records the ones that moved.
//
// A line for a game SyncGames has not written yet is skipped, and picked up by
// the first run after it has. The lines schedule sits behind the games one so
// that is rare -- a games run that failed, or overran the lag.
func (s *SyncService) SyncLines(ctx context.Context) error {
	start, end := incrementalWindow(s.clock.Now())
	s.logger.Info("syncing lines", "from", start, "to", end)

	if err := s.syncLines(ctx, LineQueryOpts{StartDateRange: &start, EndDateRange: &end}); err != nil {
		return fmt.Errorf("syncing lines: %w", err)
	}
	return nil
}

// incrementalWindow is the span the incremental syncs ask for: from the start
// of yesterday to the end of three days from now, as UTC days in the ISO 8601
// form the API validates.
//
// The times of day are appended as text rather than written into the layout.
// Go reads a layout by its reference values, so "T23:59:59.000Z" is not a
// literal: the 2 is the day, the 3 the hour, the 5 the second and .000 the
// milliseconds. It rendered as "T269:309:309.747Z", CBBD refused it with a 400,
// and the job had failed every run since it was written -- the only basketball
// data in any database came from a manual seed.
//
// The instant is taken to UTC first, since the strings say Z.
func incrementalWindow(now time.Time) (start, end string) {
	return dayWindow(now, 1, 3)
}

// scheduleWindow is the span the daily schedule sync asks for.
func scheduleWindow(now time.Time) (start, end string) {
	return dayWindow(now, 7, 30)
}

// dayWindow is whole UTC days around now, in the form the API validates.
func dayWindow(now time.Time, back, ahead int) (start, end string) {
	today := now.UTC()
	start = today.AddDate(0, 0, -back).Format(time.DateOnly) + "T00:00:00.000Z"
	end = today.AddDate(0, 0, ahead).Format(time.DateOnly) + "T23:59:59.000Z"
	return start, end
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

	// A full page means the API dropped the rest of the window, and every later
	// game in it silently stays unwritten.
	if len(games) >= gamesResultCap {
		s.logger.Warn("games response hit the API's result cap", "games", len(games))
	}

	syncedCount := 0
	for _, g := range games {
		// A missing startDate decodes to year 1, which reads as a tip-off long
		// past: it closes betting and freezes every bet already placed.
		if g.StartDate.IsZero() {
			s.logger.Warn("skipping game: no start date", "game", g.ID)
			continue
		}

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

		status := s.gamesFeedStatus(g)

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

		// Sync the score for a game being played or over. gameResultFrom leaves
		// FinalizedAt nil until the game is over.
		//
		// The status decides this, not the presence of points: the feed reports
		// every scheduled, postponed and cancelled game as 0-0, and a row written
		// from points alone puts a score on a game nobody has played.
		if hasScore(status) && g.HomePoints != nil && g.AwayPoints != nil {
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

	// Every line written is offered to the history, which keeps it only if it
	// moved. One instant for the whole run, so every book's movement from one
	// response is dated alike. A movement that will not save leaves the run
	// incomplete, but in its own tally: the lines themselves are stored and
	// bettable, and an admin page reading "odds writes failed" would send
	// someone after the wrong fault. The next run writes it instead, since the
	// history still disagrees with the feed.
	now := s.clock.Now()
	moved := 0
	var failedMoves syncerr.Tally
	recordMove := func(m models.OddsMovement) {
		recorded, err := s.oddsMovementRepo.Record(m)
		if err != nil {
			s.logger.Error("failed to record odds movement",
				"game_id", m.GameID, "source", m.Source, "market", m.Market, "error", err)
			failedMoves.Add(err)
			return
		}
		if recorded {
			moved++
		}
	}

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

		quotes, unknown := quotesBySource(l.Lines)
		for _, provider := range unknown {
			if !unknownBooks[provider] {
				unknownBooks[provider] = true
				s.logger.Warn("skipping odds from an unrecognised sportsbook", "provider", provider)
			}
		}

		for _, quote := range quotes {
			source, line := quote.source, quote.line

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
				} else {
					recordMove(models.MoneyLineMovement(*mlOdds, now))
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
				} else {
					recordMove(models.SpreadMovement(*spreadOdds, now))
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
				} else {
					recordMove(models.OverUnderMovement(*ouOdds, now))
				}
			}
		}

		syncedCount++
	}

	s.logger.Info("synced lines for games", "for", syncedCount, "moved", moved,
		"failed_writes", failed.Count(), "failed_history_writes", failedMoves.Count())
	return errors.Join(failed.Err("odds"), failedMoves.Err("odds history"))
}

// hasScore reports whether a game in this status has played any basketball, and
// so whether the points the feed reports for it are a score.
func hasScore(status models.GameStatus) bool {
	return status == models.GameStatusInProgress || status == models.GameStatusFinal
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

// gamesFeedStatus is the status to store for a /games row. A "final" the score
// cannot back up is stored as in progress, which leaves the score provisional and
// the bets pending until a feed reports a result that can be final. Upsert never
// regresses a final, so this cannot reopen a game already finished.
func (s *SyncService) gamesFeedStatus(g APIGame) models.GameStatus {
	status := mapGameStatus(g.Status)
	if status == models.GameStatusFinal && !finalIsCredible(g.HomePoints, g.AwayPoints) {
		s.logger.Warn("ignoring a final the score cannot back up",
			"game", g.ID, "home_points", g.HomePoints, "away_points", g.AwayPoints)
		return models.GameStatusInProgress
	}
	return status
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

// A sourcedQuote is one book's line for a game, its provider resolved to the
// source the odds tables are keyed by.
type sourcedQuote struct {
	source models.OddsSource
	line   APILineProvider
}

// quotesBySource folds a game's quotes to one per source, and names the
// providers it did not recognise. See cfbdata's for why: two spellings mapping
// to one source would otherwise write the line history twice at one instant,
// and every later run would record a phantom move. Market by market, the later
// quote wins, which is what the odds rows held when both were written.
func quotesBySource(lines []APILineProvider) (quotes []sourcedQuote, unknown []string) {
	index := make(map[models.OddsSource]int)
	for _, line := range lines {
		source, known := mapProviderToSource(line.Provider)
		if !known {
			unknown = append(unknown, line.Provider)
		}
		if source == "" {
			continue
		}

		i, seen := index[source]
		if !seen {
			index[source] = len(quotes)
			quotes = append(quotes, sourcedQuote{source: source, line: line})
			continue
		}
		folded := &quotes[i].line
		if line.HomeMoneyline != nil && line.AwayMoneyline != nil {
			folded.HomeMoneyline, folded.AwayMoneyline = line.HomeMoneyline, line.AwayMoneyline
		}
		if line.Spread != nil {
			folded.Spread = line.Spread
		}
		if line.OverUnder != nil {
			folded.OverUnder = line.OverUnder
		}
	}
	return quotes, unknown
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
