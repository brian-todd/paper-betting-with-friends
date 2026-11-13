package games

import (
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/shopspring/decimal"
)

// baseline100 is a national average chosen so the arithmetic reads off the page:
// 100 points per 100 possessions at 70 possessions a game.
var baseline100 = repository.AdjustedBaseline{
	Efficiency: decimal.RequireFromString("100"),
	Pace:       decimal.RequireFromString("70"),
	Teams:      2,
}

func setAdjusted(side *TeamStats, offense, defense, pace string) {
	side.Ratings[models.RatingSourceAdjusted] = models.TeamRating{
		Source:  models.RatingSourceAdjusted,
		Offense: decimalOf(offense),
		Defense: decimalOf(defense),
	}
	side.Season = &models.TeamSeasonStats{Games: 5, Pace: decimalOf(pace)}
}

func projectedMatchup(neutral bool) *Matchup {
	m := basketballMatchup()
	m.NeutralSite = neutral
	setAdjusted(&m.Home, "110", "95", "70")
	setAdjusted(&m.Away, "100", "105", "70")
	return m
}

func TestProjectScoreWorksFromEfficiencyAndPace(t *testing.T) {
	// 70 possessions. Home scores 110 x 105 / 100 per 100, which is 80.85; away
	// scores 100 x 95 / 100, which is 66.5. A 14.35-point margin.
	neutral := projectScore(projectedMatchup(true), baseline100)
	if neutral == nil {
		t.Fatal("projectScore() = nil for two rated teams")
	}
	// 147.35 combined, shared out by largest remainder: 80.85 takes the point
	// that 66.5 does not.
	if neutral.Home != 81 || neutral.Away != 66 {
		t.Errorf("neutral site = %d-%d, want 81-66", neutral.Home, neutral.Away)
	}
	if neutral.HomePercent != 90 || neutral.AwayPercent != 10 {
		t.Errorf("neutral site chances = %d%%/%d%%, want 90%%/10%%", neutral.HomePercent, neutral.AwayPercent)
	}

	// Home court moves the margin by three points and leaves the total where it was.
	home := projectScore(projectedMatchup(false), baseline100)
	if home.Home != 82 || home.Away != 65 {
		t.Errorf("home site = %d-%d, want 82-65", home.Home, home.Away)
	}
	if home.HomePercent != 94 {
		t.Errorf("home site chance = %d%%, want 94%%", home.HomePercent)
	}
	// 82.35 and 65.0 rather than 80.85 and 66.5: the same 147.35 combined.
	if home.Total() != 147 || neutral.Total() != 147 {
		t.Errorf("totals = %d home, %d neutral; want 147 for both", home.Total(), neutral.Total())
	}
}

func TestProjectScoreUsesThePaceOfBothTeams(t *testing.T) {
	m := basketballMatchup()
	m.NeutralSite = true
	// Two average teams, one fast and one slow: 80 x 60 / 70 is 68.57 possessions,
	// each scoring 68.57 at a national-average efficiency, 137.14 between them.
	setAdjusted(&m.Home, "100", "100", "80")
	setAdjusted(&m.Away, "100", "100", "60")

	got := projectScore(m, baseline100)
	if got.Total() != 137 || got.HomePercent != 50 {
		t.Errorf("got a total of %d at %d%%, want 137 at 50%%", got.Total(), got.HomePercent)
	}
}

// Both or neither, as with the rating rows: a side missing any one input leaves
// nothing to project with.
func TestProjectScoreNeedsEveryInputOnBothSides(t *testing.T) {
	tests := []struct {
		name string
		drop func(m *Matchup)
	}{
		{"away unrated", func(m *Matchup) { delete(m.Away.Ratings, models.RatingSourceAdjusted) }},
		{"home has no offense", func(m *Matchup) {
			r := m.Home.Ratings[models.RatingSourceAdjusted]
			r.Offense = nil
			m.Home.Ratings[models.RatingSourceAdjusted] = r
		}},
		{"away has no defense", func(m *Matchup) {
			r := m.Away.Ratings[models.RatingSourceAdjusted]
			r.Defense = nil
			m.Away.Ratings[models.RatingSourceAdjusted] = r
		}},
		{"home has no season", func(m *Matchup) { m.Home.Season = nil }},
		{"away has no pace", func(m *Matchup) { m.Away.Season.Pace = nil }},
		{"football", func(m *Matchup) { m.Sport = models.SportFootball }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := projectedMatchup(false)
			tt.drop(m)
			if got := projectScore(m, baseline100); got != nil {
				t.Errorf("projectScore() = %+v, want nil", got)
			}
		})
	}

	if got := projectScore(projectedMatchup(false), repository.AdjustedBaseline{}); got != nil {
		t.Errorf("projectScore() with no baseline = %+v, want nil", got)
	}
	if got := projectScore(nil, baseline100); got != nil {
		t.Errorf("projectScore(nil) = %+v, want nil", got)
	}
}

// The page sets the total beside the O/U line and the score beside the win chance,
// so the rounded scores have to agree with both.
func TestRoundScoresKeepsTheTotalAndTheFavourite(t *testing.T) {
	tests := []struct {
		name               string
		home, away         float64
		wantHome, wantAway int
	}{
		// Rounded one at a time these are 79-62, a total of 141 over a projection
		// of 140.33.
		{"total rounds once", 78.5, 61.83, 78, 62},
		{"two remainders carry", 70.9, 60.8, 71, 61},
		{"already whole", 82, 65, 82, 65},
		// 70-70 by largest remainder, beside a 53% win chance.
		{"tie goes to the home favourite", 70.4, 69.6, 71, 69},
		{"tie goes to the away favourite", 69.6, 70.4, 69, 71},
		{"a true tie stays one", 70, 70, 70, 70},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, a := roundScores(tt.home, tt.away)
			if h != tt.wantHome || a != tt.wantAway {
				t.Errorf("roundScores(%v, %v) = %d-%d, want %d-%d", tt.home, tt.away, h, a, tt.wantHome, tt.wantAway)
			}
		})
	}
}

func TestWinPercentIsSymmetricAndNeverCertain(t *testing.T) {
	tests := []struct {
		margin float64
		want   int
	}{
		{0, 50},
		{11, 84},
		{-11, 16},
		{60, 99},
		{-60, 1},
	}
	for _, tt := range tests {
		if got := winPercent(tt.margin); got != tt.want {
			t.Errorf("winPercent(%v) = %d, want %d", tt.margin, got, tt.want)
		}
	}
}

func TestLineTextNamesTheFavourite(t *testing.T) {
	home := models.Team{Abbreviation: "DUKE"}
	away := models.Team{Abbreviation: "UNC"}
	spread := func(homeSpread string) *models.SpreadOdds {
		h := decimal.RequireFromString(homeSpread)
		return &models.SpreadOdds{HomeSpread: h, AwaySpread: h.Neg()}
	}
	total := &models.OverUnderOdds{Total: decimal.RequireFromString("151.5")}

	tests := []struct {
		name string
		p    *ScoreProjection
		want string
	}{
		{"home favoured", &ScoreProjection{Spread: spread("-4.5"), OverUnder: total}, "DUKE -4.5, O/U 151.5"},
		{"away favoured", &ScoreProjection{Spread: spread("3")}, "UNC -3"},
		{"pick'em", &ScoreProjection{Spread: spread("0")}, "pick'em"},
		{"total only", &ScoreProjection{OverUnder: total}, "O/U 151.5"},
		{"no line", &ScoreProjection{}, ""},
		{"no projection", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.LineText(home, away); got != tt.want {
				t.Errorf("LineText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGameDetailRendersTheProjection(t *testing.T) {
	m := projectedMatchup(false)
	m.ScoreProjection = projectScore(m, baseline100)
	h := decimal.RequireFromString("-4.5")
	m.ScoreProjection.Spread = &models.SpreadOdds{HomeSpread: h, AwaySpread: h.Neg()}

	page := renderMatchup(t, m)
	for _, want := range []string{
		"Projected score",
		"(total 147; line PIT -4.5)",
		"<td>65</td>",
		"<td>82</td>",
		"Win chance",
		"<td>6%</td>",
		"<td>94%</td>",
		"with 3 points for home court",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page is missing %q", want)
		}
	}
}

func TestGameDetailOmitsHomeCourtAtANeutralSite(t *testing.T) {
	m := projectedMatchup(true)
	m.ScoreProjection = projectScore(m, baseline100)

	page := renderMatchup(t, m)
	if !strings.Contains(page, "Projected from adjusted efficiency and pace.") {
		t.Error("the footnote is missing")
	}
	if strings.Contains(page, "home court") {
		t.Error("a neutral-site projection claimed a home-court adjustment")
	}
	if strings.Contains(page, "; line") {
		t.Error("a line was drawn for a game with none posted")
	}
}

func TestGameDetailDrawsNoProjectionWithoutOne(t *testing.T) {
	page := renderMatchup(t, projectedMatchup(false))
	for _, absent := range []string{"Projected score", "Win chance", "Projected from"} {
		if strings.Contains(page, absent) {
			t.Errorf("the page drew %q with no projection", absent)
		}
	}
}
