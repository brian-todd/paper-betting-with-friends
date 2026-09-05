package models

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestGameLiveStatePeriodLabel(t *testing.T) {
	tests := []struct {
		name   string
		period *int
		want   string
	}{
		{"no period reported", nil, ""},
		{"first quarter", new(1), "Q1"},
		{"fourth quarter", new(4), "Q4"},
		{"overtime", new(5), "OT"},
		{"double overtime", new(6), "2OT"},
		{"quadruple overtime", new(8), "4OT"},
		// The feed is not ours; a zero or negative period is not a period.
		{"zero", new(0), ""},
		{"negative", new(-1), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &GameLiveState{Period: tt.period}
			if got := state.PeriodLabel(); got != tt.want {
				t.Errorf("PeriodLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGameLiveStateLiveLabel(t *testing.T) {
	tests := []struct {
		name  string
		state *GameLiveState
		want  string
	}{
		// A nil state is a game the scoreboard has never covered. Every method
		// has to answer for one, since the templates call them unguarded.
		{"nil state", nil, ""},
		{"period and clock", &GameLiveState{Period: new(3), Clock: new("07:42")}, "Q3 · 07:42"},
		{"period only", &GameLiveState{Period: new(2)}, "Q2"},
		{"clock only", &GameLiveState{Clock: new("00:00")}, "00:00"},
		{"neither", &GameLiveState{}, ""},
		{"blank clock", &GameLiveState{Period: new(1), Clock: new("  ")}, "Q1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.LiveLabel(); got != tt.want {
				t.Errorf("LiveLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGameLiveStatePossession(t *testing.T) {
	tests := []struct {
		name               string
		state              *GameLiveState
		wantHome, wantAway bool
	}{
		{"nil state", nil, false, false},
		{"none reported", &GameLiveState{}, false, false},
		{"home", &GameLiveState{Possession: new("home")}, true, false},
		{"away", &GameLiveState{Possession: new("away")}, false, true},
		{"mixed case and spaces", &GameLiveState{Possession: new(" Home ")}, true, false},
		// Anything the feed words differently resolves to neither side rather
		// than to a guess -- a ball marker on the wrong team is worse than none.
		{"unrecognised", &GameLiveState{Possession: new("Alabama")}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.HomeHasBall(); got != tt.wantHome {
				t.Errorf("HomeHasBall() = %v, want %v", got, tt.wantHome)
			}
			if got := tt.state.AwayHasBall(); got != tt.wantAway {
				t.Errorf("AwayHasBall() = %v, want %v", got, tt.wantAway)
			}
		})
	}
}

func TestGameLiveStateWeatherText(t *testing.T) {
	temp := decimal.RequireFromString("78.6")

	tests := []struct {
		name  string
		state *GameLiveState
		want  string
	}{
		{"nil state", nil, ""},
		{"nothing reported", &GameLiveState{}, ""},
		{"both", &GameLiveState{WeatherDescription: new("Light Rain"), Temperature: &temp}, "Light Rain, 79°F"},
		{"description only", &GameLiveState{WeatherDescription: new("Clear")}, "Clear"},
		{"temperature only", &GameLiveState{Temperature: &temp}, "79°F"},
		// The feed nulls all four weather fields together for a venue it has no
		// station for, but an empty string is what an indoor game has produced.
		{"blank description", &GameLiveState{WeatherDescription: new("")}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.WeatherText(); got != tt.want {
				t.Errorf("WeatherText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGameLiveStateWindText(t *testing.T) {
	speed := decimal.RequireFromString("6.8")

	tests := []struct {
		name  string
		state *GameLiveState
		want  string
	}{
		{"nil state", nil, ""},
		{"no speed", &GameLiveState{WindDirection: new(340)}, ""},
		{"speed only", &GameLiveState{WindSpeed: &speed}, "7 mph"},
		{"speed and bearing", &GameLiveState{WindSpeed: &speed, WindDirection: new(340)}, "7 mph NNW"},
		{"due north", &GameLiveState{WindSpeed: &speed, WindDirection: new(0)}, "7 mph N"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.WindText(); got != tt.want {
				t.Errorf("WindText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompassPoint(t *testing.T) {
	tests := []struct {
		degrees int
		want    string
	}{
		{0, "N"},
		{11, "N"},   // Just inside the first half-span.
		{12, "NNE"}, // Just past it.
		{90, "E"},
		{180, "S"},
		{270, "W"},
		{359, "N"}, // Wraps back to north rather than off the end of the table.
		{360, "N"},
		{450, "E"}, // A bearing past a full turn is still a bearing.
		{-90, "W"}, // As is a negative one.
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := compassPoint(&tt.degrees); got != tt.want {
				t.Errorf("compassPoint(%d) = %q, want %q", tt.degrees, got, tt.want)
			}
		})
	}

	if got := compassPoint(nil); got != "" {
		t.Errorf("compassPoint(nil) = %q, want empty", got)
	}
}

func TestGameLiveStateAwayWinProbability(t *testing.T) {
	if got := (*GameLiveState)(nil).AwayWinProbability(); got != nil {
		t.Errorf("AwayWinProbability() on nil state = %v, want nil", got)
	}

	if got := (&GameLiveState{}).AwayWinProbability(); got != nil {
		t.Errorf("AwayWinProbability() with none reported = %v, want nil", got)
	}

	home := decimal.RequireFromString("0.7250")
	state := &GameLiveState{HomeWinProbability: &home}
	want := decimal.RequireFromString("0.2750")
	got := state.AwayWinProbability()
	if got == nil || !got.Equal(want) {
		t.Errorf("AwayWinProbability() = %v, want %v", got, want)
	}
	// The home value is the stored one and must survive being read.
	if !home.Equal(decimal.RequireFromString("0.7250")) {
		t.Errorf("home probability mutated to %v", home)
	}
}

func TestGameLiveStateHasLiveDetail(t *testing.T) {
	tests := []struct {
		name  string
		state *GameLiveState
		want  bool
	}{
		{"nil state", nil, false},
		{"empty", &GameLiveState{}, false},
		{"period", &GameLiveState{Period: new(1)}, true},
		{"clock", &GameLiveState{Clock: new("12:00")}, true},
		{"situation", &GameLiveState{Situation: new("3rd & 7")}, true},
		{"last play", &GameLiveState{LastPlay: new("End of 4th quarter.")}, true},
		// Broadcast and weather are known before kickoff for every game, so
		// neither counts as something happening.
		{"broadcast only", &GameLiveState{TV: new("ESPN")}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.HasLiveDetail(); got != tt.want {
				t.Errorf("HasLiveDetail() = %v, want %v", got, tt.want)
			}
		})
	}
}
