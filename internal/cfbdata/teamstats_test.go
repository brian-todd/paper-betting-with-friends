package cfbdata

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// resolveAll maps every name onto one id, for tests that are about the mapping
// rather than about team lookup.
func resolveAll(id uuid.UUID) func(string) (uuid.UUID, bool) {
	return func(string) (uuid.UUID, bool) { return id, true }
}

func TestSPRatingsFromKeepsOnlyPopulatedFields(t *testing.T) {
	team := uuid.New()

	// The shape the endpoint actually returns: a rating and a ranking, unit
	// ratings and rankings, and null for every sub-detail. Not an edge case --
	// this is every row, in a live season and a completed one alike.
	rank, offRank, defRank := 1, 3, 1
	offRating, defRating, stRating := 40.0, 10.1, 0.1
	payload := []APITeamSP{{
		Year:    2026,
		Team:    "Ohio State",
		Rating:  30,
		Ranking: &rank,
	}}
	payload[0].Offense.Rating, payload[0].Offense.Ranking = &offRating, &offRank
	payload[0].Defense.Rating, payload[0].Defense.Ranking = &defRating, &defRank
	payload[0].SpecialTeams.Rating = &stRating

	rows := spRatingsFrom(2026, payload, resolveAll(team))
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	got := rows[0]
	if got.Source != models.RatingSourceSP {
		t.Errorf("Source = %q, want %q", got.Source, models.RatingSourceSP)
	}
	if want := decimal.RequireFromString("30"); !got.Overall.Equal(want) {
		t.Errorf("Overall = %s, want %s", got.Overall, want)
	}
	if got.OverallRank == nil || *got.OverallRank != 1 {
		t.Errorf("OverallRank = %v, want 1", got.OverallRank)
	}
	if got.Offense == nil || !got.Offense.Equal(decimal.RequireFromString("40")) {
		t.Errorf("Offense = %v, want 40", got.Offense)
	}
	// SP+ supplies no resume ranks and says nothing about which week it has
	// seen. Those columns belong to other sources and must stay nil here rather
	// than being filled in with a guess.
	if got.StrengthOfSchedule != nil || got.StrengthOfRecord != nil {
		t.Errorf("resume ranks = %v/%v, want nil", got.StrengthOfSchedule, got.StrengthOfRecord)
	}
	if got.ThroughWeek != nil || got.ModelVersion != nil {
		t.Errorf("through week/model = %v/%v, want nil", got.ThroughWeek, got.ModelVersion)
	}
}

// The /ratings/sp payload ends with a synthetic league-averages row. It is not
// a team, so it must not reach the resolver -- otherwise it lands in the
// unmatched-teams warning on every run and trains the reader to ignore it.
func TestSPRatingsFromSkipsNationalAveragesBeforeResolving(t *testing.T) {
	var asked []string
	resolve := func(name string) (uuid.UUID, bool) {
		asked = append(asked, name)
		return uuid.New(), true
	}

	rows := spRatingsFrom(2026, []APITeamSP{
		{Team: "Ohio State", Rating: 30},
		{Team: APINationalAveragesTeam, Rating: -0.13405797101449277},
	}, resolve)

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	for _, name := range asked {
		if name == APINationalAveragesTeam {
			t.Fatalf("resolver was asked to place %q", APINationalAveragesTeam)
		}
	}
}

func TestSPRatingsFromDropsUnresolvedTeams(t *testing.T) {
	resolve := func(name string) (uuid.UUID, bool) {
		if name == "Ohio State" {
			return uuid.New(), true
		}
		return uuid.Nil, false
	}

	rows := spRatingsFrom(2026, []APITeamSP{
		{Team: "Ohio State", Rating: 30},
		{Team: "Renamed School", Rating: 12},
	}, resolve)

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
}

// FPI publishes three decimal places where SP+ rounds to one. The column is
// decimal(6,3) for exactly this reason; a float or a decimal(6,2) loses it.
func TestFPIRatingsFromPreservesThreeDecimalPlaces(t *testing.T) {
	fpi := -14.484
	sos, sor, rank := 104, 94, 122
	payload := []APITeamFPI{{Team: "UAB", FPI: &fpi}}
	payload[0].ResumeRanks.StrengthOfSchedule = &sos
	payload[0].ResumeRanks.StrengthOfRecord = &sor
	payload[0].ResumeRanks.FPI = &rank

	rows := fpiRatingsFrom(2026, payload, resolveAll(uuid.New()))
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	got := rows[0]
	if want := decimal.RequireFromString("-14.484"); !got.Overall.Equal(want) {
		t.Errorf("Overall = %s, want %s", got.Overall, want)
	}
	if got.StrengthOfSchedule == nil || *got.StrengthOfSchedule != sos {
		t.Errorf("StrengthOfSchedule = %v, want %d", got.StrengthOfSchedule, sos)
	}
	if got.StrengthOfRecord == nil || *got.StrengthOfRecord != sor {
		t.Errorf("StrengthOfRecord = %v, want %d", got.StrengthOfRecord, sor)
	}
	if got.OverallRank == nil || *got.OverallRank != rank {
		t.Errorf("OverallRank = %v, want %d", got.OverallRank, rank)
	}
}

// Overall is NOT NULL. A team the model has no headline number for has nothing
// to store, and the efficiencies are percentiles that cannot stand in for it.
func TestFPIRatingsFromSkipsRowsWithoutARating(t *testing.T) {
	efficiency := 51.8
	payload := []APITeamFPI{{Team: "Somebody"}}
	payload[0].Efficiencies.Offense = &efficiency

	if rows := fpiRatingsFrom(2026, payload, resolveAll(uuid.New())); len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestCoreRatingsFromCarriesThroughWeekAndModelVersion(t *testing.T) {
	overall, offense, defense := 19.2, 8.41, -10.79
	week := 1
	rows := coreRatingsFrom(2026, []APITeamCore{{
		Team:         "Ohio State",
		Overall:      &overall,
		Offense:      &offense,
		Defense:      &defense,
		ThroughWeek:  &week,
		ModelVersion: "core-preseason-v1",
	}}, resolveAll(uuid.New()))

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	got := rows[0]
	if got.ThroughWeek == nil || *got.ThroughWeek != week {
		t.Errorf("ThroughWeek = %v, want %d", got.ThroughWeek, week)
	}
	if got.ModelVersion == nil || *got.ModelVersion != "core-preseason-v1" {
		t.Errorf("ModelVersion = %v, want core-preseason-v1", got.ModelVersion)
	}
	if want := decimal.RequireFromString("-10.79"); got.Defense == nil || !got.Defense.Equal(want) {
		t.Errorf("Defense = %v, want %s", got.Defense, want)
	}
}

func TestTeamRecordsFromMapsTheThreeStoredSplits(t *testing.T) {
	expected := 0.5327526330947876
	rows := teamRecordsFrom(2026, []APITeamRecords{{
		TeamID:       2,
		Team:         "Auburn",
		ExpectedWins: &expected,
		Total:        APIRecordSplit{Games: 3, Wins: 2, Losses: 1},
		HomeGames:    APIRecordSplit{Games: 2, Wins: 2},
		AwayGames:    APIRecordSplit{Games: 1, Losses: 1},
	}}, func(int64) (uuid.UUID, bool) { return uuid.New(), true })

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	got := rows[0]
	if got.Overall().String() != "2-1" {
		t.Errorf("Overall = %q, want %q", got.Overall(), "2-1")
	}
	if got.Home().String() != "2-0" {
		t.Errorf("Home = %q, want %q", got.Home(), "2-0")
	}
	if got.Away().String() != "0-1" {
		t.Errorf("Away = %q, want %q", got.Away(), "0-1")
	}
	if got.ExpectedWins == nil {
		t.Fatal("ExpectedWins was dropped")
	}
}

// A team with no games is not 0-0 on the page: that reads as a result. It is a
// team that has not played, and the strip renders nothing.
func TestWinLossPlayedDistinguishesNoGamesFromNoWins(t *testing.T) {
	unplayed := models.WinLoss{}
	if unplayed.Played() {
		t.Error("a 0-game split reported as played")
	}

	winless := models.WinLoss{Games: 2, Losses: 2}
	if !winless.Played() {
		t.Error("a 0-2 split reported as unplayed")
	}
	if winless.String() != "0-2" {
		t.Errorf("String = %q, want %q", winless, "0-2")
	}

	tied := models.WinLoss{Games: 3, Wins: 1, Losses: 1, Ties: 1}
	if tied.String() != "1-1-1" {
		t.Errorf("String = %q, want %q", tied, "1-1-1")
	}
}

// Rendering a rating that has been through a decimal(6,3) column must not show
// the trailing zeroes the round trip adds.
func TestRatingOverallTextRoundsForDisplay(t *testing.T) {
	tests := []struct {
		stored string
		want   string
	}{
		{"14.200", "14.2"},
		{"-14.484", "-14.5"},
		{"30.000", "30"},
	}

	for _, tt := range tests {
		t.Run(tt.stored, func(t *testing.T) {
			rating := models.TeamRating{Overall: decimal.RequireFromString(tt.stored)}
			if got := rating.OverallText(); got != tt.want {
				t.Errorf("OverallText() = %q, want %q", got, tt.want)
			}
		})
	}
}
