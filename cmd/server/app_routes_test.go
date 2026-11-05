package main

// Where the game pages live, through the real router: the URLs under /games/{sport},
// the redirects from the URLs before them, and the links that get a reader there.

import (
	"io"
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
)

var currentNavLink = regexp.MustCompile(`href="([^"]*)" class="nav-link" aria-current="page"`)

func TestGameURLsThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)
	register(t, client, server.URL, "route-reader", "correct-horse-battery-staple")

	tipOff := firstSnapshot.Add(24 * time.Hour)
	football := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportFootball, ScheduledAt: tipOff})
	basketball := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportBasketball, ScheduledAt: tipOff})

	t.Run("a game's page is served at its own path without a redirect", func(t *testing.T) {
		for _, game := range []*models.Game{football, basketball} {
			resp, _ := get(t, client, server.URL+game.Path())
			if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != game.Path() {
				t.Errorf("GET %s: status %d, landed on %s", game.Path(), resp.StatusCode, resp.Request.URL.Path)
			}
		}
	})

	// A separate view of the same cookie jar that stops at the first redirect.
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	moved := []struct{ name, from, to string }{
		{"a football week keeps its filter",
			"/games/2026/regular/1?applied=1&team=duke", "/games/football/2026/regular/1?applied=1&team=duke"},
		{"the basketball index", "/basketball", "/games/basketball"},
		{"a basketball day keeps its date and filter",
			"/basketball/games?date=2026-11-04&conf=Zed", "/games/basketball?date=2026-11-04&conf=Zed"},
		{"a football game ID alone keeps the bet form's message",
			"/games/" + football.ID.String() + "?success=bet_placed", football.Path() + "?success=bet_placed"},
		{"a basketball game ID alone", "/games/" + basketball.ID.String(), basketball.Path()},
		{"a game under the other sport", "/games/football/" + basketball.ID.String(), basketball.Path()},
		{"a game under no sport we offer", "/games/hockey/" + football.ID.String() + "?error=x", football.Path() + "?error=x"},
	}
	for _, tt := range moved {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := noFollow.Get(server.URL + tt.from)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.from, err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusMovedPermanently {
				t.Errorf("status %d, want 301", resp.StatusCode)
			}
			if got := resp.Header.Get("Location"); got != tt.to {
				t.Errorf("redirects to %q, want %q", got, tt.to)
			}
		})
	}

	refused := []struct {
		path string
		want int
	}{
		{"/games/" + uuid.NewString(), http.StatusNotFound},
		{"/games/football/" + uuid.NewString(), http.StatusNotFound},
		{"/games/not-a-game", http.StatusBadRequest},
		{"/games/football/not-a-game", http.StatusBadRequest},
		// The old week route matches these, and must not send them anywhere.
		{"/games/football/2026/regular", http.StatusNotFound},
		{"/games/football/regular/1", http.StatusNotFound},
		{"/games/basketball/2026/regular", http.StatusNotFound},
		{"/games/2026/preseason/1", http.StatusNotFound},
		{"/games/2026/regular/last", http.StatusNotFound},
	}
	for _, tt := range refused {
		t.Run("GET "+tt.path, func(t *testing.T) {
			// The first answer, because a redirect to a week that does not exist also
			// ends in a 404.
			resp, err := noFollow.Get(server.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

func TestSportLinksThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)

	t.Run("the signed-out home page names the product and both sports", func(t *testing.T) {
		_, body := get(t, newClient(t, server), server.URL+"/")
		for _, want := range []string{`class="nav-brand">🎟️ Paper Betting With Friends<`, "<title>Home | Paper Betting With Friends</title>", "college football and basketball"} {
			if !strings.Contains(body, want) {
				t.Errorf("the page is missing %q", want)
			}
		}
		if strings.Contains(body, "College Football Betting Tracker") {
			t.Error("the page still leads with football")
		}
	})

	client := newClient(t, server)
	register(t, client, server.URL, "link-reader", "correct-horse-battery-staple")

	tipOff := firstSnapshot.Add(24 * time.Hour)
	football := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportFootball, ScheduledAt: tipOff})
	basketball := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportBasketball, ScheduledAt: tipOff})

	for _, path := range []string{"/", "/games"} {
		t.Run(path+" offers both sports", func(t *testing.T) {
			_, body := get(t, client, server.URL+path)
			for _, want := range []string{
				`class="btn btn-primary">Football Games<`, `class="btn btn-primary">Basketball Games<`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("the page is missing %q", want)
				}
			}
		})
	}

	// The middle of the table is the point: a game page belongs to its own sport.
	sections := []struct{ path, want string }{
		{"/", ""},
		{"/games", ""},
		{"/games/football", "/games/football"},
		{football.Path(), "/games/football"},
		{"/games/basketball", "/games/basketball"},
		{basketball.Path(), "/games/basketball"},
		{"/bets", "/bets"},
		{"/leagues", "/leagues"},
		{"/leagues/create", "/leagues"},
	}
	for _, tt := range sections {
		t.Run("the nav marks the section of "+tt.path, func(t *testing.T) {
			resp, body := get(t, client, server.URL+tt.path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}

			var got []string
			for _, match := range currentNavLink.FindAllStringSubmatch(body, -1) {
				got = append(got, match[1])
			}
			switch {
			case tt.want == "" && len(got) != 0:
				t.Errorf("marks %v, want nothing", got)
			case tt.want != "" && (len(got) != 1 || got[0] != tt.want):
				t.Errorf("marks %v, want %s", got, tt.want)
			}
		})
	}

	t.Run("the league form only preselects a sport it was asked for", func(t *testing.T) {
		const unchosen = `<option value="" disabled selected>`
		for path, want := range map[string]string{
			"/leagues/create":                  unchosen,
			"/leagues/create?sport=curling":    unchosen,
			"/leagues/create?sport=basketball": `<option value="basketball" selected>`,
			"/leagues/create?sport=football":   `<option value="football" selected>`,
		} {
			if _, body := get(t, client, server.URL+path); !strings.Contains(body, want) {
				t.Errorf("GET %s is missing %q", path, want)
			}
		}
	})

	t.Run("creating a league", func(t *testing.T) {
		submit := func(t *testing.T, form url.Values) (int, string) {
			t.Helper()
			resp, err := client.PostForm(server.URL+"/leagues", form)
			if err != nil {
				t.Fatalf("POST /leagues: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("reading the response: %v", err)
			}
			return resp.StatusCode, string(body)
		}

		t.Run("a failure keeps the chosen sport", func(t *testing.T) {
			status, body := submit(t, url.Values{"name": {"   "}, "sport": {"basketball"}})
			if status != http.StatusBadRequest {
				t.Errorf("status %d, want 400", status)
			}
			if !strings.Contains(body, `<option value="basketball" selected>`) {
				t.Error("the form lost the sport")
			}
		})

		t.Run("no sport creates nothing", func(t *testing.T) {
			status, body := submit(t, url.Values{"name": {"Sportless"}})
			if status != http.StatusBadRequest || !strings.Contains(body, "Choose football or basketball") {
				t.Errorf("status %d, and the page does not ask for a sport", status)
			}
			var leagues int64
			if err := env.DB.Model(&models.League{}).Where("name = ?", "Sportless").Count(&leagues).Error; err != nil {
				t.Fatalf("counting leagues: %v", err)
			}
			if leagues != 0 {
				t.Error("a league was created with a sport nobody chose")
			}
		})
	})

	t.Run("a game with no league to bet in offers to create one for its sport", func(t *testing.T) {
		_, body := get(t, client, server.URL+basketball.Path())
		if !strings.Contains(body, `href="/leagues/create?sport=basketball"`) {
			t.Error("the game page does not link to a basketball league form")
		}
	})
}
