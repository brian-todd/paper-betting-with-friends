package games

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

// renderMatchup renders the real game detail page with one matchup panel.
// Executing rather than parsing is the check worth having: a parse succeeds
// happily on a field the panel does not have.
//
// The result is unescaped first. html/template writes "+" as "&#43;" -- correct
// HTML that a browser shows as "+", but it would make every assertion about a
// label like "SP+" read as a puzzle.
func renderMatchup(t *testing.T, matchup *Matchup) string {
	t.Helper()
	return html.UnescapeString(renderGameDetail(t, uuid.New(), map[string]any{"Matchup": matchup}))
}

// A football game synced before the stats job first ran, and every basketball
// game ever, reach this template with no panel at all.
func TestGameDetailRendersWithoutAMatchup(t *testing.T) {
	page := renderMatchup(t, nil)

	if strings.Contains(page, "matchup-table") {
		t.Error("the panel was drawn for a game with no matchup")
	}
	// The rest of the page still has to be there -- an absent panel is not an
	// absent page.
	if !strings.Contains(page, "Game Information") {
		t.Error("the page did not render its game information")
	}
}

// FBS hosting FCS: /records covers every division but the ratings cover FBS
// alone, so this is a normal September Saturday rather than an edge case.
func TestGameDetailDropsRatingRowsWhenOnlyOneSideIsRated(t *testing.T) {
	matchup := matchupWith("14.2", "")
	matchup.Home.Record = &models.TeamRecord{
		Games: 2, Wins: 2, HomeGames: 1, HomeWins: 1,
		FetchedAt: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC),
	}
	matchup.Away.Record = &models.TeamRecord{
		Games: 2, Wins: 1, Losses: 1, AwayGames: 1, AwayLosses: 1,
		FetchedAt: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC),
	}

	page := renderMatchup(t, matchup)

	if !strings.Contains(page, "matchup-table") {
		t.Fatal("the panel was not drawn")
	}
	// Both records are present, so the panel is worth drawing...
	if !strings.Contains(page, "2-0") || !strings.Contains(page, "1-1") {
		t.Error("records did not render")
	}
	// ...but the SP+ row must not appear with one side blank. A row reading
	// "— | 14.2" reads as "the visitor is terrible" rather than "unrated".
	if strings.Contains(page, "SP+") {
		t.Error("an SP+ row was drawn with only one side rated")
	}
	// And the reader is told why the panel is short.
	if !strings.Contains(page, "FBS teams only") {
		t.Error("the FBS-only footnote is missing")
	}
}

func TestGameDetailRendersAFullMatchup(t *testing.T) {
	fetched := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	rank := 22

	matchup := matchupWith("14.2", "8.4")
	home := matchup.Home.Ratings[models.RatingSourceSP]
	home.FetchedAt, home.OverallRank = fetched, &rank
	matchup.Home.Ratings[models.RatingSourceSP] = home

	away := matchup.Away.Ratings[models.RatingSourceSP]
	away.FetchedAt = fetched
	matchup.Away.Ratings[models.RatingSourceSP] = away

	homeElo, awayElo := 1681, 1288
	matchup.Home.PregameElo, matchup.Away.PregameElo = &homeElo, &awayElo
	matchup.Home.Record = &models.TeamRecord{Games: 2, Wins: 2, FetchedAt: fetched}
	matchup.Away.Record = &models.TeamRecord{Games: 2, Wins: 1, Losses: 1, FetchedAt: fetched}

	finalized := time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)
	matchup.Home.Form = []RecentResult{{
		For: 31, Against: 17, AtHome: true, Won: new(true),
		Opponent: models.Team{Name: "West Virginia"},
		Game:     models.Game{Result: &models.GameResult{FinalizedAt: &finalized}},
	}}

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"matchup-table",
		"SP+",
		"14.2",
		"#22",
		"1681",
		// The projection is the one number the page computes rather than
		// sources, and it belongs in the SP+ row as a parenthetical.
		"projects PIT by 8.3",
		"matchup-projection",
		"form-W",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// The stored precision is three decimal places so FPI is not rounded on the
	// way in, but nobody wants to read "14.200" beside a point spread.
	if strings.Contains(page, "14.200") {
		t.Error("a rating rendered with its storage precision")
	}

	// Without the as-of line a reader assumes the numbers are live.
	if !strings.Contains(page, "as of") {
		t.Error("the as-of line is missing")
	}
	// Times are rendered through localTime, never .Format, or the server's UTC
	// leaks into the page.
	if !strings.Contains(page, `data-format="mediumdate"`) {
		t.Error("the as-of date was not rendered through localTime")
	}

	// The FBS-only footnote describes a panel short some rows. This one is not.
	if strings.Contains(page, "FBS teams only") {
		t.Error("the FBS-only footnote was drawn for a fully rated matchup")
	}
}

// Every rating row decomposes into its units in place, beneath the number they
// add up to.
func TestGameDetailRendersRatingUnitRows(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	setSPUnits(&matchup.Home, "34.700", "18.200", "0.600")
	setSPUnits(&matchup.Away, "29.100", "21.500", "-1.400")

	rank := 12
	home := matchup.Home.Ratings[models.RatingSourceSP]
	home.DefenseRank = &rank
	matchup.Home.Ratings[models.RatingSourceSP] = home

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"matchup-subrow",
		"Offense",
		"Defense",
		"Special teams",
		"34.7",
		"21.5",
		"-1.4",
		"#12",
		// Without this the two units read as one elite and one dreadful, since
		// SP+ scores them in opposite directions. Said once, on the row they
		// sit under.
		"offense high, defense low",
		// A leaf row keeps the muted label styling; a row with units under it
		// reads as the heading it is.
		"matchup-parent",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// The units sit under the SP+ row, not above it or in a table of their own.
	if strings.Index(page, "SP+") > strings.Index(page, "matchup-subrow") {
		t.Error("the unit rows were drawn before the rating they decompose")
	}
	// Stored precision is three places so nothing is lost on the way in; one is
	// what a reader wants, here as much as on the row above.
	if strings.Contains(page, "34.700") {
		t.Error("a unit rating rendered with its storage precision")
	}
}

// FPI and CORE break out too, each labelled with its own scale -- and the two
// defences read opposite ways, so the page never lets them share a caption.
func TestGameDetailRendersUnitsForEverySource(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	for _, side := range []*TeamStats{&matchup.Home, &matchup.Away} {
		setUnits(side, models.RatingSourceFPI, "88.1", "90.1", "72.5")
		setUnits(side, models.RatingSourceCORE, "8.4", "-10.8", "")
	}

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"FPI",
		"CORE",
		"90.1",
		"-10.8",
		// FPI runs both units upwards; CORE and SP+ want a low defence.
		"percentiles; higher is better throughout",
		"opponent-relative efficiency; offense high, defense low",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// CORE publishes no special teams rating and gets no row for one, even
	// though FPI's sits a few rows above.
	if strings.Count(page, "Special teams") != 1 {
		t.Error("special teams rows were drawn for a source that publishes none")
	}
}

// The headline rating is NOT NULL while the units are nullable, so a matchup
// with ratings and no units is ordinary and draws the row alone.
func TestGameDetailDrawsTheSPRowWithoutItsUnits(t *testing.T) {
	page := renderMatchup(t, matchupWith("14.2", "8.4"))

	if !strings.Contains(page, "SP+") {
		t.Fatal("the SP+ row was not drawn")
	}
	if strings.Contains(page, "matchup-subrow") {
		t.Error("unit rows were drawn for a matchup with no unit ratings")
	}
}

// Recent form is a row per finished game, each linking back to it, under one
// header spanning the lot.
func TestGameDetailRendersRecentGameRows(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	matchup.Home.Form = []RecentResult{
		formResult(31, 17, "WVU", true),
		formResult(14, 28, "OSU", false),
	}
	matchup.Away.Form = []RecentResult{formResult(38, 10, "UNH", true)}

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"Recent",
		"31-17",
		"vs WVU",
		// A road game is marked, because it is a different result to the same
		// scoreline at home.
		"@ OSU",
		"38-10",
		"recent-opponent",
		// One header for the whole block rather than a label per row.
		`rowspan="2"`,
		// The outcome badge survived the move off the old chip strip.
		"form-chip",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// Every result links to the game it came from.
	for _, result := range append(matchup.Home.Form, matchup.Away.Form...) {
		href := `href="/games/` + result.Game.ID.String() + `"`
		if !strings.Contains(page, href) {
			t.Errorf("page is missing a link to %s", result.Game.ID)
		}
	}

	// The visitor has played once, so its second row is a dash rather than a
	// fabricated result.
	if !strings.Contains(page, "no-data") {
		t.Error("the shorter side's empty row did not render a dash")
	}
	// The rows are in order and say so once. Counting them off individually is
	// five rows of arithmetic the reader can already do.
	if strings.Contains(page, "games ago") {
		t.Error("a per-row recency label was drawn")
	}
}

// Week 1: no finished games, so no rows and no header for them.
func TestGameDetailDrawsNoRecentRowsInWeekOne(t *testing.T) {
	page := renderMatchup(t, matchupWith("14.2", "8.4"))

	if strings.Contains(page, "recent-row") {
		t.Error("a recent-games row was drawn for a team with no finished games")
	}
	if strings.Contains(page, "form-chip") {
		t.Error("an outcome badge was drawn with no form")
	}
	if strings.Contains(page, "Recent") {
		t.Error("the recent-form header was drawn with nothing under it")
	}
}
