package cfbdata

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestGetScoreboardRequestsClassification(t *testing.T) {
	var gotPath, gotQuery string

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("classification")
		w.Write([]byte(`[
			{"id": 401856766, "status": "completed", "tv": "ESPN | Disney+",
			 "homeTeam": {"id": 2628, "name": "TCU", "points": 10, "lineScores": [10,0,0,0]},
			 "awayTeam": {"id": 153,  "name": "North Carolina", "points": 15},
			 "weather": {"temperature": 63.7, "description": "Overcast", "windSpeed": 6.8, "windDirection": 340}}
		]`))
	})

	games, err := c.GetScoreboard(context.Background(), "fcs")
	if err != nil {
		t.Fatalf("GetScoreboard() error = %v", err)
	}

	if gotPath != "/scoreboard" {
		t.Errorf("path = %q, want /scoreboard", gotPath)
	}
	// The endpoint defaults to FBS and returns nothing else, so a division that
	// does not reach it is a sync that succeeds and stores no scores.
	if gotQuery != "fcs" {
		t.Errorf("classification = %q, want fcs", gotQuery)
	}

	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	g := games[0]
	if g.ID != 401856766 || g.Status != ScoreboardStatusCompleted {
		t.Errorf("game = %d/%s, want 401856766/completed", g.ID, g.Status)
	}
	if g.HomeTeam.Points == nil || *g.HomeTeam.Points != 10 {
		t.Errorf("home points = %v, want 10", g.HomeTeam.Points)
	}
	if g.TV != "ESPN | Disney+" {
		t.Errorf("tv = %q, want %q", g.TV, "ESPN | Disney+")
	}
	if g.Weather == nil || g.Weather.WindDirection == nil || *g.Weather.WindDirection != 340 {
		t.Errorf("weather = %+v, want wind direction 340", g.Weather)
	}
	// Nulls have to survive as nulls: a game that has not kicked off reports no
	// clock, and reading that as a zeroed one would put every card on "Q0".
	if g.Period != nil || g.Clock != nil || g.Possession != nil {
		t.Errorf("period/clock/possession = %v/%v/%v, want all nil", g.Period, g.Clock, g.Possession)
	}
}

func TestScoreboardStatus(t *testing.T) {
	tests := []struct {
		status        string
		wantStatus    models.GameStatus
		wantCompleted bool
		wantOK        bool
	}{
		{ScoreboardStatusScheduled, models.GameStatusScheduled, false, true},
		{ScoreboardStatusInProgress, models.GameStatusInProgress, false, true},
		{ScoreboardStatusCompleted, models.GameStatusFinal, true, true},
		// A status the feed has not used before must not be guessed at: parking
		// a live game on "final" would take it out of the bet slip.
		{"delayed", "", false, false},
		{"", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			status, completed, ok := scoreboardStatus(tt.status)
			if status != tt.wantStatus || completed != tt.wantCompleted || ok != tt.wantOK {
				t.Errorf("scoreboardStatus(%q) = %v/%v/%v, want %v/%v/%v",
					tt.status, status, completed, ok, tt.wantStatus, tt.wantCompleted, tt.wantOK)
			}
		})
	}
}

func TestScoreboardResult(t *testing.T) {
	gameID := uuid.New()
	now := time.Date(2026, time.September, 5, 20, 0, 0, 0, time.UTC)

	points := func(v int) *int { return &v }

	tests := []struct {
		name          string
		game          APIScoreboardGame
		wantOK        bool
		wantHome      int
		wantAway      int
		wantFinalized bool
	}{
		{
			name: "scheduled game writes nothing",
			game: APIScoreboardGame{Status: ScoreboardStatusScheduled},
		},
		{
			// The feed has been observed reporting a side with points and the
			// other still null minutes into a game. That is a shutout so far,
			// not a missing score, and the row it writes can never settle a bet.
			name:     "live game reads a missing side as zero",
			game:     APIScoreboardGame{Status: ScoreboardStatusInProgress, HomeTeam: APIScoreboardTeam{Points: points(7)}},
			wantOK:   true,
			wantHome: 7,
			wantAway: 0,
		},
		{
			name:   "live game with no points at all writes nothing",
			game:   APIScoreboardGame{Status: ScoreboardStatusInProgress},
			wantOK: false,
		},
		{
			// A completed game is held to the stricter rule, because that row is
			// what settlement later reads: a half-arrived final is worse than none.
			name:   "completed game with one side missing writes nothing",
			game:   APIScoreboardGame{Status: ScoreboardStatusCompleted, HomeTeam: APIScoreboardTeam{Points: points(21)}},
			wantOK: false,
		},
		{
			name: "completed game finalizes",
			game: APIScoreboardGame{
				Status:   ScoreboardStatusCompleted,
				HomeTeam: APIScoreboardTeam{Points: points(10), LineScores: []int{10, 0, 0, 0}},
				AwayTeam: APIScoreboardTeam{Points: points(15), LineScores: []int{10, 2, 3, 0}},
			},
			wantOK:        true,
			wantHome:      10,
			wantAway:      15,
			wantFinalized: true,
		},
		{
			// A scheduled game can carry stale points on a rescheduled fixture;
			// nothing has been played, so nothing is a score.
			name:   "unrecognised status writes nothing even with points",
			game:   APIScoreboardGame{Status: "delayed", HomeTeam: APIScoreboardTeam{Points: points(3)}, AwayTeam: APIScoreboardTeam{Points: points(0)}},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := scoreboardResult(gameID, tt.game, now)
			if ok != tt.wantOK {
				t.Fatalf("scoreboardResult() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if result.GameID != gameID {
				t.Errorf("GameID = %v, want %v", result.GameID, gameID)
			}
			if result.HomeScore != tt.wantHome || result.AwayScore != tt.wantAway {
				t.Errorf("score = %d-%d, want %d-%d", result.HomeScore, result.AwayScore, tt.wantHome, tt.wantAway)
			}
			if gotFinalized := result.IsFinal(); gotFinalized != tt.wantFinalized {
				t.Errorf("IsFinal() = %v, want %v", gotFinalized, tt.wantFinalized)
			}
			if tt.wantFinalized && !result.FinalizedAt.Equal(now) {
				t.Errorf("FinalizedAt = %v, want %v", result.FinalizedAt, now)
			}
		})
	}
}

func TestScoreboardLiveState(t *testing.T) {
	gameID := uuid.New()
	temperature, windSpeed, winProbability := 63.7, 6.8, 0.62
	description := "Overcast"
	windDirection := 340

	game := APIScoreboardGame{
		ID:         401856766,
		TV:         "ESPN | Disney+",
		Period:     new(3),
		Clock:      new("07:42"),
		Situation:  new("3rd & 7"),
		Possession: new("home"),
		LastPlay:   new("Pass complete for 4 yards."),
		HomeTeam:   APIScoreboardTeam{WinProbability: &winProbability},
		Weather: &APIScoreboardWeather{
			Temperature:   &temperature,
			Description:   &description,
			WindSpeed:     &windSpeed,
			WindDirection: &windDirection,
		},
	}

	state := scoreboardLiveState(gameID, game)

	if state.GameID != gameID {
		t.Errorf("GameID = %v, want %v", state.GameID, gameID)
	}
	if got := state.LiveLabel(); got != "Q3 · 07:42" {
		t.Errorf("LiveLabel() = %q, want %q", got, "Q3 · 07:42")
	}
	if !state.HomeHasBall() || state.AwayHasBall() {
		t.Errorf("possession = home %v / away %v, want true/false", state.HomeHasBall(), state.AwayHasBall())
	}
	if got := state.Broadcast(); got != "ESPN | Disney+" {
		t.Errorf("Broadcast() = %q, want %q", got, "ESPN | Disney+")
	}
	if got := state.WeatherText(); got != "Overcast, 64°F" {
		t.Errorf("WeatherText() = %q, want %q", got, "Overcast, 64°F")
	}
	if got := state.WindText(); got != "7 mph NNW" {
		t.Errorf("WindText() = %q, want %q", got, "7 mph NNW")
	}
	if state.HomeWinProbability == nil || !state.HomeWinProbability.Equal(decimal.RequireFromString("0.62")) {
		t.Errorf("HomeWinProbability = %v, want 0.62", state.HomeWinProbability)
	}
}

func TestScoreboardLiveStateWithoutWeather(t *testing.T) {
	// Every weather field is null together for a venue the provider has no
	// station for, and the whole object is absent on some rows. Neither may
	// panic, and neither may invent a reading.
	state := scoreboardLiveState(uuid.New(), APIScoreboardGame{TV: ""})

	if state.Temperature != nil || state.WindSpeed != nil || state.WindDirection != nil || state.WeatherDescription != nil {
		t.Errorf("weather = %+v, want all nil", state)
	}
	if state.TV != nil {
		t.Errorf("TV = %v, want nil for an empty listing", state.TV)
	}
	if got := state.WeatherText(); got != "" {
		t.Errorf("WeatherText() = %q, want empty", got)
	}
}

func TestNormalizePossession(t *testing.T) {
	tests := []struct {
		name  string
		input *string
		want  *string
	}{
		{"absent", nil, nil},
		{"home", new("home"), new("home")},
		{"away", new("away"), new("away")},
		{"mixed case and spaces", new(" Home "), new("home")},
		// The column is bounded and the feed is not. A team name here -- or
		// anything else the provider might switch to -- is dropped rather than
		// stored, because one longer than the column fails the whole row's
		// upsert and takes the clock and the situation down with it.
		{"a team name", new("North Carolina A&T Aggies"), nil},
		{"empty", new(""), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePossession(tt.input)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("normalizePossession() = %q, want nil", *got)
			case tt.want != nil && got == nil:
				t.Errorf("normalizePossession() = nil, want %q", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("normalizePossession() = %q, want %q", *got, *tt.want)
			}
		})
	}
}

// The stored value is what HomeHasBall and AwayHasBall read, so normalising on
// the way in must not change which side the page points at.
func TestScoreboardLiveStateResolvesPossession(t *testing.T) {
	for _, tt := range []struct {
		possession         string
		wantHome, wantAway bool
	}{
		{"home", true, false},
		{"away", false, true},
		{"Alabama", false, false},
	} {
		t.Run(tt.possession, func(t *testing.T) {
			state := scoreboardLiveState(uuid.New(), APIScoreboardGame{Possession: new(tt.possession)})
			if state.HomeHasBall() != tt.wantHome || state.AwayHasBall() != tt.wantAway {
				t.Errorf("possession %q resolved to home %v / away %v, want %v / %v",
					tt.possession, state.HomeHasBall(), state.AwayHasBall(), tt.wantHome, tt.wantAway)
			}
		})
	}
}
