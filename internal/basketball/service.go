package basketball

import (
	"cmp"
	"log/slog"
	"slices"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GameWithOdds contains a game with its result and primary odds for grid display.
type GameWithOdds struct {
	Game      models.Game
	Result    *models.GameResult
	MoneyLine *models.MoneyLineOdds
	Spread    *models.SpreadOdds
	OverUnder *models.OverUnderOdds
	// HomeRank and AwayRank are each side's AP position, nil when unranked.
	HomeRank *int
	AwayRank *int
	// BetSummary describes the viewer's bet on this game, empty when there is none.
	// Set by the handler, since it is per-viewer.
	BetSummary string
}

// noConference is what the sync files a team under when the feed gives it none,
// which is every non-Division I opponent.
const noConference = "Independent"

// DayQuery narrows one day's games. The zero value is the page's default.
type DayQuery struct {
	// Filter is what the reader chose. Its BettableOnly, RankingPoll, RankedTeamIDs and
	// Location are the service's to set, and whatever arrives in them is ignored.
	Filter repository.GameFilter
	// AllGames asks for games without a line as well.
	AllGames bool
}

// DayGames is one day's games and what the page needs to describe them.
type DayGames struct {
	Games []GameWithOdds
	// Total is every game on the day, whatever the query.
	Total int
	// LinesOnly reports that games without a line were left out. Books post late,
	// so a future day usually has no lines at all, and hiding every game on it
	// would read as nothing being scheduled. The default only applies to a day
	// with at least one line that matches the rest of the query.
	LinesOnly bool
	// HiddenGames is how many games LinesOnly left out.
	HiddenGames int
	Conferences []repository.WeekConference
	// PrevDay and NextDay are the nearest days with games, nil when there is none,
	// so the pager skips the holiday break and the off-season.
	PrevDay *time.Time
	NextDay *time.Time
}

// SeasonOption is one season in the page's dropdown.
type SeasonOption struct {
	Value int
	Label string
}

// Service handles basketball game business logic.
type Service struct {
	gameRepo          *repository.GameRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	spreadOddsRepo    *repository.SpreadOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository
	pollRankRepo      *repository.PollRankRepository

	// location fixes the day boundaries the listing pages page through. Games
	// are stored as instants, so without an explicit zone a late evening tips
	// over into the next day's slate.
	location *time.Location

	// clock is what Today resolves against. See cfbdata.SyncService for why
	// this is a settable field.
	clock timeutil.Clock
}

// NewService creates a new basketball service.
func NewService(db *gorm.DB, loc *time.Location) *Service {
	if loc == nil {
		loc = time.UTC
	}

	return &Service{
		gameRepo:          repository.NewGameRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
		pollRankRepo:      repository.NewPollRankRepository(db),
		location:          loc,
	}
}

// SetClock overrides the time source. The zero value is time.Now, so only a
// test replaying a recorded response needs to call this.
func (s *Service) SetClock(now func() time.Time) {
	s.clock.Set(now)
}

// Today returns the start of the current calendar day in the app's timezone.
func (s *Service) Today() time.Time {
	return timeutil.StartOfDay(s.clock.Now(), s.location)
}

// ParseDate interprets a YYYY-MM-DD value from a query string as a calendar day
// in the app's timezone, falling back to today when it is absent or malformed.
func (s *Service) ParseDate(value string) time.Time {
	if value != "" {
		// ParseInLocation, not Parse: the latter would return midnight UTC and
		// shift the whole day window for anyone east or west of it.
		if parsed, err := time.ParseInLocation("2006-01-02", value, s.location); err == nil {
			return parsed
		}
	}
	return s.Today()
}

// GetDay returns one calendar day's games, narrowed by query.
func (s *Service) GetDay(date time.Time, query DayQuery) (*DayGames, error) {
	start := timeutil.StartOfDay(date, s.location)
	end := start.AddDate(0, 0, 1)

	// Read first, since the ranked filters need them. A failed read costs the ranks and
	// those filters, not the page.
	ranks, err := s.pollRankRepo.AsOf(s.SeasonOf(start), models.PollAP, timeutil.CalendarDayInUTC(start, s.location))
	if err != nil {
		slog.Error("failed to read basketball ranks", "error", err)
	}

	filter := query.Filter
	filter.BettableOnly = false
	filter.Location = s.location
	filter.RankedTeamIDs = make([]uuid.UUID, 0, len(ranks))
	for teamID := range ranks {
		filter.RankedTeamIDs = append(filter.RankedTeamIDs, teamID)
	}

	day := &DayGames{}
	if day.Total, err = s.gameRepo.CountGamesInRange(models.SportBasketball, start, end, repository.GameFilter{}); err != nil {
		return nil, err
	}

	matching, err := s.gameRepo.CountGamesInRange(models.SportBasketball, start, end, filter)
	if err != nil {
		return nil, err
	}
	if !query.AllGames {
		withLines := filter
		withLines.BettableOnly = true
		bettable, err := s.gameRepo.CountGamesInRange(models.SportBasketball, start, end, withLines)
		if err != nil {
			return nil, err
		}
		if bettable > 0 {
			filter = withLines
			day.LinesOnly = true
			day.HiddenGames = matching - bettable
		}
	}

	games, err := s.gameRepo.FindGamesInRange(models.SportBasketball, start, end, filter)
	if err != nil {
		return nil, err
	}
	if day.Games, err = s.attachOdds(games); err != nil {
		return nil, err
	}

	attachRanks(day.Games, ranks)
	s.attachConferences(day, start, end, query.Filter.Conferences)
	s.attachPager(day, start, end)
	return day, nil
}

// attachConferences fills in the filter's choices: the day's conferences, plus any
// selected one the day has no games for. The pager carries a filter from day to day,
// and leaving it off the list would read as unfiltered while the filter still applied.
func (s *Service) attachConferences(day *DayGames, start, end time.Time, selected []string) {
	conferences, err := s.gameRepo.FindConferencesInRange(models.SportBasketball, start, end, noConference)
	if err != nil {
		slog.Error("failed to list basketball conferences", "error", err)
	}
	for _, conference := range selected {
		listed := slices.ContainsFunc(conferences, func(c repository.WeekConference) bool {
			return c.Conference == conference
		})
		if !listed {
			conferences = append(conferences, repository.WeekConference{Conference: conference})
		}
	}
	slices.SortFunc(conferences, func(a, b repository.WeekConference) int {
		return cmp.Compare(a.Conference, b.Conference)
	})
	day.Conferences = conferences
}

// attachPager finds the nearest days with games on either side.
func (s *Service) attachPager(day *DayGames, start, end time.Time) {
	prev, ok, err := s.gameRepo.LastTipBefore(models.SportBasketball, start)
	if err != nil {
		slog.Error("failed to find the previous basketball day", "error", err)
	}
	if ok {
		prevDay := timeutil.StartOfDay(prev, s.location)
		day.PrevDay = &prevDay
	}
	next, ok, err := s.gameRepo.NextTipFrom(models.SportBasketball, end)
	if err != nil {
		slog.Error("failed to find the next basketball day", "error", err)
	}
	if ok {
		nextDay := timeutil.StartOfDay(next, s.location)
		day.NextDay = &nextDay
	}
}

// attachOdds decorates games with each market's primary line, in three batched
// queries rather than three per game: a day can hold 169 games. The result is
// already preloaded.
func (s *Service) attachOdds(games []models.Game) ([]GameWithOdds, error) {
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

	withOdds := make([]GameWithOdds, len(games))
	for i, game := range games {
		withOdds[i] = GameWithOdds{Game: game, Result: game.Result}
		if rows := moneyLine[game.ID]; len(rows) > 0 {
			withOdds[i].MoneyLine = &rows[0]
		}
		if rows := spread[game.ID]; len(rows) > 0 {
			withOdds[i].Spread = &rows[0]
		}
		if rows := overUnder[game.ID]; len(rows) > 0 {
			withOdds[i].OverUnder = &rows[0]
		}
	}
	return withOdds, nil
}

// attachRanks fills in each side's AP position as of the day.
func attachRanks(games []GameWithOdds, ranks map[uuid.UUID]int) {
	for i := range games {
		if rank, ok := ranks[games[i].Game.HomeTeamID]; ok {
			games[i].HomeRank = &rank
		}
		if rank, ok := ranks[games[i].Game.AwayTeamID]; ok {
			games[i].AwayRank = &rank
		}
	}
}

// Seasons lists the seasons holding games, newest first, labelled for display.
func (s *Service) Seasons() ([]SeasonOption, error) {
	seasons, err := s.gameRepo.FindSeasons(models.SportBasketball)
	if err != nil {
		return nil, err
	}

	options := make([]SeasonOption, len(seasons))
	for i, season := range seasons {
		options[i] = SeasonOption{Value: season, Label: models.BasketballSeasonLabel(season)}
	}
	return options, nil
}

// Location is the zone the day boundaries and the tip-off window are read in.
func (s *Service) Location() *time.Location { return s.location }

// SeasonOf is the season a calendar day belongs to.
func (s *Service) SeasonOf(date time.Time) int {
	return cbbdata.SeasonFor(date.In(s.location))
}

// DefaultDay is the day to open on: today if it has games, otherwise the next day
// that does, otherwise the last. Between seasons that means landing on games
// rather than on an empty page.
func (s *Service) DefaultDay() (time.Time, error) {
	return s.nearestGameDay(s.Today())
}

// DayInSeason is the day to open a season on: the nearest game day to today within
// it, which is the first day of a season yet to start and the last of one finished.
// The second return is false for a season holding no games.
func (s *Service) DayInSeason(season int) (time.Time, bool, error) {
	first, last, ok, err := s.gameRepo.SeasonSpan(models.SportBasketball, season)
	if err != nil || !ok {
		return time.Time{}, false, err
	}

	today := s.Today()
	firstDay := timeutil.StartOfDay(first, s.location)
	lastDay := timeutil.StartOfDay(last, s.location)
	switch {
	case today.Before(firstDay):
		return firstDay, true, nil
	case today.After(lastDay):
		return lastDay, true, nil
	}
	day, err := s.nearestGameDay(today)
	return day, err == nil, err
}

func (s *Service) nearestGameDay(day time.Time) (time.Time, error) {
	next, ok, err := s.gameRepo.NextTipFrom(models.SportBasketball, day)
	if err != nil {
		return time.Time{}, err
	}
	if ok {
		return timeutil.StartOfDay(next, s.location), nil
	}

	prev, ok, err := s.gameRepo.LastTipBefore(models.SportBasketball, day)
	if err != nil {
		return time.Time{}, err
	}
	if ok {
		return timeutil.StartOfDay(prev, s.location), nil
	}
	return day, nil
}
