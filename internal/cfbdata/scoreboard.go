package cfbdata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// DefaultScoreboardClassifications is the divisions the live sync polls when
// nothing else is configured.
//
// FBS only, because the endpoint takes one division per call and the call count
// is the monthly bill: at a five-minute cadence each extra division costs
// another ~8,600 requests a month. FBS is also the only division the feed
// carries betting lines for, and the games grid filters to it by default.
var DefaultScoreboardClassifications = []string{"fbs"}

// SyncScoreboard refreshes the live state of the current week's games.
//
// It is the only source of a real football game status. /games reports whether
// a game is completed and nothing else, so everything between kickoff and the
// final whistle there is inferred from the clock; this endpoint reports the
// status, the period, the game clock and the score as they happen.
//
// What it deliberately does not do is settle bets. Finality is a claim about
// money, /games remains the feed that makes it, and EvaluateBetsForGame is
// still called from there alone -- so the worst a wrong scoreboard reading can
// do is mislabel a card until the next games sync corrects it.
//
// The odds on this feed are ignored for a related reason: they name no
// sportsbook, and the odds tables are keyed by one.
func (s *SyncService) SyncScoreboard(ctx context.Context, classifications []string) error {
	if len(classifications) == 0 {
		classifications = DefaultScoreboardClassifications
	}

	s.logger.Info("syncing scoreboard", "classifications", classifications)

	// A game shows up under more than one classification -- an FCS side visiting
	// an FBS one is returned by both calls -- and writing it twice would cost a
	// second round of updates for no new information.
	seen := make(map[int64]bool)

	var synced, live, unknown int
	for _, classification := range classifications {
		games, err := s.client.GetScoreboard(ctx, classification)
		if err != nil {
			return err
		}

		for _, g := range games {
			if seen[g.ID] {
				continue
			}
			seen[g.ID] = true

			game, err := s.gameRepo.FindByExternalID(g.ID, models.SportFootball)
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("looking up game %d: %w", g.ID, err)
				}
				// The scoreboard runs ahead of the schedule sync, and covers
				// divisions a seed may never have loaded. A game we do not have
				// is not an error, but the count is worth knowing: if it is
				// every game, the database was never seeded.
				unknown++
				continue
			}

			if err := s.applyScoreboardGame(game.ID, g); err != nil {
				s.logger.Error("failed to apply scoreboard state", "game", g.ID, "error", err)
				continue
			}

			synced++
			if g.Status == ScoreboardStatusInProgress {
				live++
			}
		}
	}

	s.logger.Info("synced scoreboard", "synced", synced, "live", live, "unknown_games", unknown)
	return nil
}

// applyScoreboardGame writes one scoreboard row across the three places its
// parts belong: the game's status, the score, and the live state.
func (s *SyncService) applyScoreboardGame(gameID uuid.UUID, g APIScoreboardGame) error {
	status, completed, ok := scoreboardStatus(g.Status)
	if !ok {
		// An unrecognised status is a feed change, not a game. Leaving the
		// stored status alone is the safe reading -- guessing at it could park
		// a live game on "final" and take it out of the bet slip.
		s.logger.Warn("ignoring unrecognised scoreboard status", "game", g.ID, "status", g.Status)
	} else if err := s.gameRepo.UpdateReportedStatus(gameID, status, completed); err != nil {
		return fmt.Errorf("updating status: %w", err)
	}

	if result, ok := scoreboardResult(gameID, g, time.Now()); ok {
		if err := s.gameResultRepo.Upsert(result); err != nil {
			return fmt.Errorf("upserting result: %w", err)
		}
	}

	if err := s.gameLiveStateRepo.Upsert(scoreboardLiveState(gameID, g)); err != nil {
		return fmt.Errorf("upserting live state: %w", err)
	}
	return nil
}

// scoreboardStatus maps the feed's status onto the stored one. The third return
// is false for a value the feed has not used before, which the caller treats as
// "leave the status alone" rather than as any particular state.
func scoreboardStatus(status string) (models.GameStatus, bool, bool) {
	switch status {
	case ScoreboardStatusScheduled:
		return models.GameStatusScheduled, false, true
	case ScoreboardStatusInProgress:
		return models.GameStatusInProgress, false, true
	case ScoreboardStatusCompleted:
		return models.GameStatusFinal, true, true
	default:
		return "", false, false
	}
}

// scoreboardResult builds the score row for a scoreboard game, reporting false
// when there is no score to write.
//
// A side that has not scored is reported as null rather than as zero, so for a
// game in progress one null and one number means nil-nil is a shutout so far
// and is read as zero. A completed game is held to the stricter rule /games
// uses -- both sides present or nothing is written -- because that row is what
// bet settlement later reads, and a half-arrived final is worse than none.
func scoreboardResult(gameID uuid.UUID, g APIScoreboardGame, now time.Time) (*models.GameResult, bool) {
	home, away := g.HomeTeam.Points, g.AwayTeam.Points

	switch g.Status {
	case ScoreboardStatusInProgress:
		if home == nil && away == nil {
			return nil, false
		}
	case ScoreboardStatusCompleted:
		if home == nil || away == nil {
			return nil, false
		}
	default:
		// Nothing has been played, so any points on the row are not a score.
		return nil, false
	}

	// FinalizedAt is what stops a bet settling against a score that is still
	// moving, so it is set only for a game the feed calls complete. The repository
	// keeps the first value written, so the games sync reaching the same game
	// later cannot push the timestamp forward.
	var finalizedAt *time.Time
	if g.Status == ScoreboardStatusCompleted {
		finalizedAt = &now
	}

	return &models.GameResult{
		GameID:         gameID,
		HomeScore:      intOrZero(home),
		AwayScore:      intOrZero(away),
		HomeLineScores: models.IntSlice(g.HomeTeam.LineScores),
		AwayLineScores: models.IntSlice(g.AwayTeam.LineScores),
		FinalizedAt:    finalizedAt,
	}, true
}

// scoreboardLiveState builds the live-state row for a scoreboard game.
func scoreboardLiveState(gameID uuid.UUID, g APIScoreboardGame) *models.GameLiveState {
	state := &models.GameLiveState{
		GameID:             gameID,
		Period:             g.Period,
		Clock:              g.Clock,
		Situation:          g.Situation,
		Possession:         g.Possession,
		LastPlay:           g.LastPlay,
		TV:                 strPtr(g.TV),
		HomeWinProbability: decimalPtr(g.HomeTeam.WinProbability),
	}

	if g.Weather != nil {
		state.WeatherDescription = g.Weather.Description
		state.Temperature = decimalPtr(g.Weather.Temperature)
		state.WindSpeed = decimalPtr(g.Weather.WindSpeed)
		state.WindDirection = g.Weather.WindDirection
	}

	return state
}

// intOrZero reads an absent count as none, which for points is zero.
func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// decimalPtr converts an optional float from the feed. Nothing here is money or
// odds -- it is temperatures and probabilities -- but decimal keeps what is
// stored equal to what was reported, which a float column would not.
func decimalPtr(v *float64) *decimal.Decimal {
	if v == nil {
		return nil
	}
	d := decimal.NewFromFloat(*v)
	return &d
}
