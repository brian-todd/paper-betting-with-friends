package cfbdata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestPregameWinProbabilitiesFromRequiresBothHalves(t *testing.T) {
	gameID := uuid.New()
	games := map[int64]uuid.UUID{401856685: gameID}

	tests := []struct {
		name     string
		row      APIPregameWP
		wantRows int
	}{
		{
			name:     "complete row maps",
			row:      APIPregameWP{GameID: 401856685, Spread: new(-17.5), HomeWinProbability: new(0.886)},
			wantRows: 1,
		},
		{
			// Both columns are NOT NULL, and a probability with no line cannot
			// be checked against the market it came from.
			name:     "probability without a spread is dropped",
			row:      APIPregameWP{GameID: 401856685, HomeWinProbability: new(0.886)},
			wantRows: 0,
		},
		{
			name:     "spread without a probability is dropped",
			row:      APIPregameWP{GameID: 401856685, Spread: new(-17.5)},
			wantRows: 0,
		},
		{
			// A game the provider has published a line on that we have never
			// synced. Not an error: /metrics/wp/pregame covers games our
			// classification filter may not.
			name:     "unknown game is dropped",
			row:      APIPregameWP{GameID: 999, Spread: new(-17.5), HomeWinProbability: new(0.886)},
			wantRows: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := pregameWinProbabilitiesFrom([]APIPregameWP{tt.row}, games)
			if len(rows) != tt.wantRows {
				t.Fatalf("got %d rows, want %d", len(rows), tt.wantRows)
			}
			if tt.wantRows == 0 {
				return
			}
			if rows[0].GameID != gameID {
				t.Errorf("GameID = %s, want %s", rows[0].GameID, gameID)
			}
			if want := decimal.RequireFromString("0.886"); !rows[0].HomeWinProbability.Equal(want) {
				t.Errorf("HomeWinProbability = %s, want %s", rows[0].HomeWinProbability, want)
			}
			// Home-relative and negative for a home favourite, exactly as
			// published. Flipping it here would make the stored number
			// disagree with the probability beside it.
			if want := decimal.RequireFromString("-17.5"); !rows[0].Spread.Equal(want) {
				t.Errorf("Spread = %s, want %s", rows[0].Spread, want)
			}
		})
	}
}

func TestGameForecastsFromKeepsIndoorGames(t *testing.T) {
	gameID := uuid.New()
	games := map[int64]uuid.UUID{401858216: gameID}

	// A real dome row. The provider does not blank these out -- it reports the
	// weather outside the roof, wind and all, and every one of the 2025
	// season's 70 indoor games carries a wind speed.
	condition := "Cloudy"
	code := 3
	humidity := 53
	payload := []APIGameWeather{{
		ID:                   401858216,
		GameIndoors:          true,
		Temperature:          new(79.0),
		Humidity:             &humidity,
		WindSpeed:            new(12.7),
		WeatherCondition:     &condition,
		WeatherConditionCode: &code,
	}}

	rows := gameForecastsFrom(payload, games)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 -- an indoor game keeps its row so the page knows to say nothing", len(rows))
	}
	if !rows[0].Indoors {
		t.Error("Indoors = false, want true")
	}
	// The row exists and the weather is stored; Show is what refuses to draw it.
	if rows[0].Show() {
		t.Error("Show() = true for an indoor game, want false")
	}
	if rows[0].WindSpeed == nil {
		t.Error("WindSpeed = nil; the feed supplied one and dropping it would hide why Show is false")
	}
}

func TestGameForecastsFromNormalisesAbsentConditions(t *testing.T) {
	gameID := uuid.New()
	games := map[int64]uuid.UUID{401862772: gameID}

	empty := ""
	zero := 0
	tests := []struct {
		name      string
		condition *string
		code      *int
	}{
		// The common case a week out: 47 of 75 week-3 rows had a null label and
		// a zero code.
		{name: "null condition stays nil", condition: nil, code: &zero},
		{name: "empty condition becomes nil", condition: &empty, code: &zero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := gameForecastsFrom([]APIGameWeather{{
				ID:                   401862772,
				Temperature:          new(86.5),
				WeatherCondition:     tt.condition,
				WeatherConditionCode: tt.code,
			}}, games)
			if len(rows) != 1 {
				t.Fatalf("got %d rows, want 1", len(rows))
			}
			if rows[0].WeatherCondition != nil {
				t.Errorf("WeatherCondition = %q, want nil", *rows[0].WeatherCondition)
			}
			if rows[0].ConditionText() != "" {
				t.Errorf("ConditionText() = %q, want empty", rows[0].ConditionText())
			}
			// A missing label must not cost the panel the numbers, which are
			// what it actually renders from.
			if !rows[0].Show() {
				t.Error("Show() = false; a forecast with a temperature is worth drawing without a label")
			}
			if rows[0].GameID != gameID {
				t.Errorf("GameID = %s, want %s", rows[0].GameID, gameID)
			}
		})
	}
}

// The three-decimal columns exist because the feed publishes three. A tenth of
// an inch of rain rounded to one place is no rain at all.
func TestGameForecastsFromKeepsPrecipitationPrecision(t *testing.T) {
	games := map[int64]uuid.UUID{1: uuid.New()}

	rows := gameForecastsFrom([]APIGameWeather{{
		ID:            1,
		Temperature:   new(44.4),
		Precipitation: new(0.343),
		Snowfall:      new(0.157),
	}}, games)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	if want := decimal.RequireFromString("0.343"); !rows[0].Precipitation.Equal(want) {
		t.Errorf("Precipitation = %s, want %s", rows[0].Precipitation, want)
	}
	// Snow wins over rain when both are reported: it is the one that changes
	// how a game is played.
	if got, want := rows[0].PrecipitationText(), "0.16 in snow"; got != want {
		t.Errorf("PrecipitationText() = %q, want %q", got, want)
	}
}

func TestATSRecordsFromMapsPushesOntoTies(t *testing.T) {
	team := uuid.New()
	resolve := func(int64) (uuid.UUID, bool) { return team, true }

	rows := atsRecordsFrom(2026, []APITeamATS{{
		TeamID:         333,
		Team:           "Alabama",
		Games:          11,
		ATSWins:        6,
		ATSLosses:      4,
		ATSPushes:      1,
		AvgCoverMargin: new(-12.17),
	}}, resolve)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	// A push is a stake returned, which is what the Ties slot means, so the ATS
	// line renders through the same helper a straight-up record does.
	if got, want := rows[0].Record().String(), "6-4-1"; got != want {
		t.Errorf("Record() = %q, want %q", got, want)
	}
	// Two decimal places survive: a full 2025 season runs to -12.17.
	if want := decimal.RequireFromString("-12.17"); !rows[0].AvgCoverMargin.Equal(want) {
		t.Errorf("AvgCoverMargin = %s, want %s", rows[0].AvgCoverMargin, want)
	}
	// The sign is the whole meaning of a cover margin, so it is never implicit.
	if got, want := rows[0].CoverMarginText(), "-12.2"; got != want {
		t.Errorf("CoverMarginText() = %q, want %q", got, want)
	}
}

func TestATSRecordsFromSignsAPositiveMargin(t *testing.T) {
	team := uuid.New()
	rows := atsRecordsFrom(2026, []APITeamATS{{
		TeamID: 333, Games: 2, ATSWins: 2, AvgCoverMargin: new(15.0),
	}}, func(int64) (uuid.UUID, bool) { return team, true })

	if got, want := rows[0].CoverMarginText(), "+15"; got != want {
		t.Errorf("CoverMarginText() = %q, want %q", got, want)
	}
	if got, want := rows[0].Record().String(), "2-0"; got != want {
		t.Errorf("Record() = %q, want %q", got, want)
	}
}

func TestAdvancedStatsFromReadsNestedHavoc(t *testing.T) {
	team := uuid.New()

	plays, drives := 143, 23
	payload := []APITeamAdvancedStats{{Season: 2026, Team: "Alabama"}}
	payload[0].Offense.Plays, payload[0].Offense.Drives = &plays, &drives
	payload[0].Offense.PPA = new(0.2411451715382133)
	payload[0].Offense.SuccessRate = new(0.5034965034965035)
	payload[0].Offense.Havoc.Total = new(0.168)
	payload[0].Defense.PPA = new(-0.18473025416128308)
	payload[0].Defense.Havoc.Total = new(0.204)

	rows := advancedStatsFrom(2026, payload, resolveAll(team))
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	got := rows[0]
	// Havoc is a level deeper than everything beside it, which is the one place
	// this mapping can silently produce nils for a fully populated payload.
	if got.OffenseHavoc == nil || !got.OffenseHavoc.Equal(decimal.RequireFromString("0.168")) {
		t.Errorf("OffenseHavoc = %v, want 0.168", got.OffenseHavoc)
	}
	if got.DefenseHavoc == nil || !got.DefenseHavoc.Equal(decimal.RequireFromString("0.204")) {
		t.Errorf("DefenseHavoc = %v, want 0.204", got.DefenseHavoc)
	}
	if got.OffensePlays == nil || *got.OffensePlays != 143 {
		t.Errorf("OffensePlays = %v, want 143", got.OffensePlays)
	}
	// Nothing the feed did not send is invented. A team with no rushing plays
	// has no line yards, and a zero would read as the worst in the country.
	if got.OffenseLineYards != nil || got.DefenseSuccessRate != nil {
		t.Errorf("absent figures = %v/%v, want nil", got.OffenseLineYards, got.DefenseSuccessRate)
	}
}

// The sample-size gate is what keeps a week-1 panel off the page. A side with
// one game of snaps has rates that describe a quarter of football.
func TestAdvancedStatsReliabilityNeedsAboutAGame(t *testing.T) {
	team := uuid.New()

	tests := []struct {
		name  string
		plays int
		want  bool
	}{
		{name: "a fragment of a game is not enough", plays: 15, want: false},
		{name: "a one-game defensive sample is", plays: 45, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plays := tt.plays
			payload := []APITeamAdvancedStats{{Season: 2026, Team: "Alabama"}}
			payload[0].Offense.Plays, payload[0].Defense.Plays = &plays, &plays

			rows := advancedStatsFrom(2026, payload, resolveAll(team))
			if got := rows[0].OffenseReliable(); got != tt.want {
				t.Errorf("OffenseReliable() = %v, want %v", got, tt.want)
			}
			if got := rows[0].DefenseReliable(); got != tt.want {
				t.Errorf("DefenseReliable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A forecast for a game that has already kicked off is of no use to anybody,
// and the feed returns every week the season has reached -- 3,235 rows for a
// completed 2025. This is the filter that keeps the daily write bounded.
func TestGameWeatherStartTimeParsesForFiltering(t *testing.T) {
	var w APIGameWeather
	if err := decodeJSON(`{"id":1,"startTime":"2026-09-19T23:00:00.000Z"}`, &w); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	want := time.Date(2026, 9, 19, 23, 0, 0, 0, time.UTC)
	if !w.StartTime.Equal(want) {
		t.Errorf("StartTime = %s, want %s", w.StartTime, want)
	}
}

// decodeJSON is a one-line helper so the test above reads as an assertion about
// the wire format rather than about encoding/json.
func decodeJSON(body string, v any) error {
	return json.Unmarshal([]byte(body), v)
}
