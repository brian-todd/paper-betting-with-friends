package main

// Level 4 is the whole thing: a recorded CFBD response, replayed through the
// real client into real SQL, read back out by the real services, rendered by
// the real templates, and delivered over a real socket through the real
// middleware stack and the real router -- which is buildHandler's, not one this
// file assembles. Nothing is faked but the upstream, and by the time the first
// request is served even that has already done its work.
//
// The router being buildHandler's is the point. A test that wires its own mux
// is testing its own opinion of the wiring, and the wiring is where the
// interesting mistakes are: which middleware wraps which routes, in what order,
// and which of them a given path is actually behind. It is only guarded if it
// was registered through the guard.
//
// The session is minted by registering through the real /register handler and
// kept in a cookie jar, because there is no other way in. "The test suite
// cannot log in" is the kind of thing discovered three days into level 4.

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/pagetest"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
)

// The season and week the committed football fixtures cover in full, and the
// instant the whole process is pinned to: the first of the twenty-six live
// scoreboard captures, a Saturday inside week 1's slate.
const (
	fixtureYear = 2026
	fixtureWeek = 1
)

var firstSnapshot = time.Date(2026, 9, 5, 20, 49, 9, 0, time.UTC)

// weekPath matches the URL /games redirects to once it has resolved which week
// the clock is in.
var weekPath = regexp.MustCompile(`^/games/(\d+)/(\w+)/(\d+)$`)

func TestAFixtureReachesThePageThroughTheRealRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	env.SeedFootball(fixtureYear, fixtureWeek)

	// Quiet: every request through the stack logs a line, and a week of page
	// renders is a wall of them with nothing in it.
	logger := slog.New(slog.DiscardHandler)

	// The scheduler is built but never started, and no job is registered on it.
	// main registers the sync jobs, and this process has no API key and wants
	// no metered request; the admin service holds the scheduler only to report
	// on it.
	app, err := buildHandler(env.Config, env.DB, env.Location, pagetest.Assets, scheduler.New(logger), logger)
	if err != nil {
		t.Fatalf("building the application: %v", err)
	}
	app.SetClock(env.Now)

	server := httptest.NewServer(app.Handler)
	t.Cleanup(server.Close)

	t.Run("an anonymous reader is sent to the login page", func(t *testing.T) {
		// Before minting a session, because a guard that is not there is
		// invisible once you are past it -- and a route is only guarded if it
		// was registered through the guard.
		client := newClient(t, server)
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}

		for _, path := range []string{"/games", "/bets", "/leagues", "/basketball", "/admin"} {
			resp, err := client.Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusSeeOther {
				t.Errorf("GET %s anonymously: status %d, want 303", path, resp.StatusCode)
				continue
			}
			if location := resp.Header.Get("Location"); location != "/login" {
				t.Errorf("GET %s anonymously redirects to %q, want /login", path, location)
			}
		}
	})

	// Registering is the only way in, so everything below shares this client
	// and the cookie it now holds.
	client := newClient(t, server)
	register(t, client, server.URL, "level-four", "correct-horse-battery-staple")

	t.Run("the middleware stack is the one main assembles", func(t *testing.T) {
		resp, body := get(t, client, server.URL+"/")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /: status %d, want 200", resp.StatusCode)
		}

		// securityHeaders sits inside the loggers and outside OptionalAuth, so
		// its headers are on every response the router produces. HSTS is the
		// one that must not be here: this is not production, and pinning
		// https:// for a year against a localhost is a per-browser chore to
		// undo.
		for header, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "same-origin",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s is %q, want %q", header, got, want)
			}
		}
		if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors") {
			t.Errorf("Content-Security-Policy is %q, which names no frame-ancestors", csp)
		}
		if hsts := resp.Header.Get("Strict-Transport-Security"); hsts != "" {
			t.Errorf("Strict-Transport-Security is set to %q outside production", hsts)
		}

		// The cookie reached OptionalAuth, which is the innermost wrapper and
		// the only reason the home page knows who is reading it.
		if !strings.Contains(body, "level-four") {
			t.Error("the home page does not name the logged-in reader, so the session did not survive the stack")
		}
	})

	t.Run("/games resolves the week the clock is in", func(t *testing.T) {
		// ListGames asks the games service which week contains now, and the
		// answer is a redirect. The expectation is not a week number written
		// here -- it is that the week the page chose really does contain the
		// instant every clock in the process was set to.
		resp, _ := get(t, client, server.URL+"/games")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /games: status %d, want 200", resp.StatusCode)
		}

		landed := resp.Request.URL.Path
		match := weekPath.FindStringSubmatch(landed)
		if match == nil {
			t.Fatalf("GET /games landed on %q, which is not a week page; the calendar did not resolve", landed)
		}

		var week models.Week
		err := env.DB.Where("season = ? AND season_type = ? AND number = ?",
			match[1], match[2], match[3]).First(&week).Error
		if err != nil {
			t.Fatalf("reading the week %s back: %v", landed, err)
		}
		if env.At.Before(week.StartDate) || env.At.After(week.EndDate) {
			t.Errorf("/games chose %s, which runs %s to %s and does not contain %s",
				landed, week.StartDate, week.EndDate, env.At)
		}
	})

	t.Run("a game the feed wrote is on the page it renders", func(t *testing.T) {
		// One row, chosen from the transaction rather than from the page, so
		// this is a claim about the feed reaching the browser rather than about
		// the page being self-consistent. It has to be a game the default view
		// shows: FBS, priced, and in the week the clock is in.
		var game struct {
			ID       string
			Season   int
			Week     int
			HomeName string
			AwayName string
		}
		err := env.DB.Raw(`
			SELECT g.id, w.season, w.number AS week, home.name AS home_name, away.name AS away_name
			FROM games g
			JOIN weeks w ON w.id = g.week_id
			JOIN teams home ON home.id = g.home_team_id
			JOIN teams away ON away.id = g.away_team_id
			WHERE home.classification = 'fbs' AND away.classification = 'fbs'
			  AND EXISTS (SELECT 1 FROM spread_odds o WHERE o.game_id = g.id)
			ORDER BY g.scheduled_at
			LIMIT 1`).Scan(&game).Error
		if err != nil || game.ID == "" {
			t.Fatalf("finding a priced FBS game: %v", err)
		}

		// The team filter narrows the grid to that one matchup, so the
		// assertion does not depend on which page of four hundred it fell on.
		target := server.URL + "/games/" + strconv.Itoa(game.Season) + "/regular/" + strconv.Itoa(game.Week) +
			"?applied=1&team=" + url.QueryEscape(game.HomeName)
		resp, body := get(t, client, target)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200", target, resp.StatusCode)
		}

		if !strings.Contains(body, `href="/games/`+game.ID+`"`) {
			t.Fatalf("game %s (%s v %s) is in the database but not on its own filtered page",
				game.ID, game.AwayName, game.HomeName)
		}
		for _, name := range []string{game.HomeName, game.AwayName} {
			if !strings.Contains(body, name) {
				t.Errorf("the page does not name %q", name)
			}
		}

		// And the detail page for the same row, which is the other template
		// and a different set of queries behind it.
		resp, body = get(t, client, server.URL+"/games/"+game.ID)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET the game detail: status %d, want 200", resp.StatusCode)
		}
		for _, name := range []string{game.HomeName, game.AwayName} {
			if !strings.Contains(body, name) {
				t.Errorf("the detail page does not name %q", name)
			}
		}
	})

	t.Run("a reader can get from registering to a placed bet", func(t *testing.T) {
		// The money path, over HTTP, against a line CFBD really published. Two
		// forms and two pages: create a league, place a bet on a game the feed
		// wrote, and read the bet and the debited purse back off the pages that
		// show them.
		//
		// Nothing here knows an ID it was not given by a previous response or
		// by the seeded rows, which is what makes it a journey rather than a
		// sequence of handler calls sharing a fixture.
		var line struct {
			GameID   string
			OddsID   string
			HomeName string
		}
		err := env.DB.Raw(`
			SELECT g.id AS game_id, o.id AS odds_id, home.name AS home_name
			FROM games g
			JOIN spread_odds o ON o.game_id = g.id
			JOIN teams home ON home.id = g.home_team_id
			WHERE g.scheduled_at > ?
			ORDER BY g.scheduled_at
			LIMIT 1`, env.At).Scan(&line).Error
		if err != nil || line.GameID == "" {
			t.Fatalf("finding a priced game still ahead of %s: %v", env.At, err)
		}

		resp, _ := postForm(t, client, server.URL+"/leagues", url.Values{
			"name":             {"Level Four"},
			"starting_balance": {"1000"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("creating a league: status %d, want 200 after the redirect", resp.StatusCode)
		}
		leaguePath := resp.Request.URL.Path
		leagueID := strings.TrimPrefix(leaguePath, "/leagues/")
		if leagueID == leaguePath || leagueID == "" {
			t.Fatalf("creating a league landed on %q, which names no league", leaguePath)
		}

		resp, body := postForm(t, client, server.URL+"/bets/spread", url.Values{
			"game_id":   {line.GameID},
			"league_id": {leagueID},
			"pick":      {string(models.SpreadPickHome)},
			"stake":     {"25"},
			"odds_id":   {line.OddsID},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("placing the bet: status %d (%s)", resp.StatusCode, strings.TrimSpace(body))
		}
		// Both outcomes redirect to the game, so the path alone says nothing:
		// success carries ?success=bet_placed and a refusal carries ?error=,
		// naming which gate turned it away. Reading the query is the difference
		// between a failure diagnosed here and one diagnosed three assertions
		// later as "the bets page is empty".
		if got := resp.Request.URL.Query().Get("success"); got != "bet_placed" {
			t.Fatalf("placing the bet landed on %q, which does not report a placement",
				resp.Request.URL.RequestURI())
		}

		resp, body = get(t, client, server.URL+"/bets")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /bets: status %d, want 200", resp.StatusCode)
		}
		for what, want := range map[string]string{
			"the game it was placed on": `href="/games/` + line.GameID + `"`,
			"the stake":                 `<td class="stake-cell">25</td>`,
			"the league it is in":       "Level Four",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the bets page does not show %s (%s)", what, want)
			}
		}

		// The purse moved by exactly the stake, on the page that reports it.
		resp, body = get(t, client, server.URL+leaguePath)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200", leaguePath, resp.StatusCode)
		}
		if want := `<span class="balance-amount">$975</span>`; !strings.Contains(body, want) {
			t.Errorf("the league page does not show a balance of 975 after a 25 stake against 1000")
		}
	})
}

// newClient is an HTTP client with a cookie jar, which is the whole of what a
// browser brings to this.
func newClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("building a cookie jar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

// register mints a session the only way the application offers one: by posting
// the real registration form and keeping whatever comes back.
func register(t *testing.T, client *http.Client, base, username, password string) {
	t.Helper()

	resp, err := client.PostForm(base+"/register", url.Values{
		"username":         {username},
		"password":         {password},
		"confirm_password": {password},
	})
	if err != nil {
		t.Fatalf("registering %q: %v", username, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registering %q: status %d, want 200 after the redirect home", username, resp.StatusCode)
	}
	if resp.Request.URL.Path != "/" {
		t.Fatalf("registering %q landed on %q, want /", username, resp.Request.URL.Path)
	}

	base_, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %q: %v", base, err)
	}
	if len(client.Jar.Cookies(base_)) == 0 {
		t.Fatal("registration returned no cookie, so nothing below is logged in")
	}
}

// postForm submits a form and follows wherever it leads, which for every form
// in this application is a redirect to a page.
func postForm(t *testing.T, client *http.Client, target string, form url.Values) (*http.Response, string) {
	t.Helper()

	resp, err := client.PostForm(target, form)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", target, err)
	}
	return resp, string(body)
}

// get fetches a page and returns the response together with its body, having
// followed any redirects -- resp.Request.URL is where it ended up.
func get(t *testing.T, client *http.Client, target string) (*http.Response, string) {
	t.Helper()

	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", target, err)
	}
	return resp, string(body)
}
