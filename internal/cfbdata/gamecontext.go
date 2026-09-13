package cfbdata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SyncGameContext refreshes the pre-game context that is keyed on a game rather
// than on a team: the provider's pre-game win probability, and the kickoff
// forecast.
//
// Two requests. It is a separate job from SyncTeamStats rather than two more
// steps inside it because the two halves fail differently: a team-season
// resource that comes back empty means the provider has stopped publishing,
// while a game-keyed one that comes back empty usually means the season has not
// started or nobody has posted a line yet. Folding them together would make one
// of those two readings impossible to act on.
func (s *SyncService) SyncGameContext(ctx context.Context, year int) error {
	s.logger.Info("syncing game context", "season", year)

	var errs []error
	if err := s.syncPregameWinProbabilities(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("pregame win probability: %w", err))
	}
	if err := s.syncGameForecasts(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("forecasts: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	s.logger.Info("synced game context", "season", year)
	return nil
}

func (s *SyncService) syncPregameWinProbabilities(ctx context.Context, year int) error {
	payload, err := s.client.GetPregameWinProbabilities(ctx, year)
	if err != nil {
		return err
	}

	externalIDs := make([]int64, 0, len(payload))
	for _, p := range payload {
		externalIDs = append(externalIDs, p.GameID)
	}

	games, err := s.gameRepo.IDsByExternalID(models.SportFootball, externalIDs)
	if err != nil {
		return fmt.Errorf("resolving games: %w", err)
	}

	rows := pregameWinProbabilitiesFrom(payload, games)
	fetchedAt := time.Now()
	written := 0
	for _, row := range rows {
		row.FetchedAt = fetchedAt
		if err := s.gamePregameWPRepo.Upsert(&row); err != nil {
			s.logger.Error("failed to upsert pregame win probability", "game", row.GameID, "error", err)
			continue
		}
		written++
	}

	// No errNoRows guard here, unlike the team-season resources. An empty
	// response is a legitimate state for this endpoint rather than a sign of a
	// bad one: the metric is derived from a posted line, so before a season
	// opens there is nothing to publish, and failing the job every summer would
	// train whoever reads the admin page to ignore it.
	s.logger.Info("synced pregame win probabilities", "published", len(payload), "written", written)
	return nil
}

func (s *SyncService) syncGameForecasts(ctx context.Context, year int) error {
	payload, err := s.client.GetGameWeather(ctx, year)
	if err != nil {
		return err
	}

	// Only games still to be played. The feed returns every week the season has
	// reached -- 3,235 rows for a completed 2025 -- and a forecast for a game
	// that has already kicked off is of no use to anybody: the live strip owns
	// the weather from kickoff on. Dropping them here keeps the daily write
	// bounded at the few hundred games actually ahead of us.
	upcoming := make([]APIGameWeather, 0, len(payload))
	externalIDs := make([]int64, 0, len(payload))
	now := time.Now()
	for _, w := range payload {
		if !w.StartTime.After(now) {
			continue
		}
		upcoming = append(upcoming, w)
		externalIDs = append(externalIDs, w.ID)
	}

	games, err := s.gameRepo.IDsByExternalID(models.SportFootball, externalIDs)
	if err != nil {
		return fmt.Errorf("resolving games: %w", err)
	}

	rows := gameForecastsFrom(upcoming, games)
	fetchedAt := time.Now()
	written := 0
	for _, row := range rows {
		row.FetchedAt = fetchedAt
		if err := s.gameForecastRepo.Upsert(&row); err != nil {
			s.logger.Error("failed to upsert game forecast", "game", row.GameID, "error", err)
			continue
		}
		written++
	}

	// As above: out of season every game is in the past and this legitimately
	// writes nothing.
	s.logger.Info("synced game forecasts", "published", len(payload), "upcoming", len(upcoming), "written", written)
	return nil
}

// pregameWinProbabilitiesFrom maps a /metrics/wp/pregame payload onto rows,
// dropping games the database does not have.
func pregameWinProbabilitiesFrom(payload []APIPregameWP, games map[int64]uuid.UUID) []models.GamePregameWinProbability {
	rows := make([]models.GamePregameWinProbability, 0, len(payload))
	for _, p := range payload {
		// Both columns are NOT NULL, and neither half is worth having alone: a
		// probability with no line cannot be checked against the market, and a
		// line with no probability is what the odds tables already hold.
		if p.HomeWinProbability == nil || p.Spread == nil {
			continue
		}

		gameID, ok := games[p.GameID]
		if !ok {
			continue
		}

		rows = append(rows, models.GamePregameWinProbability{
			GameID:             gameID,
			HomeWinProbability: decimal.NewFromFloat(*p.HomeWinProbability),
			Spread:             decimal.NewFromFloat(*p.Spread),
		})
	}
	return rows
}

// gameForecastsFrom maps a /games/weather payload onto rows.
//
// Indoor games are kept rather than skipped. The row is what lets the page know
// to say nothing: skipping it would be indistinguishable from never having
// fetched a forecast, and the next reader to add a "no forecast yet" placeholder
// would put one on every dome in the country.
func gameForecastsFrom(payload []APIGameWeather, games map[int64]uuid.UUID) []models.GameForecast {
	rows := make([]models.GameForecast, 0, len(payload))
	for _, w := range payload {
		gameID, ok := games[w.ID]
		if !ok {
			continue
		}

		row := models.GameForecast{
			GameID:               gameID,
			Indoors:              w.GameIndoors,
			Temperature:          decimalPtr(w.Temperature),
			Humidity:             w.Humidity,
			Precipitation:        decimalPtr(w.Precipitation),
			Snowfall:             decimalPtr(w.Snowfall),
			WindSpeed:            decimalPtr(w.WindSpeed),
			WeatherConditionCode: w.WeatherConditionCode,
		}

		// Code 0 is the feed's way of saying it has no label yet, and it always
		// arrives with a null condition. Normalising the empty string to nil as
		// well keeps the template's {{with}} honest whichever way it comes.
		if w.WeatherCondition != nil && *w.WeatherCondition != "" {
			row.WeatherCondition = w.WeatherCondition
		}

		rows = append(rows, row)
	}
	return rows
}
