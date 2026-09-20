package cfbdata_test

import (
	"context"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
)

// fixtureYear is the season the committed captures cover.
const fixtureYear = 2026

// client returns a real client pointed at the committed fixtures.
func client(t *testing.T, opts ...fixtureserver.Option) *cfbdata.Client {
	t.Helper()
	base, _, stop, err := fixtureserver.Listen(fixtures.CFBD, opts...)
	if err != nil {
		t.Fatalf("starting the fixture server: %v", err)
	}
	t.Cleanup(stop)
	// No API key: the point is that replaying a capture needs nothing.
	return cfbdata.NewClientAt(base, "")
}

// Level 1: a recorded body through the real decoder. It is the cheapest test
// in the plan and it covers the thing nobody can check by reading -- whether
// the structs still match what CFBD sends, field for field.
//
// Every assertion is "more than none", because the failure being guarded
// against is a rename upstream that leaves the JSON decoding cleanly into
// zero values. A count is the only thing that notices that.
func TestClientDecodesEveryCapturedEndpoint(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	week := 1

	t.Run("venues", func(t *testing.T) {
		venues, err := c.GetVenues(ctx)
		if err != nil {
			t.Fatalf("GetVenues: %v", err)
		}
		if len(venues) == 0 {
			t.Fatal("no venues decoded")
		}
		var named int
		for _, v := range venues {
			if v.Name != "" {
				named++
			}
		}
		if named == 0 {
			t.Errorf("decoded %d venues and not one has a name", len(venues))
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

		// The division spread is the property the capture rule protects: never
		// filter a capture by classification, because comparing one in the
		// wrong case is a bug this repository has actually shipped.
		divisions := map[string]int{}
		for _, team := range teams {
			divisions[team.Classification]++
		}
		for _, want := range []string{"fbs", "fcs", "ii", "iii"} {
			if divisions[want] == 0 {
				t.Errorf("no %s teams in the capture; the fixture set has lost its division spread", want)
			}
		}
		// The case is asserted rather than assumed. testdb.InsertTeam hard-codes
		// "fbs" and its comment says that is the case CFBD reports; this is the
		// only thing that would notice if that stopped being true, and
		// CFB_SCOREBOARD_CLASSIFICATIONS=FBS has already shipped once as a
		// division that matched no team while every run logged success.
		if divisions["FBS"] != 0 {
			t.Errorf("CFBD now reports classification in upper case; every comparison against %q is wrong", "fbs")
		}
	})

	t.Run("calendar", func(t *testing.T) {
		weeks, err := c.GetCalendar(ctx, fixtureYear)
		if err != nil {
			t.Fatalf("GetCalendar: %v", err)
		}
		if len(weeks) == 0 {
			t.Fatal("no weeks decoded")
		}
		for _, w := range weeks {
			if w.StartDate.IsZero() || w.EndDate.IsZero() {
				t.Fatalf("week %d decoded with a zero date; the calendar drives which season the sync fetches", w.Week)
			}
		}
	})

	t.Run("games", func(t *testing.T) {
		games, err := c.GetGames(ctx, fixtureYear, &week, nil)
		if err != nil {
			t.Fatalf("GetGames: %v", err)
		}
		if len(games) == 0 {
			t.Fatal("no games decoded")
		}
		var withTeams, withStart int
		for _, g := range games {
			if g.HomeTeam != "" && g.AwayTeam != "" {
				withTeams++
			}
			if !g.StartDate.IsZero() {
				withStart++
			}
		}
		if withTeams != len(games) {
			t.Errorf("%d of %d games decoded without both team names", len(games)-withTeams, len(games))
		}
		if withStart != len(games) {
			t.Errorf("%d of %d games decoded with a zero start date, which reads as a kickoff long past", len(games)-withStart, len(games))
		}
	})

	t.Run("rankings", func(t *testing.T) {
		polls, err := c.GetRankings(ctx, fixtureYear, &week, nil)
		if err != nil {
			t.Fatalf("GetRankings: %v", err)
		}
		if len(polls) == 0 {
			t.Fatal("no ranking weeks decoded")
		}
		var ranked int
		for _, wk := range polls {
			for _, poll := range wk.Polls {
				ranked += len(poll.Ranks)
			}
		}
		if ranked == 0 {
			t.Error("ranking weeks decoded but every poll inside them is empty")
		}
	})

	t.Run("lines", func(t *testing.T) {
		lines, err := c.GetLines(ctx, fixtureYear, &week, nil)
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
		games, err := c.GetScoreboard(ctx, "fbs")
		if err != nil {
			t.Fatalf("GetScoreboard: %v", err)
		}
		if len(games) == 0 {
			t.Fatal("no scoreboard games decoded")
		}

		// The capture is a live Saturday, so it has to contain games in more
		// than one state -- a fixture where everything is already final cannot
		// exercise anything the scoreboard exists for.
		statuses := map[string]int{}
		for _, g := range games {
			statuses[g.Status]++
		}
		if len(statuses) < 2 {
			t.Errorf("the first scoreboard capture holds only %v; it cannot show a game changing state", statuses)
		}
	})
}
