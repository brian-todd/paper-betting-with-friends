package games

import (
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/shopspring/decimal"
)

func decimalOf(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func basketballMatchup() *Matchup {
	m := matchupWith("", "")
	m.Sport = models.SportBasketball
	return m
}

func seasonStats(games int, seed string) *models.TeamSeasonStats {
	v := decimalOf(seed)
	return &models.TeamSeasonStats{
		Games: games, FetchedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
		Pace: v, OffenseRating: v, DefenseRating: v,
		OffenseEffectiveFGPct: v, DefenseEffectiveFGPct: v,
		OffenseTurnoverPct: v, DefenseTurnoverPct: v,
		OffenseReboundPct: v, DefenseReboundPct: v,
		OffenseFreeThrowRate: v, DefenseFreeThrowRate: v,
		OffenseThreePct: v, DefenseThreePct: v,
	}
}

func TestHasEloHidesTwoBasketballTeamsAtTheStartingValue(t *testing.T) {
	start, moved := 1500, 1504
	tests := []struct {
		name       string
		sport      string
		home, away *int
		want       bool
	}{
		{"both at the start", models.SportBasketball, &start, &start, false},
		{"one has moved", models.SportBasketball, &moved, &start, true},
		{"one is missing", models.SportBasketball, &moved, nil, false},
		{"football has no such starting value", models.SportFootball, &start, &start, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := matchupWith("", "")
			m.Sport, m.Home.PregameElo, m.Away.PregameElo = tt.sport, tt.home, tt.away
			if got := m.HasElo(); got != tt.want {
				t.Errorf("HasElo() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Two teams at the starting value are most of a season's first week, and the Elo row
// is all such a game would have, so the panel would be a header over no rows.
func TestHasAnyIgnoresAnEloThePanelWillNotDraw(t *testing.T) {
	start := 1500
	m := matchupWith("", "")
	m.Sport, m.Home.PregameElo, m.Away.PregameElo = models.SportBasketball, &start, &start
	if m.HasAny() {
		t.Error("HasAny() true with nothing but two starting Elos")
	}
}

func TestCoverageNoteNamesTheSportsDivision(t *testing.T) {
	m := basketballMatchup()
	m.Home.Ratings[models.RatingSourceSRS] = rating(models.RatingSourceSRS, "4.2")
	if got := m.CoverageNote(); !strings.Contains(got, "Division I") || strings.Contains(got, "FBS") {
		t.Errorf("CoverageNote() = %q", got)
	}
}

func TestGameDetailRendersABasketballMatchup(t *testing.T) {
	m := basketballMatchup()
	for _, side := range []*TeamStats{&m.Home, &m.Away} {
		adjusted := rating(models.RatingSourceAdjusted, "41.9")
		adjusted.Offense, adjusted.Defense = decimalOf("130.6"), decimalOf("88.8")
		rank := 1
		adjusted.OverallRank, adjusted.OffenseRank, adjusted.DefenseRank = &rank, &rank, &rank
		side.Ratings[models.RatingSourceAdjusted] = adjusted
		side.Ratings[models.RatingSourceSRS] = rating(models.RatingSourceSRS, "22.6")
	}
	m.Home.Season = seasonStats(1, "56.7")
	m.Away.Season = seasonStats(12, "46.2")

	page := renderMatchup(t, m)
	for _, want := range []string{
		"Adjusted", "41.9", "130.6", "88.8", "#1",
		"SRS", "22.6",
		"Efficiency", "Pace", "Points / 100 possessions", "Effective FG%", "Turnover rate",
		"Offensive rebound rate", "Free throw rate", "3-point %",
		"56.7%", "46.2%",
		"Based on 12 games for SYR and 1 game for PIT this season.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	for _, unwanted := range []string{"PPA / play", "FBS teams only", "Special teams"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("a basketball page shows %q", unwanted)
		}
	}
}

// Half a panel invites a conclusion about the missing team, and the feed has no row
// for a team that has not played.
func TestBasketballEfficiencyNeedsBothSides(t *testing.T) {
	m := basketballMatchup()
	m.Home.Season = seasonStats(5, "50")
	if m.HasEfficiency() {
		t.Error("drawn with one side missing")
	}

	m.Away.Season = seasonStats(0, "50")
	if m.HasEfficiency() {
		t.Error("drawn for a team with no games")
	}

	m.Away.Season = seasonStats(1, "50")
	if !m.HasEfficiency() {
		t.Error("not drawn with a game on each side")
	}
}

// A figure the feed reports as null is a dash, not a zero.
func TestBasketballEfficiencyLeavesAMissingFigureEmpty(t *testing.T) {
	m := basketballMatchup()
	m.Home.Season = seasonStats(5, "50")
	m.Away.Season = seasonStats(5, "50")
	m.Away.Season.OffenseTurnoverPct = nil

	for _, metric := range m.EfficiencyMetrics() {
		if metric.Label == "Turnover rate" {
			if metric.Units[0].Away != "" || metric.Units[0].Home != "50.0%" {
				t.Errorf("units = %+v", metric.Units[0])
			}
			return
		}
	}
	t.Error("no turnover rate metric")
}
