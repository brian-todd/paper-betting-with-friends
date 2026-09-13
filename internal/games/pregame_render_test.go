package games

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// renderForecast renders the real page with a forecast and no matchup, which is
// how a game reaches this template before the team stats job has ever run.
func renderForecast(t *testing.T, forecast *models.GameForecast) string {
	t.Helper()
	return html.UnescapeString(renderGameDetail(t, uuid.New(), map[string]any{"Forecast": forecast}))
}

func TestGameDetailRendersTheATSRow(t *testing.T) {
	fetched := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	homeMargin := decimal.RequireFromString("3.5")
	awayMargin := decimal.RequireFromString("-12.17")

	matchup := matchupWith("", "")
	matchup.Home.ATS = &models.TeamATSRecord{
		Games: 11, ATSWins: 6, ATSLosses: 4, ATSPushes: 1,
		AvgCoverMargin: &homeMargin, FetchedAt: fetched,
	}
	matchup.Away.ATS = &models.TeamATSRecord{
		Games: 11, ATSWins: 4, ATSLosses: 7,
		AvgCoverMargin: &awayMargin, FetchedAt: fetched,
	}

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"Against the spread",
		// A push is a stake returned, so it shows in the record rather than
		// being folded into the losses.
		"6-4-1",
		"4-7",
		"(+3.5)",
		"(-12.2)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

// 0-0 against the spread is not a record, and rendering it beside an opponent's
// real one would invite a comparison that does not exist.
func TestGameDetailDropsTheATSRowWithoutGradedGames(t *testing.T) {
	matchup := matchupWith("", "")
	matchup.Home.ATS = &models.TeamATSRecord{Games: 2, ATSWins: 1, ATSLosses: 1}
	matchup.Away.ATS = &models.TeamATSRecord{}

	if strings.Contains(renderMatchup(t, matchup), "Against the spread") {
		t.Error("the ATS row was drawn for a side with no graded games")
	}
}

func TestGameDetailRendersTheWinProbabilityRow(t *testing.T) {
	matchup := matchupWith("", "")
	matchup.WinProbability = &models.GamePregameWinProbability{
		HomeWinProbability: decimal.RequireFromString("0.886"),
		Spread:             decimal.RequireFromString("-17.5"),
		FetchedAt:          time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC),
	}

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"Win probability",
		"89%",
		// The complement, so the two always total 100 rather than each
		// rounding independently.
		"11%",
		// The line it was derived from, which is not necessarily a line we
		// hold. Without it the number is a claim with no stated basis.
		"-17.5",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// Three decimal places are stored because the feed publishes three. Nobody
	// reads a pre-game probability to a tenth of a percent.
	if strings.Contains(page, "0.886") {
		t.Error("the probability rendered at its storage precision")
	}
}

// Most games have no row: the metric is derived from a posted line, so its
// coverage follows the market. Nothing is substituted.
func TestGameDetailDropsTheWinProbabilityRowWhenUnpublished(t *testing.T) {
	if strings.Contains(renderMatchup(t, matchupWith("14.2", "8.4")), "Win probability") {
		t.Error("a win probability row was drawn for a game with none")
	}
}

func TestGameDetailRendersTheEfficiencyPanel(t *testing.T) {
	matchup := matchupWith("", "")
	matchup.Home.Advanced = advancedFor("0.241", "0.503")
	matchup.Away.Advanced = advancedFor("-0.185", "0.287")

	page := renderMatchup(t, matchup)

	for _, want := range []string{
		"Efficiency",
		"Offense",
		"Defense",
		// The metrics nest under those two headings rather than sitting flush
		// against them, the same shape the SP+ units use. This matchup has no
		// ratings and no form, so these are the only sub-rows on the page.
		"matchup-subrow",
		"PPA / play",
		"+0.24",
		"50.3%",
		// The two units invert against each other, and the metric says so
		// rather than trusting the reader to hold two conventions at once.
		"Success rate",
		"more for an offense, fewer for a defense",
		// Havoc is the exception, and says so in its own words.
		"more for a defense, fewer for an offense",
		// Two teams three games in look exactly as authoritative as two teams
		// ten games in unless the panel says otherwise.
		"offensive plays for SYR",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

func TestGameDetailDropsTheEfficiencyPanelWhenOnlyOneSideIsCovered(t *testing.T) {
	matchup := matchupWith("14.2", "")
	matchup.Home.Advanced = advancedFor("0.241", "0.503")

	page := renderMatchup(t, matchup)

	if strings.Contains(page, "efficiency-card") {
		t.Error("the efficiency panel was drawn with only one side covered")
	}
	// The footnote that explains the gap covers efficiency as well as ratings,
	// since both are published for FBS alone.
	if !strings.Contains(page, "FBS teams only") {
		t.Error("the FBS-only footnote is missing")
	}
}

func TestGameDetailRendersTheForecast(t *testing.T) {
	temperature := decimal.RequireFromString("44.4")
	wind := decimal.RequireFromString("12.7")
	precipitation := decimal.RequireFromString("0.343")
	condition := "Light Rain"
	humidity := 93

	page := renderForecast(t, &models.GameForecast{
		Temperature:      &temperature,
		WindSpeed:        &wind,
		Precipitation:    &precipitation,
		WeatherCondition: &condition,
		Humidity:         &humidity,
		FetchedAt:        time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC),
	})

	for _, want := range []string{
		"Kickoff forecast",
		"44°F",
		"13 mph",
		"Light Rain",
		"0.34 in rain",
		"93%",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// A forecast moves all afternoon where a rating moves once a week, so
	// staleness matters more here than anywhere else on the page -- and the
	// time goes through localTime, never .Format.
	if !strings.Contains(page, `data-format="datetime"`) {
		t.Error("the forecast's as-of time was not rendered through localTime")
	}
}

// A forecast with no label still has numbers, which is what the panel draws
// from. 47 of 75 week-3 rows had no label a week out.
func TestGameDetailRendersAForecastWithNoCondition(t *testing.T) {
	temperature := decimal.RequireFromString("86.5")
	page := renderForecast(t, &models.GameForecast{Temperature: &temperature})

	if !strings.Contains(page, "87°F") {
		t.Error("the temperature is missing")
	}
	if strings.Contains(page, "Conditions") {
		t.Error("an empty conditions row was drawn")
	}
	// No rain forecast is the default state of a football game and does not
	// need a row of its own.
	if strings.Contains(page, "Precipitation") {
		t.Error("a precipitation row was drawn for a dry forecast")
	}
}

// The service refuses to pass an indoor forecast through, but the template is
// the last line and a dome's forecast is fully populated and entirely
// plausible -- it is the weather outside the roof.
func TestGameDetailDrawsNoForecastSection(t *testing.T) {
	page := renderForecast(t, nil)

	if strings.Contains(page, "Kickoff forecast") {
		t.Error("the forecast section was drawn with no forecast")
	}
	if !strings.Contains(page, "Game Information") {
		t.Error("the page did not render its game information")
	}
}
