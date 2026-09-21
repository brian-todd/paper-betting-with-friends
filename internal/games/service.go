package games

import (
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrWeekNotFound = errors.New("week not found")
	ErrGameNotFound = errors.New("game not found")
)

// GameWithOdds contains a game with its result and primary odds for grid display.
type GameWithOdds struct {
	Game      models.Game
	Result    *models.GameResult
	MoneyLine *models.MoneyLineOdds
	Spread    *models.SpreadOdds
	OverUnder *models.OverUnderOdds
	// HomeRank and AwayRank are the team's position in the week's effective
	// poll (CFP when the week has it, else AP), nil when unranked.
	HomeRank *int
	AwayRank *int
	// BetSummary describes the live bet the viewing user already holds on this
	// game, empty when there is none. Set by the handler, not this service,
	// since it is per-viewer.
	BetSummary string
}

// UnifiedOdds combines all odds types for a single source.
type UnifiedOdds struct {
	Source    models.OddsSource
	Spread    *models.SpreadOdds
	OverUnder *models.OverUnderOdds
	MoneyLine *models.MoneyLineOdds
}

// GameDetail contains a game with all odds from all sources for the detail page.
type GameDetail struct {
	Game        models.Game
	Result      *models.GameResult
	UnifiedOdds []UnifiedOdds
	// Keep old fields for backward compatibility if needed.
	MoneyLineOdds []models.MoneyLineOdds
	SpreadOdds    []models.SpreadOdds
	OverUnderOdds []models.OverUnderOdds
	// HomeRank and AwayRank are the team's position in the game's week's
	// effective poll. Always nil for basketball, which has no WeekID.
	HomeRank *int
	AwayRank *int
	// Matchup is the pre-game comparison panel -- ratings, records and recent
	// form for both sides. Nil for basketball, whose provider supplies none of
	// it, and for a football game whose teams have not been synced yet.
	Matchup *Matchup
	// Forecast is the weather expected at kickoff, for a game that has not
	// kicked off yet and is not played under a roof. Separate from Matchup
	// because it is a fact about the venue rather than about either side, and
	// absent once the game starts -- from then on Game.LiveState owns the
	// weather and two sources would eventually disagree on the page.
	Forecast *models.GameForecast
	// BetSummary describes the live bet the viewing user already holds on this
	// game, empty when there is none. Set by the handler, not this service,
	// since it is per-viewer.
	BetSummary string
}

// PageSize is how many games one page of the games grid holds.
const PageSize = 100

// Page describes where a filtered result set has been cut. Number is 1-based,
// and First/Last are the 1-based positions of this page's first and last game
// within the filtered set, for the "showing 1-100 of 412" line.
type Page struct {
	Number int
	Size   int
	Total  int
	Pages  int
	First  int
	Last   int
}

// HasPrev reports whether a page exists before this one.
func (p Page) HasPrev() bool { return p.Number > 1 }

// HasNext reports whether a page exists after this one.
func (p Page) HasNext() bool { return p.Number < p.Pages }

// Prev is the page number before this one, floored at the first page.
func (p Page) Prev() int {
	if p.Number <= 1 {
		return 1
	}
	return p.Number - 1
}

// Next is the page number after this one, capped at the last page.
func (p Page) Next() int {
	if p.Number >= p.Pages {
		return p.Pages
	}
	return p.Number + 1
}

// WeekWithGames contains one page of a week's filtered games, plus what the
// filter UI needs to describe itself.
type WeekWithGames struct {
	Week        *models.Week
	Games       []GameWithOdds
	Page        Page
	Conferences []repository.WeekConference
	// TotalInWeek is the unfiltered game count, so the page can say how much
	// the current filter is hiding.
	TotalInWeek int
}

// Service handles game business logic.
type Service struct {
	weekRepo          *repository.WeekRepository
	gameRepo          *repository.GameRepository
	gameResultRepo    *repository.GameResultRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	spreadOddsRepo    *repository.SpreadOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository
	rankingRepo       *repository.RankingRepository
	teamRatingRepo    *repository.TeamRatingRepository
	teamRecordRepo    *repository.TeamRecordRepository

	teamATSRecordRepo     *repository.TeamATSRecordRepository
	teamAdvancedStatsRepo *repository.TeamAdvancedStatsRepository
	gamePregameWPRepo     *repository.GamePregameWinProbabilityRepository
	gameForecastRepo      *repository.GameForecastRepository

	// location resolves the calendar-day and time-of-day filters. Those ask
	// which local day a kickoff falls on, which no instant can answer alone.
	location *time.Location

	// clock decides which week is current and whether a game is still open to
	// bet on. See cfbdata.SyncService for why this is a settable field.
	clock timeutil.Clock
}

// NewService creates a new games service.
func NewService(db *gorm.DB, location *time.Location) *Service {
	if location == nil {
		location = time.UTC
	}
	return &Service{
		weekRepo:          repository.NewWeekRepository(db),
		gameRepo:          repository.NewGameRepository(db),
		gameResultRepo:    repository.NewGameResultRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
		rankingRepo:       repository.NewRankingRepository(db),
		teamRatingRepo:    repository.NewTeamRatingRepository(db),
		teamRecordRepo:    repository.NewTeamRecordRepository(db),

		teamATSRecordRepo:     repository.NewTeamATSRecordRepository(db),
		teamAdvancedStatsRepo: repository.NewTeamAdvancedStatsRepository(db),
		gamePregameWPRepo:     repository.NewGamePregameWinProbabilityRepository(db),
		gameForecastRepo:      repository.NewGameForecastRepository(db),

		location: location,
	}
}

// SetClock overrides the time source. The zero value is time.Now, so only a
// test replaying a recorded response needs to call this.
func (s *Service) SetClock(now func() time.Time) {
	s.clock.Set(now)
}

// Location is the timezone the service resolves calendar-day filters in.
func (s *Service) Location() *time.Location { return s.location }

// Now is the service's own clock, for a handler that has to label a page with
// the moment it is describing rather than the moment it is being rendered.
func (s *Service) Now() time.Time { return s.clock.Now() }

// GetWeekWithGames retrieves one page of a week's games, narrowed by filter.
//
// page is 1-based and clamped into range, so a stale bookmark past the end of a
// newly-filtered set lands on the last page instead of an empty one.
func (s *Service) GetWeekWithGames(season, weekNumber int, seasonType models.SeasonType, filter repository.GameFilter, page int) (*WeekWithGames, error) {
	week, err := s.weekRepo.FindBySeasonNumberAndType(season, weekNumber, seasonType)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWeekNotFound
		}
		return nil, err
	}

	filter.Location = s.location

	// Resolved before the games query, since the effective poll name is an
	// input to the filter itself.
	poll, ranks, err := s.rankingRepo.EffectiveRanks(week.ID)
	if err != nil {
		return nil, err
	}
	filter.RankingPoll = poll

	if page < 1 {
		page = 1
	}
	games, total, err := s.gameRepo.FindWeekGames(week.ID, filter, (page-1)*PageSize, PageSize)
	if err != nil {
		return nil, err
	}

	// Refetch only when the requested page turned out to be past the end, which
	// is what a bookmark from a wider filter looks like.
	pageInfo := paginate(page, total)
	if pageInfo.Number != page {
		games, _, err = s.gameRepo.FindWeekGames(week.ID, filter, (pageInfo.Number-1)*PageSize, PageSize)
		if err != nil {
			return nil, err
		}
	}

	gamesWithOdds, err := s.attachOdds(games, ranks)
	if err != nil {
		return nil, err
	}

	conferences, err := s.gameRepo.FindWeekConferences(week.ID)
	if err != nil {
		return nil, err
	}

	totalInWeek, err := s.gameRepo.CountWeekGames(week.ID, repository.GameFilter{})
	if err != nil {
		return nil, err
	}

	return &WeekWithGames{
		Week:        week,
		Games:       gamesWithOdds,
		Page:        pageInfo,
		Conferences: conferences,
		TotalInWeek: totalInWeek,
	}, nil
}

// paginate clamps a requested page against a result count and works out the
// span it covers.
func paginate(page, total int) Page {
	pages := max((total+PageSize-1)/PageSize, 1)
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}

	first := (page-1)*PageSize + 1
	last := min(page*PageSize, total)
	if total == 0 {
		first = 0
	}

	return Page{Number: page, Size: PageSize, Total: total, Pages: pages, First: first, Last: last}
}

// attachOdds decorates a page of games with the primary line for each market
// and, from ranks, each side's poll position.
//
// The odds come back in three batched queries rather than three per game. The
// result is already preloaded on the game, so it costs nothing here.
func (s *Service) attachOdds(games []models.Game, ranks map[uuid.UUID]int) ([]GameWithOdds, error) {
	gameIDs := make([]uuid.UUID, len(games))
	for i, game := range games {
		gameIDs[i] = game.ID
	}

	moneyLine, err := s.moneyLineOddsRepo.FindBookLinesByGames(gameIDs)
	if err != nil {
		return nil, err
	}
	spread, err := s.spreadOddsRepo.FindBookLinesByGames(gameIDs)
	if err != nil {
		return nil, err
	}
	overUnder, err := s.overUnderOddsRepo.FindBookLinesByGames(gameIDs)
	if err != nil {
		return nil, err
	}

	gamesWithOdds := make([]GameWithOdds, len(games))
	for i, game := range games {
		gamesWithOdds[i] = GameWithOdds{Game: game, Result: game.Result}
		if rows := moneyLine[game.ID]; len(rows) > 0 {
			gamesWithOdds[i].MoneyLine = &rows[0]
		}
		if rows := spread[game.ID]; len(rows) > 0 {
			gamesWithOdds[i].Spread = &rows[0]
		}
		if rows := overUnder[game.ID]; len(rows) > 0 {
			gamesWithOdds[i].OverUnder = &rows[0]
		}
		if rank, ok := ranks[game.HomeTeamID]; ok {
			gamesWithOdds[i].HomeRank = &rank
		}
		if rank, ok := ranks[game.AwayTeamID]; ok {
			gamesWithOdds[i].AwayRank = &rank
		}
	}
	return gamesWithOdds, nil
}

// GetSeasonWeeks retrieves all weeks for a season and season type.
func (s *Service) GetSeasonWeeks(season int, seasonType models.SeasonType) ([]models.Week, error) {
	weeks, err := s.weekRepo.FindBySeason(season)
	if err != nil {
		return nil, err
	}

	// Filter by season type.
	filtered := make([]models.Week, 0)
	for _, week := range weeks {
		if week.SeasonType == seasonType {
			filtered = append(filtered, week)
		}
	}

	return filtered, nil
}

// GetAvailableSeasonTypes returns available season types for a given season.
func (s *Service) GetAvailableSeasonTypes(season int) ([]models.SeasonType, error) {
	weeks, err := s.weekRepo.FindBySeason(season)
	if err != nil {
		return nil, err
	}

	typeMap := make(map[models.SeasonType]bool)
	for _, week := range weeks {
		typeMap[week.SeasonType] = true
	}

	types := make([]models.SeasonType, 0, len(typeMap))
	// Order: regular first, then postseason.
	if typeMap[models.SeasonTypeRegular] {
		types = append(types, models.SeasonTypeRegular)
	}
	if typeMap[models.SeasonTypePostseason] {
		types = append(types, models.SeasonTypePostseason)
	}

	return types, nil
}

// GetAvailableSeasons retrieves all unique seasons that have weeks.
func (s *Service) GetAvailableSeasons() ([]int, error) {
	weeks, err := s.weekRepo.FindAll()
	if err != nil {
		return nil, err
	}

	// Extract unique seasons.
	seasonMap := make(map[int]bool)
	for _, week := range weeks {
		seasonMap[week.Season] = true
	}

	seasons := make([]int, 0, len(seasonMap))
	for season := range seasonMap {
		seasons = append(seasons, season)
	}

	// Sort descending (most recent first).
	for i := 0; i < len(seasons)-1; i++ {
		for j := i + 1; j < len(seasons); j++ {
			if seasons[j] > seasons[i] {
				seasons[i], seasons[j] = seasons[j], seasons[i]
			}
		}
	}

	return seasons, nil
}

// GetGameByID retrieves a game by its ID.
func (s *Service) GetGameByID(gameID uuid.UUID) (*models.Game, error) {
	game, err := s.gameRepo.FindByID(gameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGameNotFound
		}
		return nil, err
	}
	return game, nil
}

// GetGameDetail retrieves a game with all odds from all sources.
func (s *Service) GetGameDetail(gameID uuid.UUID) (*GameDetail, error) {
	game, err := s.gameRepo.FindByID(gameID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGameNotFound
		}
		return nil, err
	}

	detail := &GameDetail{Game: *game}

	// Load result if available.
	result, err := s.gameResultRepo.FindByGameID(gameID)
	if err == nil {
		detail.Result = result
	}

	// Load all odds from all sources.
	moneyLine, err := s.moneyLineOddsRepo.FindBookLinesByGame(gameID)
	if err == nil {
		detail.MoneyLineOdds = moneyLine
	}

	spread, err := s.spreadOddsRepo.FindBookLinesByGame(gameID)
	if err == nil {
		detail.SpreadOdds = spread
	}

	overUnder, err := s.overUnderOddsRepo.FindBookLinesByGame(gameID)
	if err == nil {
		detail.OverUnderOdds = overUnder
	}

	// Create unified odds by merging all sources.
	detail.UnifiedOdds = mergeOddsBySources(moneyLine, spread, overUnder)

	// Basketball games carry no WeekID, and there is nothing ranked to show.
	//
	// A failure here costs the rank badge and nothing else, so the page still
	// renders -- but it is logged rather than swallowed, since silently
	// unranking every team looks identical to a week no poll has been synced
	// for.
	if game.WeekID != nil {
		_, ranks, err := s.rankingRepo.EffectiveRanks(*game.WeekID)
		if err != nil {
			slog.Error("failed to fetching rankings for game detail", "game", game.ID, "error", err)
		} else {
			if rank, ok := ranks[game.HomeTeamID]; ok {
				detail.HomeRank = &rank
			}
			if rank, ok := ranks[game.AwayTeamID]; ok {
				detail.AwayRank = &rank
			}
		}
	}

	// The comparison panel is football-only because its provider is, and the
	// sport has to be checked before HasAny rather than instead of it. Nothing
	// basketball reaches here populates a rating, a record or an efficiency
	// figure -- but RecentResults is sport-aware and answers happily, so a
	// basketball page drew a panel consisting of nothing but form, and spent a
	// dozen queries finding out that the rest of it was empty. HasAny still
	// earns its place afterwards: a football game synced before the stats job
	// first ran has to render the absent case.
	if game.Sport == models.SportFootball {
		if matchup := s.buildMatchup(game); matchup.HasAny() {
			detail.Matchup = matchup
		}
	}
	detail.Forecast = s.forecastFor(game)

	return detail, nil
}

// forecastFor is the kickoff weather, or nil when there is nothing to show.
//
// Gated on the kickoff time rather than on Game.Status, deliberately and
// unlike most of this page: status only advances when a sync runs, so a game
// that started twenty minutes ago can still read "scheduled", and a forecast
// is exactly the thing nobody wants to see once the game is being played.
// The instant comparison needs no location -- see AGENTS.md.
func (s *Service) forecastFor(game *models.Game) *models.GameForecast {
	if !game.ScheduledAt.After(s.clock.Now()) {
		return nil
	}

	forecast, err := s.gameForecastRepo.FindByGameID(game.ID)
	if err != nil {
		slog.Error("failed to fetch game forecast", "game", game.ID, "error", err)
		return nil
	}
	if !forecast.Show() {
		return nil
	}
	return forecast
}

// formGames is how many recent results the form strip shows.
const formGames = 5

// buildMatchup assembles the comparison panel for a game.
//
// Every lookup in here degrades to an absent section, logged: a page that
// renders without its ratings is worth more than a 500, and the log is what
// keeps an unsynced season distinguishable from a season where nobody is rated.
// Callers get a non-nil Matchup and ask HasAny whether it is worth drawing.
func (s *Service) buildMatchup(game *models.Game) *Matchup {
	matchup := &Matchup{
		NeutralSite: game.NeutralSite,
		Home: TeamStats{
			Team:       game.HomeTeam,
			PregameElo: game.HomePregameElo,
			Ratings:    make(map[models.RatingSource]models.TeamRating),
		},
		Away: TeamStats{
			Team:       game.AwayTeam,
			PregameElo: game.AwayPregameElo,
			Ratings:    make(map[models.RatingSource]models.TeamRating),
		},
	}

	// One query for both sides rather than one per side -- the page always
	// wants the pair, and at three sources this is six rows.
	ratings, err := s.teamRatingRepo.FindForTeams(game.Season, game.HomeTeamID, game.AwayTeamID)
	if err != nil {
		slog.Error("failed to fetch team ratings", "game", game.ID, "error", err)
	}
	for _, rating := range ratings {
		switch rating.TeamID {
		case game.HomeTeamID:
			matchup.Home.Ratings[rating.Source] = rating
		case game.AwayTeamID:
			matchup.Away.Ratings[rating.Source] = rating
		}
	}

	records, err := s.teamRecordRepo.FindForTeams(game.Season, game.HomeTeamID, game.AwayTeamID)
	if err != nil {
		slog.Error("failed to fetch team records", "game", game.ID, "error", err)
	}
	if record, ok := records[game.HomeTeamID]; ok {
		matchup.Home.Record = &record
	}
	if record, ok := records[game.AwayTeamID]; ok {
		matchup.Away.Record = &record
	}

	ats, err := s.teamATSRecordRepo.FindForTeams(game.Season, game.HomeTeamID, game.AwayTeamID)
	if err != nil {
		slog.Error("failed to fetch ats records", "game", game.ID, "error", err)
	}
	if record, ok := ats[game.HomeTeamID]; ok {
		matchup.Home.ATS = &record
	}
	if record, ok := ats[game.AwayTeamID]; ok {
		matchup.Away.ATS = &record
	}

	advanced, err := s.teamAdvancedStatsRepo.FindForTeams(game.Season, game.HomeTeamID, game.AwayTeamID)
	if err != nil {
		slog.Error("failed to fetch advanced stats", "game", game.ID, "error", err)
	}
	if stats, ok := advanced[game.HomeTeamID]; ok {
		matchup.Home.Advanced = &stats
	}
	if stats, ok := advanced[game.AwayTeamID]; ok {
		matchup.Away.Advanced = &stats
	}

	winProbability, err := s.gamePregameWPRepo.FindByGameID(game.ID)
	if err != nil {
		slog.Error("failed to fetch pregame win probability", "game", game.ID, "error", err)
	}
	matchup.WinProbability = winProbability

	matchup.Home.Form = s.recentForm(game, game.HomeTeamID)
	matchup.Away.Form = s.recentForm(game, game.AwayTeamID)

	return matchup
}

// recentForm is one team's last few finished games before this one, within the
// same season. An empty strip in week 1 is the correct answer, not a failure.
func (s *Service) recentForm(game *models.Game, teamID uuid.UUID) []RecentResult {
	played, err := s.gameRepo.RecentResults(teamID, game.Sport, game.Season, game.ScheduledAt, formGames)
	if err != nil {
		slog.Error("failed to fetch recent results", "game", game.ID, "team", teamID, "error", err)
		return nil
	}

	form := make([]RecentResult, 0, len(played))
	for _, previous := range played {
		if result, ok := recentResultFrom(previous, teamID); ok {
			form = append(form, result)
		}
	}
	return form
}

// mergeOddsBySources combines odds from different types by their source.
func mergeOddsBySources(
	moneyLine []models.MoneyLineOdds,
	spread []models.SpreadOdds,
	overUnder []models.OverUnderOdds,
) []UnifiedOdds {
	// Create maps for quick lookup by source. The rows arrive without custom
	// lines, so a source here is always a book.
	moneyLineMap := make(map[models.OddsSource]*models.MoneyLineOdds)
	for i := range moneyLine {
		moneyLineMap[moneyLine[i].Source] = &moneyLine[i]
	}

	spreadMap := make(map[models.OddsSource]*models.SpreadOdds)
	for i := range spread {
		spreadMap[spread[i].Source] = &spread[i]
	}

	overUnderMap := make(map[models.OddsSource]*models.OverUnderOdds)
	for i := range overUnder {
		overUnderMap[overUnder[i].Source] = &overUnder[i]
	}

	// Collect all unique sources.
	sourcesMap := make(map[models.OddsSource]bool)
	for source := range moneyLineMap {
		sourcesMap[source] = true
	}
	for source := range spreadMap {
		sourcesMap[source] = true
	}
	for source := range overUnderMap {
		sourcesMap[source] = true
	}

	// Build unified odds.
	unified := make([]UnifiedOdds, 0, len(sourcesMap))
	for source := range sourcesMap {
		unified = append(unified, UnifiedOdds{
			Source:    source,
			MoneyLine: moneyLineMap[source],
			Spread:    spreadMap[source],
			OverUnder: overUnderMap[source],
		})
	}

	// Sort by source name for consistent display.
	sort.Slice(unified, func(i, j int) bool {
		return string(unified[i].Source) < string(unified[j].Source)
	})

	return unified
}

// GetCurrentWeek returns the season, week number, and season type of the week
// /games should land on right now.
func (s *Service) GetCurrentWeek() (season int, weekNumber int, seasonType models.SeasonType, found bool) {
	weeks, err := s.weekRepo.FindAll()
	if err != nil {
		return 0, 0, "", false
	}

	usable := plausibleWeeks(weeks)
	if skipped := len(weeks) - len(usable); skipped > 0 {
		slog.Warn("ignoring weeks with an implausible span", "count", skipped, "max_span", models.MaxWeekSpan)
	}

	week, ok := pickCurrentWeek(s.clock.Now(), usable)
	if !ok {
		return 0, 0, "", false
	}
	return week.Season, week.Number, week.SeasonType, true
}

// plausibleWeeks drops rows whose dates cannot describe a week. The rule lives
// on the model because the data sync has to apply the same one -- see
// models.Week.Plausible.
func plausibleWeeks(weeks []models.Week) []models.Week {
	usable := make([]models.Week, 0, len(weeks))
	for _, week := range weeks {
		if !week.Plausible() {
			continue
		}
		usable = append(usable, week)
	}
	return usable
}

// pickCurrentWeek chooses the week that best represents "now", in order of
// preference: the week now falls inside, then the next one to start, then the
// one that ended most recently.
//
// The upcoming-week step is what makes this track the calendar. Weeks do not
// tile the year -- there is a gap between the last bowl game and next August,
// and a one-minute seam between consecutive weeks -- so falling straight to the
// latest end date lands on the postseason for most of the offseason, months
// away from the games anyone is looking for.
//
// Every comparison here is between two instants, so it needs no location.
func pickCurrentWeek(now time.Time, weeks []models.Week) (*models.Week, bool) {
	var current, upcoming, latest *models.Week

	for i := range weeks {
		week := &weeks[i]

		switch {
		// Start is inclusive and end exclusive so the seam between two weeks
		// belongs to exactly one of them.
		case !now.Before(week.StartDate) && now.Before(week.EndDate):
			if current == nil || week.StartDate.Before(current.StartDate) {
				current = week
			}
		case week.StartDate.After(now):
			if upcoming == nil || week.StartDate.Before(upcoming.StartDate) {
				upcoming = week
			}
		}

		if latest == nil || week.EndDate.After(latest.EndDate) {
			latest = week
		}
	}

	switch {
	case current != nil:
		return current, true
	case upcoming != nil:
		return upcoming, true
	case latest != nil:
		return latest, true
	}
	return nil, false
}
