package main

import (
	"net/http"
	"net/url"
	"regexp"
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
		if !strings.Contains(page(t, "/games"), `href="/games/basketball"`) {
			t.Error("no nav link to /games/basketball")
		}
	})

	t.Run("a conference with no games that day says so and stays selected", func(t *testing.T) {
		body := page(t, "/games/basketball?date="+day+"&applied=1&conf=Zed")
		if !strings.Contains(body, "match these filters") {
			t.Error("the empty state ignores the filter")
		}
		if !regexp.MustCompile(`name="conf" value="Zed"\s+checked`).MatchString(body) {
			t.Error("the filter reads as unset while it applies")
		}
	})

	t.Run("choosing a season keeps the filter", func(t *testing.T) {
		resp, _ := get(t, client, server.URL+"/games/basketball?season=2026&applied=1&conf=Zed")
		query := resp.Request.URL.Query()
		if query.Get("date") == "" || query.Get("conf") != "Zed" || query.Get("applied") != "1" {
			t.Errorf("landed on %s", resp.Request.URL)
		}
	})

	t.Run("the index opens on the next day with games", func(t *testing.T) {
		resp, _ := get(t, client, server.URL+"/games/basketball")
		if got := resp.Request.URL.RawQuery; got != "date="+day {
			t.Errorf("landed on %q, want only the date %q", got, day)
		}
	})

	t.Run("the season dropdown lands on a day in that season", func(t *testing.T) {
		resp, _ := get(t, client, server.URL+"/games/basketball?season=2026")
		if got := resp.Request.URL.Query().Get("date"); got != tip.AddDate(0, -5, 0).Format(time.DateOnly) {
			t.Errorf("landed on %q", got)
		}
	})

	t.Run("the day page shows the rank and both seasons", func(t *testing.T) {
		body := page(t, "/games/basketball?date="+day)
		if !strings.Contains(body, "#7") {
			t.Error("the card does not show the rank")
		}
		if !strings.Contains(body, "2026-27") || !strings.Contains(body, "2025-26") {
			t.Error("the season dropdown does not label both seasons")
		}
	})

	t.Run("the game page shows the rank and links back to its day", func(t *testing.T) {
		body := page(t, game.Path())
		if !strings.Contains(body, "#7") {
			t.Error("the game page does not show the rank")
		}
		if !strings.Contains(body, "/games/basketball?date="+day) {
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

		if !strings.Contains(page(t, "/games/basketball?date="+day), "badge-user-bet") {
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

	_, body := get(t, client, server.URL+game(2026).Path())
	if strings.Contains(body, ">SRS<") {
		t.Error("a 2026 game shows a 2025 rating")
	}

	_, body = get(t, client, server.URL+game(2025).Path())
	if !strings.Contains(body, ">SRS<") {
		t.Error("a 2025 game does not show its own season's rating, so this test checked nothing")
	}
}

// The panel has the same rows before and after a game, and only a box score that adds
// up to the final score is called one.
func TestTeamStatsPanelThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	resp, err := client.PostForm(server.URL+"/register", url.Values{
		"username":         {"box-reader"},
		"password":         {"long-enough-password"},
		"confirm_password": {"long-enough-password"},
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	resp.Body.Close()

	home := testdb.InsertTeam(t, env.DB, "")
	away := testdb.InsertTeam(t, env.DB, "")

	n := func(v int) *int { return &v }
	box := func(points int) models.TeamBox {
		return models.TeamBox{
			Points: n(points), FGMade: n(27), FGAttempted: n(60), ThreeMade: n(9), ThreeAttempted: n(24),
			FTMade: n(10), FTAttempted: n(14), Rebounds: n(35), OffRebounds: n(9), DefRebounds: n(26),
			Assists: n(15), Steals: n(6), Blocks: n(3), Turnovers: n(11), Fouls: n(17),
			PaintPoints: n(30), FastBreakPoints: n(12), PointsOffTurnovers: n(14),
		}
	}
	for _, team := range []uuid.UUID{home.ID, away.ID} {
		season := models.TeamSeasonStats{
			TeamID: team, Season: 2026, FetchedAt: firstSnapshot, Games: 10,
			Box: models.TeamBox{Points: n(751), FGMade: n(270), FGAttempted: n(600), Rebounds: n(355)},
		}
		if err := env.DB.Create(&season).Error; err != nil {
			t.Fatalf("creating season stats: %v", err)
		}
	}

	game := func(status models.GameStatus) *models.Game {
		return testdb.InsertGame(t, env.DB, models.Game{
			Sport: models.SportBasketball, Season: 2026, Status: status,
			ScheduledAt: firstSnapshot.AddDate(0, 0, -1), HomeTeamID: home.ID, AwayTeamID: away.ID,
		})
	}
	final := func(g *models.Game, homeScore, awayScore int) {
		t.Helper()
		at := firstSnapshot
		result := models.GameResult{GameID: g.ID, HomeScore: homeScore, AwayScore: awayScore, FinalizedAt: &at}
		if err := env.DB.Create(&result).Error; err != nil {
			t.Fatalf("creating result: %v", err)
		}
	}
	store := func(g *models.Game, homePoints, awayPoints int) {
		t.Helper()
		for team, points := range map[uuid.UUID]int{home.ID: homePoints, away.ID: awayPoints} {
			row := models.GameTeamStats{GameID: g.ID, TeamID: team, FetchedAt: firstSnapshot, Box: box(points)}
			if err := env.DB.Create(&row).Error; err != nil {
				t.Fatalf("creating box score: %v", err)
			}
		}
	}
	page := func(t *testing.T, g *models.Game) string {
		t.Helper()
		resp, body := get(t, client, server.URL+g.Path())
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("game page status = %d", resp.StatusCode)
		}
		return body
	}
	const seasonTitle, boxTitle = "Season averages", "Box score"
	labels := []string{"Points", "Field goals", "3-pointers", "Free throws", "Rebounds", "Assists", "Points in the paint"}

	var pregame string
	t.Run("an upcoming game shows the season per game", func(t *testing.T) {
		pregame = page(t, game(models.GameStatusScheduled))
		if !strings.Contains(pregame, seasonTitle) || strings.Contains(pregame, boxTitle) {
			t.Error("the panel is not season averages")
		}
		if !strings.Contains(pregame, ">75.1<") || !strings.Contains(pregame, ">27.0-60.0<") {
			t.Error("the season's totals are not shown per game")
		}
	})

	t.Run("a finished game shows the same rows from its box score", func(t *testing.T) {
		g := game(models.GameStatusFinal)
		final(g, 80, 70)
		store(g, 80, 70)
		body := page(t, g)
		if !strings.Contains(body, boxTitle) || strings.Contains(body, seasonTitle) {
			t.Error("the panel is not the box score")
		}
		if !strings.Contains(body, ">80<") || !strings.Contains(body, ">27-60<") {
			t.Error("the game's own figures are not shown")
		}
		for _, label := range labels {
			row := ">" + label + "<"
			if !strings.Contains(pregame, row) || !strings.Contains(body, row) {
				t.Errorf("%q is not on both versions of the panel", label)
			}
		}
	})

	t.Run("a box score that misses the final score is not shown", func(t *testing.T) {
		g := game(models.GameStatusFinal)
		final(g, 80, 70)
		store(g, 41, 38)
		body := page(t, g)
		if strings.Contains(body, boxTitle) || !strings.Contains(body, seasonTitle) {
			t.Error("a partial box score was shown as the game's")
		}
	})

	t.Run("a finished game waiting for one shows the season", func(t *testing.T) {
		g := game(models.GameStatusFinal)
		final(g, 80, 70)
		if body := page(t, g); !strings.Contains(body, seasonTitle) {
			t.Error("the page does not fall back to the season")
		}
	})

	t.Run("a season row with no counts yet means no panel", func(t *testing.T) {
		other := testdb.InsertTeam(t, env.DB, "")
		season := models.TeamSeasonStats{TeamID: other.ID, Season: 2026, FetchedAt: firstSnapshot, Games: 10}
		if err := env.DB.Create(&season).Error; err != nil {
			t.Fatalf("creating season stats: %v", err)
		}
		g := testdb.InsertGame(t, env.DB, models.Game{
			Sport: models.SportBasketball, Season: 2026, Status: models.GameStatusScheduled,
			ScheduledAt: firstSnapshot.AddDate(0, 0, 1), HomeTeamID: home.ID, AwayTeamID: other.ID,
		})
		if body := page(t, g); strings.Contains(body, seasonTitle) {
			t.Error("a panel of blanks was drawn")
		}
	})

	t.Run("a team with no season figures means no panel", func(t *testing.T) {
		other := testdb.InsertTeam(t, env.DB, "")
		g := testdb.InsertGame(t, env.DB, models.Game{
			Sport: models.SportBasketball, Season: 2026, Status: models.GameStatusScheduled,
			ScheduledAt: firstSnapshot.AddDate(0, 0, 1), HomeTeamID: home.ID, AwayTeamID: other.ID,
		})
		body := page(t, g)
		if strings.Contains(body, seasonTitle) || strings.Contains(body, boxTitle) {
			t.Error("half a panel was drawn")
		}
	})
}

// The page hides games without a line until asked, and the form is how it is asked:
// an unticked box arrives as nothing, so "applied" is what makes it a choice.
func TestBasketballGridLinesFilterThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	resp, err := client.PostForm(server.URL+"/register", url.Values{
		"username":         {"line-watcher"},
		"password":         {"long-enough-password"},
		"confirm_password": {"long-enough-password"},
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	resp.Body.Close()

	tip := firstSnapshot.AddDate(0, 0, 1)
	game := func(name string) *models.Game {
		home := testdb.InsertTeam(t, env.DB, "")
		if err := env.DB.Model(home).Update("name", name).Error; err != nil {
			t.Fatalf("naming team: %v", err)
		}
		return testdb.InsertGame(t, env.DB, models.Game{
			Sport: models.SportBasketball, Season: 2026, ScheduledAt: tip,
			HomeTeamID: home.ID, AwayTeamID: testdb.InsertTeam(t, env.DB, "").ID,
		})
	}
	lined := game("Lined University")
	game("Unlined University")
	line := models.OverUnderOdds{
		GameID: lined.ID, Source: models.OddsSourceDraftKings, Total: decimal.NewFromInt(140),
		OverOdds: decimal.NewFromInt(-110), UnderOdds: decimal.NewFromInt(-110),
	}
	if err := env.DB.Create(&line).Error; err != nil {
		t.Fatalf("creating line: %v", err)
	}

	day := tip.In(env.Location).Format(time.DateOnly)
	tests := []struct {
		name, query string
		wantUnlined bool
	}{
		{"an untouched page hides it", "", false},
		{"the box ticked hides it", "&applied=1&bettable=1", false},
		{"the box unticked shows it", "&applied=1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, body := get(t, client, server.URL+"/games/basketball?date="+day+tt.query)
			if !strings.Contains(body, "Lined University") {
				t.Fatal("the game with a line is missing")
			}
			if got := strings.Contains(body, "Unlined University"); got != tt.wantUnlined {
				t.Errorf("the game without a line drawn = %v, want %v", got, tt.wantUnlined)
			}
		})
	}
}
