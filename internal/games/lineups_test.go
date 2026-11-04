package games

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

func TestLineupUnit(t *testing.T) {
	tests := []struct {
		name        string
		totals      models.LineupTotals
		wantMinutes int
		wantNet     string
	}{
		{"a real sample shows its rating",
			models.LineupTotals{Seconds: 815, PointsFor: 32, PossessionsFor: 18, PointsAgainst: 36, PossessionsAgainst: 19}, 14, "-11.7"},
		{"a positive rating carries its sign",
			models.LineupTotals{Seconds: 600, PointsFor: 30, PossessionsFor: 20, PointsAgainst: 20, PossessionsAgainst: 20}, 10, "+50.0"},
		{"too few possessions shows none",
			models.LineupTotals{Seconds: 90, PointsFor: 6, PossessionsFor: 3, PointsAgainst: 0, PossessionsAgainst: 3}, 2, ""},
		{"too few on one side shows none",
			models.LineupTotals{Seconds: 200, PointsFor: 20, PossessionsFor: 10, PointsAgainst: 0, PossessionsAgainst: 9}, 3, ""},
		{"no possessions at all does not divide by zero",
			models.LineupTotals{Seconds: 10}, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lineupUnit(tt.totals)
			if got.Minutes != tt.wantMinutes {
				t.Errorf("Minutes = %d, want %d", got.Minutes, tt.wantMinutes)
			}
			if got.Net != tt.wantNet {
				t.Errorf("Net = %q, want %q", got.Net, tt.wantNet)
			}
		})
	}
}
