package bets

import (
	"bytes"
	"maps"
	"strings"
	"testing"
	"time"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// renderBetsPage renders the bets page against a BetView, which is the only way
// to exercise the template's own logic -- a parse succeeds happily on a
// comparison that blows up at execution time.
func renderBetsPage(t *testing.T, bet BetView) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	var buf bytes.Buffer
	err = renderer.Render(&buf, "bets", map[string]any{
		"Title": "My Bets",
		"User":  &models.User{Username: "tester"},
		"Bets":  []BetView{bet},
	})
	if err != nil {
		t.Fatalf("rendering bets page: %v", err)
	}
	return buf.String()
}

// editableBet is a pending spread bet on a game that has not started, with two
// book lines on offer and the second one taken.
func editableBet(t *testing.T) (BetView, uuid.UUID, uuid.UUID) {
	t.Helper()

	draftKings, fanDuel := uuid.New(), uuid.New()
	return BetView{
		ID:   uuid.New(),
		Type: "spread",
		Game: models.Game{
			ID:          uuid.New(),
			ScheduledAt: time.Now().Add(3 * time.Hour),
			Status:      models.GameStatusScheduled,
			HomeTeam:    models.Team{Abbreviation: "GT"},
			AwayTeam:    models.Team{Abbreviation: "CLEM"},
			Venue:       models.Venue{Name: "Bobby Dodd Stadium"},
			Week:        &models.Week{Number: 1},
		},
		League:       models.League{Name: "Test League"},
		Pick:         "GT",
		PickSide:     "home",
		Line:         "-7",
		OddsSnapshot: decimal.RequireFromString("-110"),
		Stake:        decimal.RequireFromString("25"),
		Status:       models.BetStatusPending,
		CreatedAt:    time.Now(),
		OddsID:       fanDuel,
		Editable:     true,
		LineOptions: []BetLineOption{
			{OddsID: draftKings, Label: "DraftKings: GT -7 (-110) / CLEM +7 (-110)"},
			{OddsID: fanDuel, Label: "FanDuel: GT -7.5 (-105) / CLEM +7.5 (-115)"},
		},
	}, draftKings, fanDuel
}

func TestBetsPageRendersEditForm(t *testing.T) {
	bet, draftKings, fanDuel := editableBet(t)
	html := renderBetsPage(t, bet)

	wantSubstrings := []string{
		`action="/bets/spread/` + bet.ID.String() + `/edit"`,
		`name="stake"`,
		`value="25"`,
		`name="pick"`,
		`name="odds_id"`,
		// Spread bets get a spread field; the other types' fields must not
		// leak in, or the handler would parse a line the reader never saw.
		`name="custom_spread"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(html, want) {
			t.Errorf("edit form is missing %q", want)
		}
	}

	for _, unwanted := range []string{`name="custom_total"`, `name="custom_home_odds"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("spread edit form rendered %q, which belongs to another bet type", unwanted)
		}
	}

	// The line the bet is actually on has to come back preselected, otherwise
	// saving an unrelated change silently moves the bet to a different book.
	if !strings.Contains(html, `value="`+fanDuel.String()+`" selected`) {
		t.Errorf("the bet's current line %s was not preselected", fanDuel)
	}
	if strings.Contains(html, `value="`+draftKings.String()+`" selected`) {
		t.Errorf("line %s was preselected but the bet is not on it", draftKings)
	}
}

func TestBetsPageHidesEditFormForUneditableBet(t *testing.T) {
	bet, _, _ := editableBet(t)
	// A bet whose game has kicked off, which is what Editable reports.
	bet.Editable = false
	bet.Game.Status = models.GameStatusInProgress

	html := renderBetsPage(t, bet)

	for _, unwanted := range []string{"/edit\"", "Edit Bet", "/cancel\""} {
		if strings.Contains(html, unwanted) {
			t.Errorf("page offered %q for a bet that can no longer be changed", unwanted)
		}
	}
}

func TestBetsPageRendersOverUnderEditFields(t *testing.T) {
	bet, _, _ := editableBet(t)
	bet.Type = "overunder"
	bet.Pick = "Over"
	bet.PickSide = "over"
	bet.Line = "O/U 54.5"

	html := renderBetsPage(t, bet)

	for _, want := range []string{`name="custom_total"`, `name="custom_over_odds"`, `name="custom_under_odds"`, `value="over" selected`} {
		if !strings.Contains(html, want) {
			t.Errorf("over/under edit form is missing %q", want)
		}
	}
	if strings.Contains(html, `name="custom_spread"`) {
		t.Error("over/under edit form rendered a spread field")
	}
}

// renderBetsFilters renders the bets page with a given set of filter options.
func renderBetsFilters(t *testing.T, data map[string]any) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	page := map[string]any{
		"Title": "My Bets",
		"User":  &models.User{Username: "tester"},
		"Bets":  []BetView{},
	}
	maps.Copy(page, data)

	var buf bytes.Buffer
	if err := renderer.Render(&buf, "bets", page); err != nil {
		t.Fatalf("rendering bets page: %v", err)
	}
	return buf.String()
}

// The week dropdown used to be gated on a season already being selected, so it
// was invisible on arrival and took two round trips to reach.
func TestBetsPageRendersWeekFilterWithoutSelectedSeason(t *testing.T) {
	html := renderBetsFilters(t, map[string]any{
		"Seasons": []int{2026},
		"Weeks":   []int{1, 2, 5},
	})

	if !strings.Contains(html, `<select name="week"`) {
		t.Error("week filter is missing when no season is selected")
	}
	for _, week := range []string{">Week 1<", ">Week 2<", ">Week 5<"} {
		if !strings.Contains(html, week) {
			t.Errorf("week option %q is missing", week)
		}
	}
	if !strings.Contains(html, ">All Weeks<") {
		t.Error("week filter has no unset option")
	}
}

func TestBetsPageMarksSelectedWeek(t *testing.T) {
	selected := 2
	html := renderBetsFilters(t, map[string]any{
		"Seasons":      []int{2026},
		"Weeks":        []int{1, 2},
		"SelectedWeek": &selected,
	})

	if !strings.Contains(html, `<option value="2" selected>Week 2</option>`) {
		t.Error("selected week is not marked selected")
	}
	if strings.Contains(html, `<option value="1" selected>`) {
		t.Error("an unselected week is marked selected")
	}
}

func TestBetsPageOmitsWeekFilterWithNoWeeks(t *testing.T) {
	html := renderBetsFilters(t, map[string]any{"Seasons": []int{2026}})

	if strings.Contains(html, `<select name="week"`) {
		t.Error("week filter rendered with no weeks to choose from")
	}
}
