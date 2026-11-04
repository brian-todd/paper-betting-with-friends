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
	"github.com/shopspring/decimal"
	"gorm.io/gorm/clause"
)

// A league plays one sport. Through the real router, because what is under test
// is that the form reaches the column and the column reaches the bet slip.
func TestLeagueSportThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	post := func(t *testing.T, path string, form url.Values) {
		t.Helper()
		resp, err := client.PostForm(server.URL+path, form)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		resp.Body.Close()
	}
	stored := func(t *testing.T, name string) (models.League, bool) {
		t.Helper()
		var found []models.League
		if err := env.DB.Where("name = ?", name).Find(&found).Error; err != nil {
			t.Fatalf("reading league: %v", err)
		}
		if len(found) == 0 {
			return models.League{}, false
		}
		return found[0], true
	}
	create := func(name, sport string) url.Values {
		return url.Values{"name": {name}, "sport": {sport}, "starting_balance": {"1000"}}
	}

	post(t, "/register", url.Values{
		"username":         {"two-sports"},
		"password":         {"long-enough-password"},
		"confirm_password": {"long-enough-password"},
	})

	tipOff := firstSnapshot.Add(24 * time.Hour)
	basketball := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportBasketball, ScheduledAt: tipOff})
	football := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportFootball, ScheduledAt: tipOff})

	slip := func(t *testing.T, game *models.Game) string {
		t.Helper()
		resp, body := get(t, client, server.URL+"/games/"+game.ID.String())
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("game page status = %d", resp.StatusCode)
		}
		return body
	}

	t.Run("a sport that does not exist creates nothing", func(t *testing.T) {
		post(t, "/leagues", create("Curling Club", "curling"))
		if _, ok := stored(t, "Curling Club"); ok {
			t.Error("a league was created for a sport we do not offer")
		}
	})

	post(t, "/leagues", create("Saturday Pool", models.SportFootball))

	t.Run("a member with no league in the sport is told so", func(t *testing.T) {
		body := slip(t, basketball)
		if !strings.Contains(body, "Join a Basketball League to Bet") {
			t.Error("the page does not ask them to join a basketball league")
		}
		if strings.Contains(body, "Saturday Pool") {
			t.Error("the bet slip offers a football league on a basketball game")
		}
	})

	post(t, "/leagues", create("Tuesday Hoops", models.SportBasketball))

	t.Run("the form's sport is what is stored", func(t *testing.T) {
		league, ok := stored(t, "Tuesday Hoops")
		if !ok {
			t.Fatal("the league was not created")
		}
		if league.Sport != models.SportBasketball {
			t.Errorf("sport = %q, want basketball", league.Sport)
		}
	})

	t.Run("each game's slip offers only its own sport's leagues", func(t *testing.T) {
		body := slip(t, basketball)
		if !strings.Contains(body, "Tuesday Hoops") || strings.Contains(body, "Saturday Pool") {
			t.Error("the basketball slip should offer Tuesday Hoops and not Saturday Pool")
		}
		body = slip(t, football)
		if !strings.Contains(body, "Saturday Pool") || strings.Contains(body, "Tuesday Hoops") {
			t.Error("the football slip should offer Saturday Pool and not Tuesday Hoops")
		}
	})

	t.Run("the league list says which is which", func(t *testing.T) {
		_, body := get(t, client, server.URL+"/leagues")
		if !strings.Contains(body, ">Basketball</span>") || !strings.Contains(body, ">Football</span>") {
			t.Error("the league cards do not show their sport")
		}
	})
}

// A basketball bet from the slip to every page that reads it, through the real router.
// The rules behind each page are tested in their own packages, which is how a recap
// that panicked on any basketball bet passed them: they built the service's week
// grouping by hand, with a time zone the application never handed it.
func TestBasketballBetThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	_, server := serve(t, env)
	client := newClient(t, server)

	register(t, client, server.URL, "hoops-bettor", "long-enough-password")
	var user models.User
	if err := env.DB.Where("username = ?", "hoops-bettor").First(&user).Error; err != nil {
		t.Fatalf("reading user: %v", err)
	}

	league := func(name, sport string) models.League {
		t.Helper()
		resp, err := client.PostForm(server.URL+"/leagues", url.Values{
			"name": {name}, "sport": {sport}, "starting_balance": {"1000"},
		})
		if err != nil {
			t.Fatalf("creating league: %v", err)
		}
		resp.Body.Close()
		var found models.League
		if err := env.DB.Where("name = ?", name).First(&found).Error; err != nil {
			t.Fatalf("reading league %q: %v", name, err)
		}
		return found
	}
	hoops := league("Tuesday Hoops", models.SportBasketball)
	pool := league("Saturday Pool", models.SportFootball)

	// Sunday night in Eastern and already Monday in UTC, so the week it falls in says
	// whether the recap was given the application's time zone.
	tip := time.Date(2026, 9, 6, 22, 30, 0, 0, env.Location)
	game := testdb.InsertGame(t, env.DB, models.Game{Sport: models.SportBasketball, Season: 2027, ScheduledAt: tip})
	odds := &models.SpreadOdds{
		GameID: game.ID, Source: models.OddsSourceDraftKings,
		HomeSpread: decimal.NewFromInt(-5), AwaySpread: decimal.NewFromInt(5),
		HomeOdds: decimal.NewFromInt(-110), AwayOdds: decimal.NewFromInt(-110),
	}
	if err := env.DB.Create(odds).Error; err != nil {
		t.Fatalf("creating odds: %v", err)
	}

	page := func(t *testing.T, path string) string {
		t.Helper()
		resp, body := get(t, client, server.URL+path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, resp.StatusCode)
		}
		return body
	}

	t.Run("My Bets says a sport filter found nothing, not that there are no bets", func(t *testing.T) {
		body := page(t, "/bets?sport=basketball")
		if !strings.Contains(body, "No bets match your current filters") {
			t.Error("the empty state ignores the sport filter")
		}
	})

	resp, err := client.PostForm(server.URL+"/bets/spread", url.Values{
		"game_id": {game.ID.String()}, "league_id": {hoops.ID.String()},
		"pick": {"home"}, "stake": {"100"}, "odds_id": {odds.ID.String()},
	})
	if err != nil {
		t.Fatalf("placing bet: %v", err)
	}
	resp.Body.Close()
	var placed int64
	if err := env.DB.Model(&models.SpreadBet{}).Where("league_id = ?", hoops.ID).Count(&placed).Error; err != nil {
		t.Fatalf("counting bets: %v", err)
	}
	if placed != 1 {
		t.Fatalf("%d bets in the league after posting the slip, want 1", placed)
	}

	t.Run("the league page shows it in the application's week", func(t *testing.T) {
		body := page(t, "/leagues/"+hoops.ID.String())
		if !strings.Contains(body, "Week of Aug 31") {
			t.Error("the recap does not group a Sunday night game into the week it tipped off")
		}
	})

	t.Run("My Bets offers only the sport's leagues", func(t *testing.T) {
		body := page(t, "/bets?sport=basketball")
		if !strings.Contains(body, "Tuesday Hoops") || strings.Contains(body, "Saturday Pool") {
			t.Error("the league dropdown should hold Tuesday Hoops and not Saturday Pool")
		}
	})

	t.Run("My Bets keeps a selected league whatever its sport", func(t *testing.T) {
		body := page(t, "/bets?sport=football&league="+hoops.ID.String())
		if !strings.Contains(body, `<option value="`+hoops.ID.String()+`" selected`) {
			t.Error("a dropdown without the league in force reads as unfiltered")
		}
	})

	// Placement refuses this now, but bets from before leagues had a sport exist.
	t.Run("a basketball bet in a football league still opens", func(t *testing.T) {
		legacy := &models.SpreadBet{
			UserID: user.ID, LeagueID: pool.ID, GameID: game.ID, SpreadOddsID: odds.ID,
			Pick: models.SpreadPickHome, SpreadSnapshot: odds.HomeSpread, OddsSnapshot: odds.HomeOdds,
			Stake: decimal.NewFromInt(50), Status: models.BetStatusPending,
		}
		if err := env.DB.Omit(clause.Associations).Create(legacy).Error; err != nil {
			t.Fatalf("creating bet: %v", err)
		}
		if body := page(t, "/leagues/"+pool.ID.String()); !strings.Contains(body, "Week of Aug 31") {
			t.Error("the football league's recap does not show the basketball bet")
		}
	})
}
