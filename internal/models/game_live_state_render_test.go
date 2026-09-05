package models

import (
	"html/template"
	"strings"
	"testing"
)

// The grid and detail pages call these helpers straight off Game.LiveState,
// which is nil for every game the scoreboard has not covered -- basketball, any
// division outside CFB_SCOREBOARD_CLASSIFICATIONS, any week but the current
// one. html/template resolves a method on a typed nil pointer rather than
// erroring, but only because every one of these takes a nil receiver; this
// pins that down, since the failure mode is a page that renders as an empty
// swap target with the reason only in the log.
func TestLiveStateHelpersRenderWithNilState(t *testing.T) {
	tmpl := template.Must(template.New("card").Parse(
		`[{{if eq .Game.Status "in_progress"}}{{with .Game.LiveState.LiveLabel}}{{.}}{{else}}Live{{end}}{{end}}]` +
			`[{{with .Game.LiveState.Broadcast}}TV {{.}}{{end}}]` +
			`[{{with .Game.LiveState.SituationText}}{{.}}{{end}}]` +
			`[{{with .Game.LiveState.WeatherText}}{{.}}{{end}}]` +
			`[{{with .Game.LiveState.WindText}}{{.}}{{end}}]` +
			`[{{if and (eq .Game.Status "in_progress") .Game.LiveState.HomeHasBall}}BALL{{end}}]`))

	clock := "07:42"
	period := 3

	tests := []struct {
		name string
		game Game
		want string
	}{
		{
			name: "no scoreboard row at all",
			game: Game{Status: GameStatusInProgress},
			want: "[Live][][][][][]",
		},
		{
			name: "row exists but nothing has been reported yet",
			game: Game{Status: GameStatusInProgress, LiveState: &GameLiveState{}},
			want: "[Live][][][][][]",
		},
		{
			name: "live game with a clock and possession",
			game: Game{Status: GameStatusInProgress, LiveState: &GameLiveState{
				Period: &period, Clock: &clock, Possession: new("home"), TV: new("ESPN"),
			}},
			want: "[Q3 · 07:42][TV ESPN][][][][BALL]",
		},
		{
			// The clock survives the whistle, so a finished game must not be
			// drawn as though it were still running.
			name: "finished game keeps the last clock but is not live",
			game: Game{Status: GameStatusFinal, LiveState: &GameLiveState{Period: &period, Clock: &clock}},
			want: "[][][][][][]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			if err := tmpl.Execute(&out, map[string]any{"Game": tt.game}); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Errorf("rendered %q, want %q", got, tt.want)
			}
		})
	}
}
