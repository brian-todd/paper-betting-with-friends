package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/pagetest"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// The basketball pages through the real router. The rules behind them are tested
// in their own packages; what is checked here is that a reader can reach them and
// that the wiring carries ranks and bets to the page.
func TestBasketballThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	resp, err := client.PostForm(server.URL+"/register", url.Values{
		"username":         {"hoops-fan"},
		"password":         {"long-enough-password"},
		"confirm_password": {"long-enough-password"},
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	resp.Body.Close()

	var user models.User
	if err := env.DB.Where("username = ?", "hoops-fan").First(&user).Error; err != nil {
		t.Fatalf("reading user: %v", err)
	}

	// A day after the fixture instant, late enough in Eastern to be the next day
	// in UTC, which is what the back link has to get right.
	tip := time.Date(2026, 9, 6, 22, 30, 0, 0, env.Location)
	day := tip.Format(time.DateOnly)
	game := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportBasketball, Season: 2027, ScheduledAt: tip})
	testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportBasketball, Season: 2026, ScheduledAt: tip.AddDate(0, -5, 0), Status: models.GameStatusFinal})

	// A poll date arrives as midnight UTC.
	if err := env.DB.Create(&models.PollRank{
		TeamID: game.HomeTeamID, Season: 2027, Poll: models.PollAP,
		PollDate: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), Rank: 7,
	}).Error; err != nil {
		t.Fatalf("creating rank: %v", err)
	}

	page := func(t *testing.T, path string) string {
		t.Helper()
		resp, body := get(t, client, server.URL+path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, resp.StatusCode)
		}
		return body
	}

	t.Run("the nav links to basketball", func(t *testing.T) {
		if !strings.Contains(page(t, "/games"), `href="/basketball"`) {
			t.Error("no nav link to /basketball")
		}
	})

	t.Run("a conference with no games that day says so and stays selected", func(t *testing.T) {
		body := page(t, "/basketball/games?date="+day+"&conference=Zed")
		if !strings.Contains(body, "No Zed basketball games on this date") {
			t.Error("the empty state ignores the conference filter")
		}
		if !strings.Contains(body, `<option value="Zed" selected`) {
			t.Error("the dropdown reads as unfiltered while the filter applies")
		}
	})

	t.Run("the index opens on the next day with games", func(t *testing.T) {
		resp, _ := get(t, client, server.URL+"/basketball")
		if got := resp.Request.URL.Query().Get("date"); got != day {
			t.Errorf("landed on %q, want %q", got, day)
		}
	})

	t.Run("the season dropdown lands on a day in that season", func(t *testing.T) {
		resp, _ := get(t, client, server.URL+"/basketball/games?season=2026")
		if got := resp.Request.URL.Query().Get("date"); got != tip.AddDate(0, -5, 0).Format(time.DateOnly) {
			t.Errorf("landed on %q", got)
		}
	})

	t.Run("the day page shows the rank and both seasons", func(t *testing.T) {
		body := page(t, "/basketball/games?date="+day)
		if !strings.Contains(body, "#7") {
			t.Error("the card does not show the rank")
		}
		if !strings.Contains(body, "2026-27") || !strings.Contains(body, "2025-26") {
			t.Error("the season dropdown does not label both seasons")
		}
	})

	t.Run("the game page shows the rank and links back to its day", func(t *testing.T) {
		body := page(t, "/games/"+game.ID.String())
		if !strings.Contains(body, "#7") {
			t.Error("the game page does not show the rank")
		}
		if !strings.Contains(body, "/basketball/games?date="+day) {
			t.Error("the back link does not return to the game's day")
		}
	})

	t.Run("a bet shows on the card and My Bets filters by sport", func(t *testing.T) {
		league := env.SportLeague("Hoops", models.SportBasketball, &user, "1000")
		line := &models.SpreadOdds{
			GameID: game.ID, Source: models.OddsSourceDraftKings,
			HomeSpread: decimal.NewFromInt(-3), AwaySpread: decimal.NewFromInt(3),
			HomeOdds: decimal.NewFromInt(-110), AwayOdds: decimal.NewFromInt(-110),
		}
		if err := env.DB.Create(line).Error; err != nil {
			t.Fatalf("creating line: %v", err)
		}
		bet := &models.SpreadBet{
			UserID: user.ID, LeagueID: league.ID, GameID: game.ID, SpreadOddsID: line.ID,
			Pick: models.SpreadPickHome, SpreadSnapshot: line.HomeSpread, OddsSnapshot: line.HomeOdds,
			Stake: decimal.NewFromInt(10), Status: models.BetStatusPending,
		}
		if err := env.DB.Create(bet).Error; err != nil {
			t.Fatalf("creating bet: %v", err)
		}

		if !strings.Contains(page(t, "/basketball/games?date="+day), "badge-user-bet") {
			t.Error("the card does not mark the viewer's bet")
		}

		abbreviation := func() string {
			var team models.Team
			if err := env.DB.First(&team, "id = ?", game.HomeTeamID).Error; err != nil {
				t.Fatalf("reading team: %v", err)
			}
			return team.Abbreviation
		}()
		basketball := page(t, "/bets?sport=basketball")
		if !strings.Contains(basketball, abbreviation) {
			t.Error("the basketball filter hides a basketball bet")
		}
		if !strings.Contains(basketball, "2026-27 Season") {
			t.Error("the season filter does not use the basketball label")
		}
		if strings.Contains(page(t, "/bets?sport=football"), abbreviation) {
			t.Error("the football filter shows a basketball bet")
		}
	})
}

// A game reads its own season's ratings and no other. Rosters turn over, so last
// season's number on this season's game reads as a verdict on a different team, and a
// season's first days are meant to show none.
func TestMatchupReadsOnlyItsOwnSeason(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	resp, err := client.PostForm(server.URL+"/register", url.Values{
		"username":         {"season-watcher"},
		"password":         {"long-enough-password"},
		"confirm_password": {"long-enough-password"},
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	resp.Body.Close()

	home := testdb.InsertTeam(t, env.DB, "")
	away := testdb.InsertTeam(t, env.DB, "")
	for _, team := range []uuid.UUID{home.ID, away.ID} {
		row := models.TeamRating{
			TeamID: team, Season: 2025, Source: models.RatingSourceSRS,
			FetchedAt: firstSnapshot, Overall: decimal.RequireFromString("9.9"),
		}
		if err := env.DB.Create(&row).Error; err != nil {
			t.Fatalf("creating rating: %v", err)
		}
	}
	game := func(season int) *models.Game {
		return testdb.InsertGame(t, env.DB, models.Game{
			Sport: models.SportBasketball, Season: season, ScheduledAt: firstSnapshot.AddDate(0, 0, 1),
			HomeTeamID: home.ID, AwayTeamID: away.ID,
		})
	}

	_, body := get(t, client, server.URL+"/games/"+game(2026).ID.String())
	if strings.Contains(body, ">SRS<") {
		t.Error("a 2026 game shows a 2025 rating")
	}

	_, body = get(t, client, server.URL+"/games/"+game(2025).ID.String())
	if !strings.Contains(body, ">SRS<") {
		t.Error("a 2025 game does not show its own season's rating, so this test checked nothing")
	}
}

// What the lineups panel shows depends on when you are looking. A page must never
// credit a team with what it did after the game being viewed.
func TestLineupPanelThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	resp, err := client.PostForm(server.URL+"/register", url.Values{
		"username":         {"unit-watcher"},
		"password":         {"long-enough-password"},
		"confirm_password": {"long-enough-password"},
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	resp.Body.Close()

	team := testdb.InsertTeam(t, env.DB, "")
	opponent := testdb.InsertTeam(t, env.DB, "")
	base := firstSnapshot.AddDate(0, 0, -10)

	game := func(season int, at time.Time, status models.GameStatus) *models.Game {
		return testdb.InsertGame(t, env.DB, models.Game{
			Sport: models.SportBasketball, Season: season, ScheduledAt: at, Status: status,
			HomeTeamID: team.ID, AwayTeamID: opponent.ID,
		})
	}
	unit := func(g *models.Game, player string, seconds int) {
		t.Helper()
		row := models.GameLineup{
			GameID: g.ID, TeamID: team.ID, IDHash: player,
			Athletes: models.LineupAthletes{{ID: 1, Name: player}},
			Seconds:  seconds, PointsFor: 30, PossessionsFor: 20, PointsAgainst: 20, PossessionsAgainst: 20,
		}
		if err := env.DB.Create(&row).Error; err != nil {
			t.Fatalf("creating lineup: %v", err)
		}
	}

	lastSeason := game(2025, base.AddDate(-1, 0, 0), models.GameStatusFinal)
	earlier := game(2026, base, models.GameStatusFinal)
	viewed := game(2026, base.AddDate(0, 0, 2), models.GameStatusFinal)
	later := game(2026, base.AddDate(0, 0, 4), models.GameStatusFinal)
	upcoming := game(2026, firstSnapshot.AddDate(0, 0, 1), models.GameStatusScheduled)
	unplayedByAnyone := testdb.InsertGame(t, env.DB, models.Game{
		Sport: models.SportBasketball, Season: 2026, ScheduledAt: firstSnapshot.AddDate(0, 0, 1),
	})

	unit(lastSeason, "Graduated Player", 600)
	unit(earlier, "Earlier Player", 600)
	unit(later, "Later Player", 600)

	page := func(t *testing.T, g *models.Game) string {
		t.Helper()
		resp, body := get(t, client, server.URL+"/games/"+g.ID.String())
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("game page status = %d", resp.StatusCode)
		}
		return body
	}
	const seasonTitle, gameTitle = "Most-used lineups this season", "Lineups in this game"

	t.Run("a finished game the feed has not covered shows the season before it", func(t *testing.T) {
		body := page(t, viewed)
		if !strings.Contains(body, seasonTitle) || !strings.Contains(body, "Earlier Player") {
			t.Error("the panel does not show the season before this game")
		}
		if strings.Contains(body, "Later Player") {
			t.Error("the panel credits the team with a game played after this one")
		}
		if strings.Contains(body, "Graduated Player") {
			t.Error("the panel reaches into last season")
		}
	})

	t.Run("an upcoming game shows the whole season so far", func(t *testing.T) {
		body := page(t, upcoming)
		if !strings.Contains(body, "Earlier Player") || !strings.Contains(body, "Later Player") {
			t.Error("the panel is missing games already played")
		}
		if !strings.Contains(body, "not a starting lineup") {
			t.Error("the panel does not say what it is not")
		}
	})

	t.Run("a finished game with its own units shows those", func(t *testing.T) {
		unit(viewed, "Viewed Game Player", 600)
		body := page(t, viewed)
		if !strings.Contains(body, gameTitle) || !strings.Contains(body, "Viewed Game Player") {
			t.Error("the panel does not show this game's units")
		}
		if strings.Contains(body, "Earlier Player") {
			t.Error("the panel mixes in the rest of the season")
		}
	})

	t.Run("no units means no panel", func(t *testing.T) {
		body := page(t, unplayedByAnyone)
		if strings.Contains(body, seasonTitle) || strings.Contains(body, gameTitle) {
			t.Error("the panel renders with nothing in it")
		}
	})
}
