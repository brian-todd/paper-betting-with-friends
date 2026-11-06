package cbbdata

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/syncerr"
	"github.com/google/uuid"
)

// SyncBoxScores fetches box scores only when a finished game is waiting for one. The
// check reads the database, so a run with nothing waiting spends no request, and it keeps
// no record of the last run, so a game a failed run missed is still waiting for the next.
// One request covers every waiting game.
//
// Only waiting games are written. The feed can answer for a game still being played, and
// a partial box under the heading "Box score" would be wrong in a way nothing on the page
// could show. A box that agrees with the score is never fetched again.
func (s *SyncService) SyncBoxScores(ctx context.Context) error {
	now := s.clock.Now()
	start, end := boxScoreWindow(now)

	waiting, err := s.gameTeamStatsRepo.GamesAwaitingBox(start, end)
	if err != nil {
		return fmt.Errorf("finding games awaiting box scores: %w", err)
	}
	if len(waiting) == 0 {
		s.logger.Debug("no games awaiting box scores")
		return nil
	}

	const layout = "2006-01-02T15:04:05.000Z"
	rows, err := s.client.GetGameTeamStats(ctx, start.Format(layout), end.Format(layout))
	if err != nil {
		return fmt.Errorf("syncing box scores: %w", err)
	}

	teamIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		teamIDs = append(teamIDs, row.TeamID)
	}
	teams, err := s.teamRepo.IDsByExternalID(models.SportBasketball, teamIDs)
	if err != nil {
		return fmt.Errorf("looking up box score teams: %w", err)
	}
	if len(rows) > 0 && len(teams) == 0 {
		return fmt.Errorf("storing box scores: none of the teams in %d rows are stored, so basketball teams need seeding", len(rows))
	}

	boxes := gameTeamStatsFrom(rows, waiting, teams, now)

	var failed syncerr.Tally
	for i := range boxes {
		if err := s.gameTeamStatsRepo.Upsert(&boxes[i]); err != nil {
			s.logger.Error("failed to store box score", "game", boxes[i].GameID, "team", boxes[i].TeamID, "error", err)
			failed.Add(err)
		}
	}
	s.logger.Info("synced box scores", "waiting", len(waiting), "rows", len(rows), "stored", len(boxes)-failed.Count(), "failed", failed.Count())
	return failed.Err("box scores")
}

// boxScoreWindow is whole UTC days from boxScoreLookback ago through today.
func boxScoreWindow(now time.Time) (start, end time.Time) {
	day := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return day(now.Add(-boxScoreLookback)), day(now.AddDate(0, 0, 1))
}

// gameTeamStatsFrom keeps the rows of waiting games whose teams we hold. Each team has
// its own row, non-Division I opponents included, so only the row's own side is read.
// Duplicates are dropped, since Upsert would only write one over the other.
func gameTeamStatsFrom(rows []APIGameTeamStats, games, teams map[int64]uuid.UUID, now time.Time) []models.GameTeamStats {
	type key struct{ game, team uuid.UUID }
	seen := make(map[key]bool, len(rows))

	stats := make([]models.GameTeamStats, 0, len(rows))
	for _, row := range rows {
		gameID, ok := games[row.GameID]
		if !ok {
			continue
		}
		teamID, ok := teams[row.TeamID]
		if !ok {
			continue
		}
		k := key{gameID, teamID}
		if seen[k] {
			continue
		}
		seen[k] = true
		stats = append(stats, models.GameTeamStats{GameID: gameID, TeamID: teamID, FetchedAt: now, Box: teamBoxFrom(row.TeamStats)})
	}
	return stats
}

// teamBoxFrom reads the counts both /stats/team/season and /games/teams report.
func teamBoxFrom(side APITeamSide) models.TeamBox {
	return models.TeamBox{
		Points:             countPtr(side.Points.Total),
		FGMade:             countPtr(side.FieldGoals.Made),
		FGAttempted:        countPtr(side.FieldGoals.Attempted),
		ThreeMade:          countPtr(side.ThreePoint.Made),
		ThreeAttempted:     countPtr(side.ThreePoint.Attempted),
		FTMade:             countPtr(side.FreeThrows.Made),
		FTAttempted:        countPtr(side.FreeThrows.Attempted),
		Rebounds:           countPtr(side.Rebounds.Total),
		OffRebounds:        countPtr(side.Rebounds.Offensive),
		DefRebounds:        countPtr(side.Rebounds.Defensive),
		Assists:            countPtr(side.Assists),
		Steals:             countPtr(side.Steals),
		Blocks:             countPtr(side.Blocks),
		Turnovers:          countPtr(side.Turnovers.Total),
		Fouls:              countPtr(side.Fouls.Total),
		PaintPoints:        countPtr(side.Points.InPaint),
		FastBreakPoints:    countPtr(side.Points.FastBreak),
		PointsOffTurnovers: countPtr(side.Points.OffTurnovers),
	}
}

// countPtr rounds a feed's number to a count. The feed types every count as a double.
func countPtr(v *float64) *int {
	if v == nil {
		return nil
	}
	count := int(math.Round(*v))
	return &count
}
