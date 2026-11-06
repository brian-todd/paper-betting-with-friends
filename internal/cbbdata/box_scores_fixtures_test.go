package cbbdata_test

import (
	"context"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
)

// boxScoreCapture is when /games/teams was recorded. The sync asks for whole UTC days
// around its clock, so this instant is what makes its request the captured one.
var boxScoreCapture = time.Date(2026, 11, 5, 13, 57, 9, 0, time.UTC)

// Level 2: real box scores through the real sync.
func TestBoxScoresFromRealGames(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	sync := cbbdata.NewSyncService(cbbdata.NewClientAt(base, ""), db)
	sync.SetClock(timeutil.Fixed(boxScoreCapture))
	if err := sync.SyncReferenceForTest(ctx); err != nil {
		t.Fatalf("syncing reference data: %v", err)
	}

	team := func(t *testing.T, externalID int64) models.Team {
		t.Helper()
		var found models.Team
		if err := db.First(&found, "sport = ? AND external_id = ?", models.SportBasketball, externalID).Error; err != nil {
			t.Fatalf("reading team %d: %v", externalID, err)
		}
		return found
	}
	game := func(t *testing.T, externalID, homeTeam, awayTeam int64, status models.GameStatus) *models.Game {
		t.Helper()
		return testdb.InsertGame(t, db, models.Game{
			ExternalID: &externalID, Sport: models.SportBasketball, Status: status, Season: 2027,
			ScheduledAt: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
			HomeTeamID:  team(t, homeTeam).ID, AwayTeamID: team(t, awayTeam).ID,
		})
	}

	const georgiaSouthern, ballState = 99, 19
	finished := game(t, 372633, georgiaSouthern, ballState, models.GameStatusFinal)
	finalizedAt := boxScoreCapture
	if err := db.Create(&models.GameResult{GameID: finished.ID, HomeScore: 67, AwayScore: 55, FinalizedAt: &finalizedAt}).Error; err != nil {
		t.Fatalf("inserting the result: %v", err)
	}

	if err := sync.SyncBoxScores(ctx); err != nil {
		t.Fatalf("SyncBoxScores: %v", err)
	}
	if n := fake.Refusals(); n > 0 {
		t.Fatalf("the fake upstream refused %d requests, so the sync's window is not the captured one", n)
	}

	read := func(t *testing.T, g *models.Game, teamExternalID int64) models.TeamBox {
		t.Helper()
		var row models.GameTeamStats
		if err := db.First(&row, "game_id = ? AND team_id = ?", g.ID, team(t, teamExternalID).ID).Error; err != nil {
			t.Fatalf("reading box score: %v", err)
		}
		return row.Box
	}

	// Every figure is checked, on both sides, because each column is a chance to read
	// the wrong field and the values in this game differ enough to notice.
	for _, side := range []struct {
		name string
		team int64
		want map[string]int
	}{
		{"Georgia Southern", georgiaSouthern, map[string]int{
			"points": 67, "fgm": 20, "fga": 53, "3pm": 5, "3pa": 19, "ftm": 22, "fta": 36,
			"reb": 39, "oreb": 11, "dreb": 28, "ast": 10, "stl": 9, "blk": 4, "to": 10, "pf": 16,
			"paint": 28, "fastbreak": 22, "offto": 18,
		}},
		{"Ball State", ballState, map[string]int{
			"points": 55, "fgm": 16, "fga": 59, "3pm": 5, "3pa": 37, "ftm": 18, "fta": 25,
			"reb": 47, "oreb": 15, "dreb": 32, "ast": 5, "stl": 7, "blk": 2, "to": 15, "pf": 23,
			"paint": 20, "fastbreak": 10, "offto": 14,
		}},
	} {
		t.Run(side.name+" reads as the feed reported it", func(t *testing.T) {
			b := read(t, finished, side.team)
			for label, got := range map[string]*int{
				"points": b.Points, "fgm": b.FGMade, "fga": b.FGAttempted, "3pm": b.ThreeMade, "3pa": b.ThreeAttempted,
				"ftm": b.FTMade, "fta": b.FTAttempted, "reb": b.Rebounds, "oreb": b.OffRebounds, "dreb": b.DefRebounds,
				"ast": b.Assists, "stl": b.Steals, "blk": b.Blocks, "to": b.Turnovers, "pf": b.Fouls,
				"paint": b.PaintPoints, "fastbreak": b.FastBreakPoints, "offto": b.PointsOffTurnovers,
			} {
				if got == nil || *got != side.want[label] {
					t.Errorf("%s = %v, want %d", label, got, side.want[label])
				}
			}
		})
	}

	t.Run("the real box stops the game waiting", func(t *testing.T) {
		waiting, err := repository.NewGameTeamStatsRepository(db).GamesAwaitingBox(
			boxScoreCapture.AddDate(0, 0, -3), boxScoreCapture.AddDate(0, 0, 1))
		if err != nil {
			t.Fatalf("GamesAwaitingBox: %v", err)
		}
		if len(waiting) != 0 {
			t.Errorf("still waiting: %v", waiting)
		}
	})

	t.Run("only the stored final game is written", func(t *testing.T) {
		var n int64
		if err := db.Model(&models.GameTeamStats{}).Count(&n).Error; err != nil {
			t.Fatalf("counting: %v", err)
		}
		if n != 2 {
			t.Errorf("%d rows, want the 2 sides of the one game stored", n)
		}
	})
}
