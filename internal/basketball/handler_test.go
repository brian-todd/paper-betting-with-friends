package basketball

import (
	"bytes"
	"strings"
	"testing"
	"time"

	assets "github.com/brian/paper-betting-with-friends"
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
		"PrevDate":   "2026-01-14",
		"NextDate":   "2026-01-16",
		"Search":     "",
		"TotalGames": len(games),
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
