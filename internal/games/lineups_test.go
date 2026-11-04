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

func TestLineupUnitRates(t *testing.T) {
	totals := models.LineupTotals{
		PointsFor: 30, PossessionsFor: 25, PointsAgainst: 20, PossessionsAgainst: 25,
		For:     models.LineupBox{FGMade: 10, FGAttempted: 20, ThreeMade: 4, ThreeAttempted: 8, Turnovers: 5},
		Against: models.LineupBox{FGMade: 8, FGAttempted: 25, ThreeMade: 2, ThreeAttempted: 10, Turnovers: 3},
	}

	got := lineupUnit(totals)
	want := LineupUnit{
		Possessions: 25, Net: "+40.0", Offense: "120.0", Defense: "80.0",
		EffectiveFG: "60.0", OppEffectiveFG: "36.0", ThreeRate: "40.0", TurnoverRate: "20.0",
	}
	if got != want {
		t.Errorf("lineupUnit = %+v, want %+v", got, want)
	}

	t.Run("no attempts leaves the shooting rates empty", func(t *testing.T) {
		noShots := totals
		noShots.For, noShots.Against = models.LineupBox{}, models.LineupBox{}
		got := lineupUnit(noShots)
		if got.EffectiveFG != "" || got.OppEffectiveFG != "" || got.ThreeRate != "" {
			t.Errorf("rates from no attempts: %+v", got)
		}
		if got.TurnoverRate != "0.0" {
			t.Errorf("turnover rate = %q, want 0.0: it is per possession", got.TurnoverRate)
		}
	})

	t.Run("too small a sample shows no rates at all", func(t *testing.T) {
		small := totals
		small.PossessionsAgainst = 9
		got := lineupUnit(small)
		if got.Net != "" || got.Offense != "" || got.Defense != "" || got.EffectiveFG != "" ||
			got.OppEffectiveFG != "" || got.ThreeRate != "" || got.TurnoverRate != "" {
			t.Errorf("rates from a small sample: %+v", got)
		}
	})
}
