package games

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func rating(source models.RatingSource, overall string) models.TeamRating {
	return models.TeamRating{Source: source, Overall: decimal.RequireFromString(overall)}
}

// matchupWith builds a panel from SP+ ratings alone, which is what most of the
// projection and row-visibility rules turn on.
func matchupWith(homeSP, awaySP string) *Matchup {
	m := &Matchup{
		Home: TeamStats{
			Team:    models.Team{Abbreviation: "PIT"},
			Ratings: map[models.RatingSource]models.TeamRating{},
		},
		Away: TeamStats{
			Team:    models.Team{Abbreviation: "SYR"},
			Ratings: map[models.RatingSource]models.TeamRating{},
		},
	}
	if homeSP != "" {
		m.Home.Ratings[models.RatingSourceSP] = rating(models.RatingSourceSP, homeSP)
	}
	if awaySP != "" {
		m.Away.Ratings[models.RatingSourceSP] = rating(models.RatingSourceSP, awaySP)
	}
	return m
}

func TestSPProjectionAppliesHomeFieldAndNamesTheFavourite(t *testing.T) {
	tests := []struct {
		name         string
		homeSP       string
		awaySP       string
		neutral      bool
		wantFavorite string
		wantMargin   string
	}{
		{
			name:         "home side favoured, home field widens it",
			homeSP:       "14.2",
			awaySP:       "8.4",
			wantFavorite: "PIT",
			wantMargin:   "8.3",
		},
		{
			// The home field is worth 2.5, so a visitor rated 3 points better
			// is favoured by half a point rather than by three.
			name:         "away side better but home field narrows it",
			homeSP:       "10",
			awaySP:       "13",
			wantFavorite: "SYR",
			wantMargin:   "0.5",
		},
		{
			// A neutral site gets no adjustment at all, which is the case that
			// would silently read as a 2.5-point home edge if it were skipped.
			name:         "neutral site applies no adjustment",
			homeSP:       "14.2",
			awaySP:       "8.4",
			neutral:      true,
			wantFavorite: "PIT",
			wantMargin:   "5.8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := matchupWith(tt.homeSP, tt.awaySP)
			m.NeutralSite = tt.neutral

			got := m.SPProjection()
			if got == nil {
				t.Fatal("SPProjection() = nil, want a projection")
			}
			if got.Favorite.Abbreviation != tt.wantFavorite {
				t.Errorf("Favorite = %q, want %q", got.Favorite.Abbreviation, tt.wantFavorite)
			}
			if want := decimal.RequireFromString(tt.wantMargin); !got.Margin.Equal(want) {
				t.Errorf("Margin = %s, want %s", got.Margin, want)
			}
			// The margin is how far ahead the favourite is, so it is never
			// negative whichever side that turns out to be.
			if got.Margin.IsNegative() {
				t.Errorf("Margin = %s, want a non-negative margin", got.Margin)
			}
		})
	}
}

// There is nothing to difference when only one side is rated, and a
// division-average stand-in would be inventing the input.
func TestSPProjectionIsAbsentUnlessBothSidesAreRated(t *testing.T) {
	for _, tt := range []struct {
		name   string
		homeSP string
		awaySP string
	}{
		{"away side unrated", "14.2", ""},
		{"home side unrated", "", "8.4"},
		{"neither side rated", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchupWith(tt.homeSP, tt.awaySP).SPProjection(); got != nil {
				t.Errorf("SPProjection() = %+v, want nil", got)
			}
		})
	}
}

// A row reading "— | 14.2" invites the reader to conclude the unrated team is
// bad rather than uncovered. FBS hosting FCS is a normal September Saturday, so
// this is the common case and not an edge one.
func TestRatingRowsNeedBothSides(t *testing.T) {
	m := matchupWith("14.2", "8.4")
	m.Home.Ratings[models.RatingSourceFPI] = rating(models.RatingSourceFPI, "9.7")
	m.Home.Ratings[models.RatingSourceCORE] = rating(models.RatingSourceCORE, "5.2")
	m.Away.Ratings[models.RatingSourceCORE] = rating(models.RatingSourceCORE, "1.1")

	got := m.RatingRows()
	want := []models.RatingSource{models.RatingSourceSP, models.RatingSourceCORE}
	if len(got) != len(want) {
		t.Fatalf("RatingRows() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RatingRows() = %v, want %v", got, want)
		}
	}
}

// The footnote explains a panel that is short a few rows because one side is
// outside the divisions the ratings cover. Two unrated teams is a sync that has
// not run, which the footnote would misdescribe.
func TestRatingsAreFBSOnlyOnlyWhenExactlyOneSideIsRated(t *testing.T) {
	tests := []struct {
		name   string
		homeSP string
		awaySP string
		want   bool
	}{
		{"one side rated", "14.2", "", true},
		{"other side rated", "", "8.4", true},
		{"both rated", "14.2", "8.4", false},
		{"neither rated", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchupWith(tt.homeSP, tt.awaySP).RatingsAreFBSOnly(); got != tt.want {
				t.Errorf("RatingsAreFBSOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The panel is drawn only when it has something in it. A nil receiver has to be
// safe too: the template calls these on a game that has none.
func TestHasAnyAndNilReceiversAreSafe(t *testing.T) {
	var absent *Matchup
	if absent.HasAny() || absent.HasElo() || absent.RatingsAreFBSOnly() {
		t.Error("a nil matchup reported content")
	}
	if absent.RatingRows() != nil || absent.SPProjection() != nil || absent.AsOf() != nil {
		t.Error("a nil matchup returned content")
	}

	empty := matchupWith("", "")
	if empty.HasAny() {
		t.Error("an empty matchup reported content")
	}

	// Pregame Elo arrives with the game rather than the stats sync, so it is on
	// its own enough to draw the panel.
	elo := 1681
	empty.Home.PregameElo = &elo
	if !empty.HasAny() {
		t.Error("a matchup with a pregame Elo reported no content")
	}
	// ...but the Elo row still needs both sides.
	if empty.HasElo() {
		t.Error("HasElo() true with only one side")
	}
}

// The "as of" line exists to tell a reader how stale the panel might be, so
// claiming it is older than it is would be its own kind of wrong.
func TestAsOfReportsTheNewestFetch(t *testing.T) {
	older := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC)

	m := matchupWith("14.2", "8.4")
	home := m.Home.Ratings[models.RatingSourceSP]
	home.FetchedAt = older
	m.Home.Ratings[models.RatingSourceSP] = home
	m.Away.Record = &models.TeamRecord{FetchedAt: newer}

	got := m.AsOf()
	if got == nil {
		t.Fatal("AsOf() = nil, want the newest fetch")
	}
	if !got.Equal(newer) {
		t.Errorf("AsOf() = %s, want %s", got, newer)
	}

	// Pregame Elo carries no fetch time of its own, so a panel with nothing but
	// Elo has no "as of" line rather than a zero one.
	elo := 1681
	bare := matchupWith("", "")
	bare.Home.PregameElo = &elo
	if got := bare.AsOf(); got != nil {
		t.Errorf("AsOf() = %v, want nil", got)
	}
}

func TestRecentResultFromReadsTheGameFromOneTeamsSide(t *testing.T) {
	home, away := uuid.New(), uuid.New()
	finalized := time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)

	game := models.Game{
		HomeTeamID: home,
		AwayTeamID: away,
		HomeTeam:   models.Team{Name: "Pittsburgh"},
		AwayTeam:   models.Team{Name: "Syracuse"},
		Result:     &models.GameResult{HomeScore: 31, AwayScore: 17, FinalizedAt: &finalized},
	}

	got, ok := recentResultFrom(game, home)
	if !ok {
		t.Fatal("recentResultFrom() rejected a final game")
	}
	if got.Outcome() != "W" || got.Score() != "31-17" || !got.AtHome {
		t.Errorf("home view = %s %s home=%v, want W 31-17 home=true", got.Outcome(), got.Score(), got.AtHome)
	}
	if got.Opponent.Name != "Syracuse" {
		t.Errorf("home view opponent = %q, want Syracuse", got.Opponent.Name)
	}

	// The same row from the other side flips everything, which is the point of
	// the helper -- the caller never has to know which side it was on.
	got, ok = recentResultFrom(game, away)
	if !ok {
		t.Fatal("recentResultFrom() rejected a final game")
	}
	if got.Outcome() != "L" || got.Score() != "17-31" || got.AtHome {
		t.Errorf("away view = %s %s home=%v, want L 17-31 home=false", got.Outcome(), got.Score(), got.AtHome)
	}
	if got.Opponent.Name != "Pittsburgh" {
		t.Errorf("away view opponent = %q, want Pittsburgh", got.Opponent.Name)
	}
}

// A game_results row exists as soon as a score is reported, so a game currently
// 7-3 in the first quarter is not a loss. The query filters these out; this is
// the second line of that defence.
func TestRecentResultFromRejectsAProvisionalScore(t *testing.T) {
	home, away := uuid.New(), uuid.New()

	live := models.Game{
		HomeTeamID: home,
		AwayTeamID: away,
		Result:     &models.GameResult{HomeScore: 3, AwayScore: 7},
	}
	if _, ok := recentResultFrom(live, home); ok {
		t.Error("recentResultFrom() accepted a game with no finalized_at")
	}

	unplayed := models.Game{HomeTeamID: home, AwayTeamID: away}
	if _, ok := recentResultFrom(unplayed, home); ok {
		t.Error("recentResultFrom() accepted a game with no result at all")
	}
}

func TestRecentResultReportsATie(t *testing.T) {
	home := uuid.New()
	finalized := time.Now()

	game := models.Game{
		HomeTeamID: home,
		AwayTeamID: uuid.New(),
		Result:     &models.GameResult{HomeScore: 21, AwayScore: 21, FinalizedAt: &finalized},
	}

	got, ok := recentResultFrom(game, home)
	if !ok {
		t.Fatal("recentResultFrom() rejected a final game")
	}
	if got.Won != nil {
		t.Errorf("Won = %v, want nil for a tie", *got.Won)
	}
	if got.Outcome() != "T" {
		t.Errorf("Outcome() = %q, want T", got.Outcome())
	}
}

// setUnits fills in one side's unit ratings for a source, leaving the headline
// number alone. An empty string leaves that unit unset, which is how the
// provider delivers a good many of them.
func setUnits(side *TeamStats, source models.RatingSource, offense, defense, special string) {
	rating, ok := side.Ratings[source]
	if !ok {
		rating = models.TeamRating{Source: source, Overall: decimal.RequireFromString("10")}
	}
	if offense != "" {
		rating.Offense = decPtr(offense)
	}
	if defense != "" {
		rating.Defense = decPtr(defense)
	}
	if special != "" {
		rating.SpecialTeams = decPtr(special)
	}
	side.Ratings[source] = rating
}

// setSPUnits is the SP+ case, which most of these tests are about.
func setSPUnits(side *TeamStats, offense, defense, special string) {
	setUnits(side, models.RatingSourceSP, offense, defense, special)
}

func TestRatingUnitsBreakOutEveryUnitBothSidesHave(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	setSPUnits(&matchup.Home, "34.700", "18.200", "0.000")
	setSPUnits(&matchup.Away, "29.100", "21.500", "-1.400")

	rank := 12
	home := matchup.Home.Ratings[models.RatingSourceSP]
	home.OffenseRank = &rank
	matchup.Home.Ratings[models.RatingSourceSP] = home

	units := matchup.RatingUnits(models.RatingSourceSP)
	if len(units) != 3 {
		t.Fatalf("RatingUnits() returned %d rows, want 3", len(units))
	}

	want := []struct{ label, away, home string }{
		{"Offense", "29.1", "34.7"},
		{"Defense", "21.5", "18.2"},
		// A special teams rating of exactly zero is an ordinary team, not a
		// missing number, and the row stays.
		{"Special teams", "-1.4", "0"},
	}
	for i, w := range want {
		if units[i].Label != w.label {
			t.Errorf("row %d label = %q, want %q", i, units[i].Label, w.label)
		}
		if units[i].Away != w.away || units[i].Home != w.home {
			t.Errorf("row %d = %q | %q, want %q | %q", i, units[i].Away, units[i].Home, w.away, w.home)
		}
	}

	if units[0].HomeRank == nil || *units[0].HomeRank != 12 {
		t.Error("the offense rank did not carry through")
	}
	// /ratings/sp publishes no ranking for special teams, so nothing may appear.
	if units[2].HomeRank != nil || units[2].AwayRank != nil {
		t.Error("a special teams rank was invented")
	}
}

// The units follow the same both-sides-or-neither rule as the rating rows, and
// each applies it on its own: the nested objects are sparsely populated and a
// side can carry one unit without the others.
func TestRatingUnitsDropAUnitOnlyOneSideHas(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	setSPUnits(&matchup.Home, "34.700", "18.200", "0.600")
	setSPUnits(&matchup.Away, "29.100", "", "")

	units := matchup.RatingUnits(models.RatingSourceSP)
	if len(units) != 1 {
		t.Fatalf("RatingUnits() returned %d rows, want 1", len(units))
	}
	if units[0].Label != "Offense" {
		t.Errorf("kept %q, want the one unit both sides have", units[0].Label)
	}
}

// No SP+ row means no rows beneath it, whether the ratings are missing
// altogether or cover only one side.
func TestRatingUnitsNeedTheRowTheySitUnder(t *testing.T) {
	oneSided := matchupWith("14.2", "")
	setSPUnits(&oneSided.Home, "34.700", "18.200", "0.600")

	for name, matchup := range map[string]*Matchup{
		"nil":       nil,
		"unrated":   matchupWith("", ""),
		"one-sided": oneSided,
	} {
		t.Run(name, func(t *testing.T) {
			if units := matchup.RatingUnits(models.RatingSourceSP); units != nil {
				t.Errorf("RatingUnits() = %v, want nil", units)
			}
		})
	}
}

// formResult builds one finished game as the strip sees it, from the team's own
// perspective. The game carries an id because every row links back to it.
func formResult(scored, allowed int, opponent string, atHome bool) RecentResult {
	result := RecentResult{
		Game:     models.Game{ID: uuid.New()},
		For:      scored,
		Against:  allowed,
		AtHome:   atHome,
		Opponent: models.Team{Abbreviation: opponent},
	}
	if scored != allowed {
		result.Won = new(scored > allowed)
	}
	return result
}

func TestOpponentTextMarksRoadGames(t *testing.T) {
	if got := formResult(31, 17, "WVU", true).OpponentText(); got != "vs WVU" {
		t.Errorf("home game = %q, want %q", got, "vs WVU")
	}
	if got := formResult(14, 28, "OSU", false).OpponentText(); got != "@ OSU" {
		t.Errorf("road game = %q, want %q", got, "@ OSU")
	}
}

// The two sides are paired by how many games back each result is, not by date,
// and the shorter side runs out rather than truncating the breakout.
func TestFormRowsPairSidesByCountAndRunTheShortSideOut(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	matchup.Home.Form = []RecentResult{
		formResult(31, 17, "WVU", true),
		formResult(14, 28, "OSU", false),
		formResult(24, 24, "PSU", true),
	}
	matchup.Away.Form = []RecentResult{formResult(38, 10, "UNH", true)}

	rows := matchup.FormRows()
	if len(rows) != 3 {
		t.Fatalf("FormRows() returned %d rows, want 3", len(rows))
	}

	if rows[0].Home == nil || rows[0].Home.OpponentText() != "vs WVU" {
		t.Error("the home side's most recent game is not on the first row")
	}
	if rows[0].Away == nil || rows[0].Away.Score() != "38-10" {
		t.Error("the away side's most recent game is not on the first row")
	}
	// A team one game into the season opposite one three games in: the rows the
	// short side cannot fill are nil, not zero results.
	for _, i := range []int{1, 2} {
		if rows[i].Away != nil {
			t.Errorf("row %d invented a result for the shorter side", i)
		}
		if rows[i].Home == nil {
			t.Errorf("row %d dropped the longer side", i)
		}
	}
}

// Week 1 has nothing to break out, and neither does a game with no panel.
func TestFormRowsAreEmptyWithoutForm(t *testing.T) {
	for name, matchup := range map[string]*Matchup{
		"nil":      nil,
		"no games": matchupWith("14.2", "8.4"),
	} {
		t.Run(name, func(t *testing.T) {
			if rows := matchup.FormRows(); len(rows) != 0 {
				t.Errorf("FormRows() returned %d rows, want none", len(rows))
			}
		})
	}
}

// Each source scores its units on its own scale, and two of the three run a
// defence the opposite way to an offence. Every rating row drawn has to say
// which, or a reader comparing an SP+ defence of 12.1 to an FPI one of 90.1
// concludes the wrong thing twice.
func TestRatingSummaryLabelsEachSourcesScale(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	setSPUnits(&matchup.Home, "34.7", "18.2", "")
	setSPUnits(&matchup.Away, "29.1", "21.5", "")
	for _, source := range []models.RatingSource{models.RatingSourceFPI, models.RatingSourceCORE} {
		setUnits(&matchup.Home, source, "88.1", "90.1", "")
		setUnits(&matchup.Away, source, "71.4", "63.2", "")
	}

	seen := map[string]models.RatingSource{}
	for _, source := range []models.RatingSource{models.RatingSourceSP, models.RatingSourceFPI, models.RatingSourceCORE} {
		summary := matchup.RatingSummary(source)
		if summary == "" {
			t.Errorf("%s drew units with no line saying how to read them", source)
			continue
		}
		// Two sources may not share a caption: their scales are different, and
		// SP+'s and CORE's defences do not even share a sign.
		if other, ok := seen[summary]; ok {
			t.Errorf("%s and %s share a summary, %q", source, other, summary)
		}
		seen[summary] = source

		// The units themselves stay bare -- the row above speaks for them.
		for _, unit := range matchup.RatingUnits(source) {
			if unit.Label == "" {
				t.Errorf("%s produced a unit with no label", source)
			}
		}
	}
}

// A caption describing rows that are not on the page is worse than none.
func TestRatingSummaryIsEmptyWithoutUnits(t *testing.T) {
	// Rated on both sides, but no unit ratings behind either.
	if got := matchupWith("14.2", "8.4").RatingSummary(models.RatingSourceSP); got != "" {
		t.Errorf("RatingSummary() = %q with no units drawn, want empty", got)
	}

	var absent *Matchup
	if got := absent.RatingSummary(models.RatingSourceSP); got != "" {
		t.Errorf("RatingSummary() = %q on an absent matchup, want empty", got)
	}
}

// CORE publishes no special teams rating, and this page says nothing about one
// even if the column starts arriving populated: the summary is the claim about
// what the numbers mean, and it makes none about special teams.
func TestRatingUnitsDrawNoUnitTheyCannotLabel(t *testing.T) {
	matchup := matchupWith("14.2", "8.4")
	setUnits(&matchup.Home, models.RatingSourceCORE, "8.4", "-10.8", "1.2")
	setUnits(&matchup.Away, models.RatingSourceCORE, "5.1", "-3.2", "0.9")

	units := matchup.RatingUnits(models.RatingSourceCORE)
	if len(units) != 2 {
		t.Fatalf("RatingUnits(core) returned %d rows, want 2", len(units))
	}
	for _, unit := range units {
		if unit.Label == "Special teams" {
			t.Error("a CORE special teams row was drawn with nothing true to say about it")
		}
	}
}
