package cbbdata

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
)

func TestBoxScoreWindow(t *testing.T) {
	start, end := boxScoreWindow(time.Date(2026, 12, 5, 23, 30, 0, 0, time.UTC))
	wantStart := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("window = %s to %s", start, end)
	}
}

func boxSide(points float64) APITeamSide {
	var side APITeamSide
	side.Points.Total = &points
	return side
}

func TestGameTeamStatsFrom(t *testing.T) {
	game, team, opponent := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 12, 5, 18, 0, 0, 0, time.UTC)

	points := func(t *testing.T, stats []models.GameTeamStats) map[uuid.UUID]int {
		t.Helper()
		got := make(map[uuid.UUID]int, len(stats))
		for _, row := range stats {
			if row.GameID != game || !row.FetchedAt.Equal(now) {
				t.Errorf("row = %+v", row)
			}
			got[row.TeamID] = *row.Box.Points
		}
		return got
	}

	games := map[int64]uuid.UUID{1: game}
	teams := map[int64]uuid.UUID{10: team, 20: opponent}

	t.Run("each row is its own team's side", func(t *testing.T) {
		got := points(t, gameTeamStatsFrom([]APIGameTeamStats{
			{GameID: 1, TeamID: 10, TeamStats: boxSide(80)},
			{GameID: 1, TeamID: 20, TeamStats: boxSide(70)},
		}, games, teams, now))
		if len(got) != 2 || got[team] != 80 || got[opponent] != 70 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("a game not waiting or a team we do not hold is left out", func(t *testing.T) {
		got := gameTeamStatsFrom([]APIGameTeamStats{
			{GameID: 2, TeamID: 10, TeamStats: boxSide(80)},
			{GameID: 1, TeamID: 99, TeamStats: boxSide(70)},
		}, games, teams, now)
		if len(got) != 0 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a repeated row is written once", func(t *testing.T) {
		got := gameTeamStatsFrom([]APIGameTeamStats{
			{GameID: 1, TeamID: 10, TeamStats: boxSide(80)},
			{GameID: 1, TeamID: 10, TeamStats: boxSide(80)},
		}, games, teams, now)
		if len(got) != 1 {
			t.Errorf("got %+v", got)
		}
	})
}

// A count the feed leaves null stays unknown rather than becoming a zero.
func TestTeamBoxFromDecodesTheFeedsShape(t *testing.T) {
	var row APIGameTeamStats
	body := `{"gameId":7,"teamId":10,"teamStats":{
		"assists":15,"blocks":3,"steals":6,"possessions":70,
		"fieldGoals":{"made":27,"attempted":60,"pct":45},
		"threePointFieldGoals":{"made":9,"attempted":24,"pct":37.5},
		"freeThrows":{"made":10,"attempted":14,"pct":71.4},
		"rebounds":{"offensive":9,"defensive":26,"total":35},
		"turnovers":{"total":11,"teamTotal":1},
		"fouls":{"total":17,"technical":0,"flagrant":0},
		"points":{"total":73,"inPaint":30,"offTurnovers":14,"fastBreak":null,"largestLead":12}
	}}`
	if err := json.Unmarshal([]byte(body), &row); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	box := teamBoxFrom(row.TeamStats)
	for label, tt := range map[string]struct {
		got  *int
		want int
	}{
		"points": {box.Points, 73}, "fg": {box.FGMade, 27}, "fga": {box.FGAttempted, 60},
		"three": {box.ThreeMade, 9}, "threes attempted": {box.ThreeAttempted, 24},
		"ft": {box.FTMade, 10}, "fta": {box.FTAttempted, 14},
		"rebounds": {box.Rebounds, 35}, "offensive": {box.OffRebounds, 9}, "defensive": {box.DefRebounds, 26},
		"assists": {box.Assists, 15}, "steals": {box.Steals, 6}, "blocks": {box.Blocks, 3},
		"turnovers": {box.Turnovers, 11}, "fouls": {box.Fouls, 17},
		"paint": {box.PaintPoints, 30}, "off turnovers": {box.PointsOffTurnovers, 14},
	} {
		if tt.got == nil || *tt.got != tt.want {
			t.Errorf("%s = %v, want %d", label, tt.got, tt.want)
		}
	}
	if box.FastBreakPoints != nil {
		t.Errorf("a null count became %d", *box.FastBreakPoints)
	}
}

func TestSyncBoxScores(t *testing.T) {
	db := testdb.Open(t)
	now := time.Date(2026, 12, 5, 18, 0, 0, 0, time.UTC)

	var rows []APIGameTeamStats
	var status = http.StatusOK
	var requests int
	c := newTestClient(t, func(w http.ResponseWriter, req *http.Request) {
		requests++
		if req.URL.Path != "/games/teams" {
			t.Errorf("unexpected path %s", req.URL.Path)
		}
		if got := req.URL.Query().Get("startDateRange"); got != "2026-12-02T00:00:00.000Z" {
			t.Errorf("startDateRange = %q", got)
		}
		if got := req.URL.Query().Get("endDateRange"); got != "2026-12-06T00:00:00.000Z" {
			t.Errorf("endDateRange = %q", got)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(rows)
	})
	sync := NewSyncService(c, db)
	sync.SetClock(func() time.Time { return now })

	run := func(t *testing.T) int {
		t.Helper()
		before := requests
		if err := sync.SyncBoxScores(context.Background()); err != nil {
			t.Fatalf("SyncBoxScores: %v", err)
		}
		return requests - before
	}

	external := func(v int64) *int64 { return &v }
	home := testdb.InsertTeam(t, db, "")
	away := testdb.InsertTeam(t, db, "")
	if err := db.Model(&models.Team{}).Where("id = ?", home.ID).Updates(map[string]any{"external_id": 10, "sport": models.SportBasketball}).Error; err != nil {
		t.Fatalf("updating team: %v", err)
	}
	if err := db.Model(&models.Team{}).Where("id = ?", away.ID).Updates(map[string]any{"external_id": 20, "sport": models.SportBasketball}).Error; err != nil {
		t.Fatalf("updating team: %v", err)
	}
	game := func(id int64, status models.GameStatus) *models.Game {
		return testdb.InsertGame(t, db, models.Game{
			ExternalID: external(id), Sport: models.SportBasketball, Status: status,
			ScheduledAt: now.Add(-6 * time.Hour), HomeTeamID: home.ID, AwayTeamID: away.ID,
		})
	}
	finishedGame := func(id int64, homeScore, awayScore int) *models.Game {
		t.Helper()
		g := game(id, models.GameStatusFinal)
		if err := db.Create(&models.GameResult{GameID: g.ID, HomeScore: homeScore, AwayScore: awayScore, FinalizedAt: &now}).Error; err != nil {
			t.Fatalf("inserting result: %v", err)
		}
		return g
	}
	stored := func(t *testing.T, g *models.Game) int64 {
		t.Helper()
		var n int64
		if err := db.Model(&models.GameTeamStats{}).Where("game_id = ?", g.ID).Count(&n).Error; err != nil {
			t.Fatalf("counting: %v", err)
		}
		return n
	}
	homePoints := func(t *testing.T, g *models.Game) int {
		t.Helper()
		var row models.GameTeamStats
		if err := db.First(&row, "game_id = ? AND team_id = ?", g.ID, home.ID).Error; err != nil {
			t.Fatalf("reading: %v", err)
		}
		return *row.Box.Points
	}

	live := game(2, models.GameStatusInProgress)
	rows = []APIGameTeamStats{
		{GameID: 1, TeamID: 10, TeamStats: boxSide(80)},
		{GameID: 1, TeamID: 20, TeamStats: boxSide(70)},
		{GameID: 2, TeamID: 10, TeamStats: boxSide(40)},
		{GameID: 2, TeamID: 20, TeamStats: boxSide(38)},
	}

	t.Run("nothing finished spends no request", func(t *testing.T) {
		if n := run(t); n != 0 {
			t.Errorf("%d requests with no finished game", n)
		}
	})

	finished := finishedGame(1, 80, 70)

	t.Run("a failed request leaves the game waiting for the next run", func(t *testing.T) {
		status = http.StatusBadGateway
		err := sync.SyncBoxScores(context.Background())
		status = http.StatusOK
		if err == nil {
			t.Fatal("a 502 reported success")
		}
		if n := stored(t, finished); n != 0 {
			t.Fatalf("a failed run stored %d rows", n)
		}
	})

	t.Run("a finished game is fetched and one still being played is not", func(t *testing.T) {
		if n := run(t); n != 1 {
			t.Errorf("%d requests, want one for the whole window", n)
		}
		if n := stored(t, finished); n != 2 {
			t.Errorf("finished game has %d rows, want 2", n)
		}
		if n := stored(t, live); n != 0 {
			t.Errorf("a live game has %d rows", n)
		}
	})

	t.Run("a box that agrees spends no more requests", func(t *testing.T) {
		if n := run(t); n != 0 {
			t.Errorf("%d requests with every finished game complete", n)
		}
	})

	t.Run("a box that disagrees is fetched again and replaced, and only it", func(t *testing.T) {
		corrected := finishedGame(3, 90, 60)
		rows = []APIGameTeamStats{
			{GameID: 3, TeamID: 10, TeamStats: boxSide(55)},
			{GameID: 3, TeamID: 20, TeamStats: boxSide(60)},
		}
		if n := run(t); n != 1 {
			t.Fatalf("%d requests for a game with no box", n)
		}

		rows = []APIGameTeamStats{
			{GameID: 1, TeamID: 10, TeamStats: boxSide(81)},
			{GameID: 3, TeamID: 10, TeamStats: boxSide(90)},
		}
		if n := run(t); n != 1 {
			t.Fatalf("%d requests for a box that disagrees, want a retry", n)
		}
		if got := homePoints(t, corrected); got != 90 {
			t.Errorf("points = %d, want the correction", got)
		}
		if got := homePoints(t, finished); got != 80 {
			t.Errorf("a game no longer waiting was rewritten to %d", got)
		}
		if n := run(t); n != 0 {
			t.Errorf("%d requests after the correction", n)
		}
	})

	t.Run("rows naming no stored team say the teams need seeding", func(t *testing.T) {
		finishedGame(4, 75, 65)
		rows = []APIGameTeamStats{{GameID: 4, TeamID: 777}}
		if err := sync.SyncBoxScores(context.Background()); err == nil {
			t.Error("a run that could place nothing reported success")
		}
	})
}
