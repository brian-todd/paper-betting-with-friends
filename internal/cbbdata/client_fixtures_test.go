package cbbdata_test

import (
	"context"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
)

// fixtureSeason is the season the committed captures cover, and the window is
// the first month SeedAll chunks it into. Both have to match what the capture
// script asked for; the fixture server's 500 says so if they drift.
const (
	fixtureSeason = 2026
	windowStart   = "2025-11-01T00:00:00.000Z"
	windowEnd     = "2025-12-01T00:00:00.000Z"
)

func client(t *testing.T) *cbbdata.Client {
	t.Helper()
	base, _, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fixture server: %v", err)
	}
	t.Cleanup(stop)
	return cbbdata.NewClientAt(base, "")
}

// Level 1 for basketball. It rides along with football because the client has
// the same shape, so the cost is an hour, and because this is the only feed
// that reports a status of its own rather than having one inferred from the
// clock -- including "postponed", which nothing else in the system produces.
func TestClientDecodesEveryCapturedEndpoint(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	season := fixtureSeason
	start, end := windowStart, windowEnd

	t.Run("venues", func(t *testing.T) {
		venues, err := c.GetVenues(ctx)
		if err != nil {
			t.Fatalf("GetVenues: %v", err)
		}
		if len(venues) == 0 {
			t.Fatal("no venues decoded")
		}
	})

	t.Run("teams", func(t *testing.T) {
		teams, err := c.GetTeams(ctx)
		if err != nil {
			t.Fatalf("GetTeams: %v", err)
		}
		if len(teams) == 0 {
			t.Fatal("no teams decoded")
		}
		var named int
		for _, team := range teams {
			if team.School != "" {
				named++
			}
		}
		if named == 0 {
			t.Errorf("decoded %d teams and not one has a school", len(teams))
		}
	})

	t.Run("games", func(t *testing.T) {
		games, err := c.GetGames(ctx, cbbdata.GameQueryOpts{
			Season: &season, StartDateRange: &start, EndDateRange: &end,
		})
		if err != nil {
			t.Fatalf("GetGames: %v", err)
		}
		if len(games) == 0 {
			t.Fatal("no games decoded")
		}

		// Basketball is the one feed that reports a status instead of having
		// one inferred from the kickoff, so a capture with a single status
		// cannot exercise the mapping that reads it.
		statuses := map[string]int{}
		var zeroStart, withPoints int
		for _, g := range games {
			statuses[g.Status]++
			if g.StartDate.IsZero() {
				zeroStart++
			}
			if g.HomePoints != nil && g.AwayPoints != nil {
				withPoints++
			}
		}
		if len(statuses) < 2 {
			t.Errorf("every game in the capture is %v; the status mapping is untested by it", statuses)
		}
		if zeroStart > 0 {
			t.Errorf("%d games decoded with a zero start date, which reads as a tip-off long past", zeroStart)
		}
		if withPoints == 0 {
			t.Error("no game in a month of finished basketball carries a score")
		}
	})

	t.Run("lines", func(t *testing.T) {
		lines, err := c.GetLines(ctx, cbbdata.LineQueryOpts{
			Season: &season, StartDateRange: &start, EndDateRange: &end,
		})
		if err != nil {
			t.Fatalf("GetLines: %v", err)
		}
		if len(lines) == 0 {
			t.Fatal("no lines decoded")
		}
		var quoted int
		for _, l := range lines {
			quoted += len(l.Lines)
		}
		if quoted == 0 {
			t.Error("games decoded but not one carries a sportsbook line")
		}
	})

	t.Run("scoreboard", func(t *testing.T) {
		games, err := c.GetScoreboard(ctx)
		if err != nil {
			t.Fatalf("GetScoreboard: %v", err)
		}
		if len(games) == 0 {
			t.Fatal("no scoreboard games decoded")
		}
		var finals, zeroStart int
		for _, g := range games {
			if g.Status == "final" && g.HomeTeam.Points != nil && g.AwayTeam.Points != nil {
				finals++
			}
			if g.StartDate.IsZero() {
				zeroStart++
			}
		}
		if finals == 0 {
			t.Error("the first capture holds no scored finals, so finalization is untested by it")
		}
		if zeroStart > 0 {
			t.Errorf("%d games decoded with a zero start date", zeroStart)
		}
	})

	// A game is two rows, one per team, and the sync reads only a row's own side, so a
	// game with one row would lose a side of its box score. Non-Division I opponents
	// have rows too. The capture has no unplayed games, though tonight's fall in its window.
	t.Run("game team stats", func(t *testing.T) {
		rows, err := c.GetGameTeamStats(ctx, "2026-11-02T00:00:00.000Z", "2026-11-06T00:00:00.000Z")
		if err != nil {
			t.Fatalf("GetGameTeamStats: %v", err)
		}
		if len(rows) == 0 {
			t.Fatal("no rows decoded")
		}
		perGame := make(map[int64]int, len(rows)/2)
		for _, row := range rows {
			perGame[row.GameID]++
			if row.TeamID == 0 {
				t.Fatalf("a row decoded with no team: %+v", row)
			}
			if row.TeamStats.Points.Total == nil || *row.TeamStats.Points.Total == 0 {
				t.Errorf("game %d: team %d decoded no points", row.GameID, row.TeamID)
			}
			if row.TeamStats.FieldGoals.Attempted == nil || row.TeamStats.Rebounds.Total == nil || row.TeamStats.Points.InPaint == nil {
				t.Errorf("game %d: team %d decoded without its counts", row.GameID, row.TeamID)
			}
		}
		for game, n := range perGame {
			if n != 2 {
				t.Errorf("game %d has %d rows, want 2", game, n)
			}
		}
	})

	t.Run("rankings", func(t *testing.T) {
		rows, err := c.GetRankings(ctx, fixtureSeason)
		if err != nil {
			t.Fatalf("GetRankings: %v", err)
		}
		var ranked, voteOnly int
		for _, row := range rows {
			if row.Ranking != nil {
				ranked++
			} else {
				voteOnly++
			}
		}
		// Both have to be present, or the capture no longer tests dropping one.
		if ranked == 0 || voteOnly == 0 {
			t.Errorf("%d ranked rows and %d receiving votes, want some of each", ranked, voteOnly)
		}
	})

}
