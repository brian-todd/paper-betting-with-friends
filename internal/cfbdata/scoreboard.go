package cfbdata

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// is the monthly bill: each extra division costs another ~2,700 requests a
// month in the heart of the season. FBS is also the only division the feed carries betting lines for, and
// the games grid filters to it by default.
//
// The list is load-bearing beyond the fetch: ScoreboardState scopes the games
// it schedules against to these same divisions, so the two must be given the
// same value.
var DefaultScoreboardClassifications = []string{"fbs"}

// normalizeClassifications applies the default and folds case.
//
// Case matters more than it looks. The configured list is not only what gets
// fetched any more -- ResolveScoreboardState matches it against
// teams.classification to decide the polling rate, and that column is stored
// lowercase as CFBD reports it. CFB_SCOREBOARD_CLASSIFICATIONS=FBS would
// therefore fetch the right division while matching no team at all, which is
// not a degraded cadence but a stuck one: nothing ever reads as live, the
// scoreboard polls hourly through every slate of the season, and the job
// reports success the whole way. Folding it here rather than in config keeps
// that package free of any knowledge of how the feed divides the sport, which
// is why the default lives here too.
func normalizeClassifications(classifications []string) []string {
	if len(classifications) == 0 {
		return DefaultScoreboardClassifications
	}

	out := make([]string, 0, len(classifications))
	seen := make(map[string]bool, len(classifications))
	for _, c := range classifications {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return DefaultScoreboardClassifications
	}
	return out
}

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
	classifications = normalizeClassifications(classifications)

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
	// A kickoff that has moved is corrected first, and a failure to do it is
	// logged rather than returned: the status, the score and the clock below are
	// what this sync exists for, and losing a run of them over a start time
	// would be the worse trade.
	//
	// This matters more since the scoreboard started scheduling itself off
	// scheduled_at. A stale kickoff there is not only a wrong card and a
	// misplaced betting cutoff -- it decides when the job next polls, so a game
	// whose start time nothing corrects can go unwatched by the feed that would
	// have corrected it.
	//
	// Two kinds of absent time are skipped, and they are not the same kind. A
	// startTimeTBD game has a placeholder instant -- midnight of the day the
	// feed expects it on, not an unknown -- and storing that would put a
	// real-looking kickoff on a game nobody has scheduled. A missing or null
	// startDate is the zero time with the flag unset, so the flag does not
	// catch it; writing year 1 would read as a kickoff long past, which closes
	// betting and freezes every bet already placed until /games writes the row
	// again. Neither is an error worth logging: the row is simply left alone
	// for the feed that knows the answer.
	//
	// Note that only scheduled_at is corrected here. week_id is set from
	// /games' own week number and never derived from the date, so a kickoff
	// pushed across a week boundary would leave the game filed under the old
	// week until /games rewrites both. The endpoint takes no week parameter and
	// returns the current week, so that is a narrow case, but it is the reason
	// this does not try to do more.
	if !g.StartTimeTBD && !g.StartDate.IsZero() {
		if err := s.gameRepo.UpdateScheduledAt(gameID, g.StartDate); err != nil {
			s.logger.Error("failed to correct kickoff", "game", g.ID, "error", err)
		}
	}

	status, completed, ok := scoreboardStatus(g.Status)
	if !ok {
		// An unrecognised status is a feed change, not a game. Leaving the
		// stored status alone is the safe reading -- guessing at it could park
		// a live game on "final" and take it out of the bet slip.
		s.logger.Warn("ignoring unrecognised scoreboard status", "game", g.ID, "status", g.Status)
	} else if err := s.gameRepo.UpdateReportedStatus(gameID, status, completed); err != nil {
		return fmt.Errorf("updating status: %w", err)
	}

	if result, ok := scoreboardResult(gameID, g, s.clock.Now()); ok {
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
		Possession:         normalizePossession(g.Possession),
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

// normalizePossession keeps only the two values the feed sends and drops
// anything else.
//
// The column is bounded and the feed is not. "home" and "away" are the only
// values observed and the only ones anything reads, so storing whatever arrives
// would buy nothing and risk a string long enough to fail the row's upsert --
// which would cost the clock, the situation and the broadcast along with it,
// every run, for as long as the feed kept sending it.
func normalizePossession(possession *string) *string {
	if possession == nil {
		return nil
	}

	switch side := strings.ToLower(strings.TrimSpace(*possession)); side {
	case "home", "away":
		return &side
	default:
		return nil
	}
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
