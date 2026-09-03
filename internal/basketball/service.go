package basketball

import (
	"sort"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
	"gorm.io/gorm"
)

// GameWithOdds contains a game with its result and primary odds for grid display.
type GameWithOdds struct {
	Game      models.Game
	Result    *models.GameResult
	MoneyLine *models.MoneyLineOdds
	Spread    *models.SpreadOdds
	OverUnder *models.OverUnderOdds
}

// Service handles basketball game business logic.
type Service struct {
	gameRepo          *repository.GameRepository
	gameResultRepo    *repository.GameResultRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	spreadOddsRepo    *repository.SpreadOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository

	// location fixes the day boundaries the listing pages page through. Games
	// are stored as instants, so without an explicit zone a late evening tips
	// over into the next day's slate.
	location *time.Location
}

// NewService creates a new basketball service.
func NewService(db *gorm.DB, loc *time.Location) *Service {
	if loc == nil {
		loc = time.UTC
	}

	return &Service{
		gameRepo:          repository.NewGameRepository(db),
		gameResultRepo:    repository.NewGameResultRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
		location:          loc,
	}
}

// Today returns the start of the current calendar day in the app's timezone.
func (s *Service) Today() time.Time {
	return timeutil.StartOfDay(time.Now(), s.location)
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

// GetGamesForDate returns basketball games for a single day, optionally filtered by search.
func (s *Service) GetGamesForDate(date time.Time, search string) ([]GameWithOdds, error) {
	start := timeutil.StartOfDay(date, s.location)
	end := start.AddDate(0, 0, 1)

	games, err := s.gameRepo.FindByDateRangeAndSport(models.SportBasketball, start, end)
	if err != nil {
		return nil, err
	}

	// Filter by search if provided.
	if search != "" {
		query := strings.ToLower(search)
		var filtered []models.Game
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.HomeTeam.Name), query) ||
				strings.Contains(strings.ToLower(g.AwayTeam.Name), query) ||
				strings.Contains(strings.ToLower(g.HomeTeam.Abbreviation), query) ||
				strings.Contains(strings.ToLower(g.AwayTeam.Abbreviation), query) {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}

	gamesWithOdds := s.loadOdds(games)
	sortGames(gamesWithOdds)
	return gamesWithOdds, nil
}

func (s *Service) loadOdds(games []models.Game) []GameWithOdds {
	gamesWithOdds := make([]GameWithOdds, len(games))
	for i, game := range games {
		gwo := GameWithOdds{Game: game}

		result, err := s.gameResultRepo.FindByGameID(game.ID)
		if err == nil {
			gwo.Result = result
		}

		moneyLine, err := s.moneyLineOddsRepo.FindBookLinesByGame(game.ID)
		if err == nil && len(moneyLine) > 0 {
			gwo.MoneyLine = &moneyLine[0]
		}

		spread, err := s.spreadOddsRepo.FindBookLinesByGame(game.ID)
		if err == nil && len(spread) > 0 {
			gwo.Spread = &spread[0]
		}

		overUnder, err := s.overUnderOddsRepo.FindBookLinesByGame(game.ID)
		if err == nil && len(overUnder) > 0 {
			gwo.OverUnder = &overUnder[0]
		}

		gamesWithOdds[i] = gwo
	}
	return gamesWithOdds
}

func sortGames(games []GameWithOdds) {
	sort.Slice(games, func(i, j int) bool {
		priorityOf := func(s models.GameStatus) int {
			switch s {
			case models.GameStatusInProgress:
				return 0
			case models.GameStatusScheduled:
				return 1
			default:
				return 2
			}
		}

		prioI, prioJ := priorityOf(games[i].Game.Status), priorityOf(games[j].Game.Status)
		if prioI != prioJ {
			return prioI < prioJ
		}
		return games[i].Game.ScheduledAt.Before(games[j].Game.ScheduledAt)
	})
}
