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
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SyncScoreboard refreshes the live state of the games the feed currently
// covers: their status, score, clock and broadcast.
//
// One request covers the whole slate, non-Division I opponents included, from
// about a day back to about a day ahead. Football takes one request per
// division; this endpoint has no such parameter to pay for.
//
// It does not settle bets. It writes finalized_at and stops, and the
// bet-settlement sweep pays out from there, which is the same split football
// uses. The odds on this feed are ignored for football's reason: they name no
// sportsbook, and the odds tables are keyed by one.
func (s *SyncService) SyncScoreboard(ctx context.Context) error {
	s.logger.Info("syncing scoreboard")

	games, err := s.client.GetScoreboard(ctx)
	if err != nil {
		return err
	}

	externalIDs := make([]int64, len(games))
	for i, g := range games {
		externalIDs[i] = g.ID
	}
	byExternalID, err := s.gameRepo.IDsByExternalID(models.SportBasketball, externalIDs)
	if err != nil {
		return fmt.Errorf("looking up scoreboard games: %w", err)
	}

	var synced, live, unknown int
	for _, g := range games {
		gameID, ok := byExternalID[g.ID]
		if !ok {
			// The feed can run ahead of the schedule sync. It is worth counting,
			// because if it is every game, the database was never seeded.
			unknown++
			continue
		}

		if err := s.applyScoreboardGame(gameID, g); err != nil {
			s.logger.Error("failed to apply scoreboard state", "game", g.ID, "error", err)
			continue
		}

		synced++
		if g.Status == scoreboardStatusInProgress {
			live++
		}
	}

	s.logger.Info("synced scoreboard", "synced", synced, "live", live, "unknown_games", unknown)
	return nil
}

// applyScoreboardGame writes one scoreboard row across the places its parts
// belong: the kickoff, the game's status, the score and the live state.
func (s *SyncService) applyScoreboardGame(gameID uuid.UUID, g APIScoreboardGame) error {
	// A failed correction is logged rather than returned, because the status and
	// the score below are what this sync exists for. A TBD game carries a
	// placeholder instant and a missing startDate decodes to year 1, so neither
	// is a kickoff worth writing.
	if !g.StartTimeTBD && !g.StartDate.IsZero() {
		if err := s.gameRepo.UpdateScheduledAt(gameID, g.StartDate); err != nil {
			s.logger.Error("failed to correct tip-off", "game", g.ID, "error", err)
		}
	}

	if status, completed, ok := s.scoreboardStatusFor(g); ok {
		if err := s.gameRepo.UpdateReportedStatus(gameID, status, completed); err != nil {
			return fmt.Errorf("updating status: %w", err)
		}
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

// scoreboardStatusFor maps the feed's status onto the stored one. The final
// return is false when the stored status should be left alone.
//
// A "final" the score cannot back up is one of those cases, and so is a status
// the feed has not used before, since guessing could park a live game on final
// and take it out of the bet slip. /games is still running, so a wrong refusal
// here only costs time.
func (s *SyncService) scoreboardStatusFor(g APIScoreboardGame) (models.GameStatus, bool, bool) {
	switch g.Status {
	case scoreboardStatusScheduled:
		return models.GameStatusScheduled, false, true
	case scoreboardStatusInProgress:
		return models.GameStatusInProgress, false, true
	case scoreboardStatusFinal:
		if !finalIsCredible(g) {
			s.logger.Warn("ignoring a final the score cannot back up",
				"game", g.ID, "home_points", g.HomeTeam.Points, "away_points", g.AwayTeam.Points)
			return "", false, false
		}
		return models.GameStatusFinal, true, true
	case scoreboardStatusPostponed, scoreboardStatusCancelled:
		// UpdateReportedStatus has no edge to either. /games writes them.
		return "", false, false
	default:
		s.logger.Warn("ignoring unrecognised scoreboard status", "game", g.ID, "status", g.Status)
		return "", false, false
	}
}

// finalIsCredible reports whether a game the feed calls final has a score that
// can be final.
//
// Both sides have to be present, and they cannot be level: basketball has no
// ties, and none of the ~6,300 finals in the captured 2025-26 season is one. A
// level "final" is the feed being wrong, most plausibly at the end of regulation
// with overtime still to come, and a spread or total settled on it pays the
// wrong people with nothing to retry it.
func finalIsCredible(g APIScoreboardGame) bool {
	home, away := g.HomeTeam.Points, g.AwayTeam.Points
	return home != nil && away != nil && *home != *away
}

// scoreboardResult builds the score row for a scoreboard game, reporting false
// when there is no score to write.
//
// The status decides this, not the presence of points: the feed reports every
// scheduled game as 0-0, so a row written from points alone would put a score on
// a game nobody has played.
func scoreboardResult(gameID uuid.UUID, g APIScoreboardGame, now time.Time) (*models.GameResult, bool) {
	home, away := g.HomeTeam.Points, g.AwayTeam.Points

	var finalizedAt *time.Time
	switch g.Status {
	case scoreboardStatusInProgress:
		if home == nil && away == nil {
			return nil, false
		}
	case scoreboardStatusFinal:
		// An incredible final with a score is still the latest score, so it is
		// written as provisional. GameResultRepository.Upsert refuses to let that
		// overwrite a result already finalized.
		if home == nil || away == nil {
			return nil, false
		}
		if finalIsCredible(g) {
			finalizedAt = &now
		}
	default:
		return nil, false
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

// scoreboardLiveState builds the live-state row for a scoreboard game. This feed
// reports only the clock and the broadcast; football's situation, possession,
// weather and win probability have no basketball counterpart here.
func scoreboardLiveState(gameID uuid.UUID, g APIScoreboardGame) *models.GameLiveState {
	return &models.GameLiveState{
		GameID: gameID,
		Period: g.Period,
		Clock:  g.Clock,
		TV:     strPtr(strings.TrimSpace(g.TV)),
	}
}

// ScoreboardState returns what the games table says about the scoreboard's
// schedule right now.
func (s *SyncService) ScoreboardState(now time.Time) ScoreboardState {
	return ResolveScoreboardState(s.gameRepo, s.logger, now)
}

// ResolveScoreboardState answers the two questions the scoreboard cadence is
// decided from.
//
// It takes a repository rather than hanging off SyncService alone because the
// admin page shows the same two facts and has no API client to build a
// SyncService with. One implementation means the page reports what the scheduler
// acted on.
//
// A database error reads as Active, which is the cheap side of the trade: a blip
// mid-slate wastes a few requests, where the other answer drops live scores to
// hourly. It is also the failure that hides best, since the job goes on looking
// healthy, so both paths log before returning it.
func ResolveScoreboardState(gameRepo *repository.GameRepository, logger *slog.Logger, now time.Time) ScoreboardState {
	active, err := gameRepo.HasActiveBasketballGames(now, maxGameDuration)
	if err != nil {
		logger.Error("failed to resolve active games for scoreboard schedule", "error", err)
		return ScoreboardState{Active: true}
	}

	state := ScoreboardState{Active: active}

	tip, err := gameRepo.NextBasketballTip(now)
	switch {
	case err == nil:
		state.NextTip = &tip
	case errors.Is(err, gorm.ErrRecordNotFound):
		// The off-season, or a season not seeded yet. Both wait at the idle rate.
	default:
		logger.Error("failed to resolve next tip-off for scoreboard schedule", "error", err)
		return ScoreboardState{Active: true}
	}

	logger.Debug("resolved scoreboard schedule state", "active", state.Active, "next_tip", state.NextTip)
	return state
}

// intOrZero reads an absent count as none, which for points is zero.
func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
