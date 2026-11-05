package basketball

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
	"time"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// renderGames renders the basketball grid for real. A parse succeeds happily on
// a field access that blows up at execution time, so executing is the check
// worth having.
func renderGames(t *testing.T, games []GameWithOdds) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	date := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	page := map[string]any{
		"Title":      "Basketball",
		"User":       &models.User{Username: "tester"},
		"Games":      games,
		"Date":       date,
		"DateStr":    date.Format("2006-01-02"),
		"PrevURL":    "/games/basketball?date=2026-01-14",
		"NextURL":    "/games/basketball?date=2026-01-16",
		"TotalGames": len(games),
		"Filter":     basketballFilterDefault(),
	}

	var buf bytes.Buffer
	if err := renderer.Render(&buf, "basketball_games", page); err != nil {
		t.Fatalf("rendering basketball games: %v", err)
	}
	return buf.String()
}

// The grid prices its lines, the same as the football one.
func TestBasketballGridShowsLineOdds(t *testing.T) {
	html := renderGames(t, []GameWithOdds{{
		Game: models.Game{
			ID:          uuid.New(),
			ScheduledAt: time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC),
			Status:      models.GameStatusScheduled,
			HomeTeam:    models.Team{Abbreviation: "GT"},
			AwayTeam:    models.Team{Abbreviation: "DUKE"},
		},
		Spread: &models.SpreadOdds{
			ID:         uuid.New(),
			HomeSpread: decimal.RequireFromString("-3.5"),
			AwaySpread: decimal.RequireFromString("3.5"),
			HomeOdds:   decimal.RequireFromString("-110"),
			AwayOdds:   decimal.RequireFromString("-105"),
		},
		OverUnder: &models.OverUnderOdds{
			ID:        uuid.New(),
			Total:     decimal.RequireFromString("142.5"),
			OverOdds:  decimal.RequireFromString("-115"),
			UnderOdds: decimal.RequireFromString("100"),
		},
	}})

	flat := strings.Join(strings.Fields(html), " ")
	for _, want := range []string{
		`U 142.5 <span class="odds-juice">+100`,
		`O 142.5 <span class="odds-juice">-115`,
		`-3.5 <span class="odds-juice">-110`,
		`+3.5 <span class="odds-juice">-105`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the basketball grid is missing %q", want)
		}
	}
}

// The marker sits beside the status, as on the football grid. Inside it, it takes on
// the status's colored pill and uppercase, and a live game's pulse.
func TestBasketballGridDrawsTheBetBesideTheStatus(t *testing.T) {
	html := renderGames(t, []GameWithOdds{{
		Game: models.Game{
			ID:          uuid.New(),
			ScheduledAt: time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC),
			Status:      models.GameStatusInProgress,
			HomeTeam:    models.Team{Abbreviation: "GT"},
			AwayTeam:    models.Team{Abbreviation: "DUKE"},
		},
		BetSummary: "DUKE -3.5",
	}})

	status := strings.Index(html, `class="game-status`)
	marker := strings.Index(html, "badge-user-bet")
	if status < 0 || marker < 0 {
		t.Fatalf("status at %d, marker at %d", status, marker)
	}
	closes := strings.Index(html[status:], "</div>")
	if closes < 0 {
		t.Fatal("the game status is never closed")
	}
	if marker < status+closes {
		t.Error("the bet marker is drawn inside the game status")
	}
}

func basketballFilterDefault() games.Filter {
	return parseFilter(url.Values{})
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantBettable bool
		wantActive   bool
		wantQuery    string
	}{
		{"an untouched page hides games without a line and says nothing in its links", "", true, false, ""},
		{"a submitted form without the box asks for every game", "applied=1", false, true, "applied=1"},
		{"a submitted form with the box keeps it", "applied=1&bettable=1", true, true, "applied=1&bettable=1"},
		{"a hand-written link still starts from the page's default", "team=duke", true, true, "applied=1&bettable=1&team=duke"},
		{"a division has nothing to match in this feed", "applied=1&tier=fbs", false, true, "applied=1"},
		{"a kickoff day has no meaning on a page of one day", "applied=1&day=3", false, true, "applied=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			got := parseFilter(values)
			if got.Bettable != tt.wantBettable {
				t.Errorf("Bettable = %v, want %v", got.Bettable, tt.wantBettable)
			}
			if !got.Defaulted != tt.wantActive {
				t.Errorf("active = %v, want %v", !got.Defaulted, tt.wantActive)
			}
			if encoded := got.Query().Encode(); encoded != tt.wantQuery {
				t.Errorf("Query() = %q, want %q", encoded, tt.wantQuery)
			}
		})
	}
}

// A link to another day carries the filter, and the date replaces the old one.
func TestDayURLCarriesTheFilter(t *testing.T) {
	values, err := url.ParseQuery("applied=1&conf=ACC&conf=SEC&ranked=1&date=2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	got := dayURL(time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), parseFilter(values))
	for _, want := range []string{"conf=ACC", "conf=SEC", "ranked=1", "applied=1", "date=2026-02-03"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing %s", got, want)
		}
	}
	if strings.Contains(got, "2026-01-01") {
		t.Errorf("%s kept the old date", got)
	}
}

func TestBasketballGridDrawsTheFootballFilters(t *testing.T) {
	html := renderGames(t, nil)
	for _, want := range []string{
		`class="games-filters"`, `name="applied"`, `name="team"`, `name="status"`, `name="bettable"`,
		`name="ranked"`, `name="ranked_matchup"`, `name="neutral"`, `name="from"`, `name="to"`,
		`name="spread_min"`, `name="total_max"`, `name="ml_min"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the filter form is missing %s", want)
		}
	}
	for _, unwanted := range []string{`name="tier"`, `name="day"`, `name="search"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("the filter form draws %s", unwanted)
		}
	}
}
