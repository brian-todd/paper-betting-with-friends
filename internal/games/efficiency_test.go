package games

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/shopspring/decimal"
)

// advancedFor builds a side's efficiency with enough plays behind it to be
// drawn, so a test that is about formatting does not have to restate the
// sample-size gate.
func advancedFor(ppa, successRate string) *models.TeamAdvancedStats {
	plays := 143
	offensePPA := decimal.RequireFromString(ppa)
	rate := decimal.RequireFromString(successRate)
	return &models.TeamAdvancedStats{
		OffensePlays:       &plays,
		DefensePlays:       &plays,
		OffensePPA:         &offensePPA,
		OffenseSuccessRate: &rate,
	}
}

// Both sides or neither, the same rule the rating rows follow: half an
// efficiency panel invites a conclusion about the team that is merely
// uncovered. /stats/season/advanced is FBS-only.
func TestHasEfficiencyNeedsBothSides(t *testing.T) {
	tests := []struct {
		name       string
		home, away *models.TeamAdvancedStats
		want       bool
	}{
		{name: "both sides", home: advancedFor("0.241", "0.503"), away: advancedFor("0.180", "0.450"), want: true},
		{name: "home only", home: advancedFor("0.241", "0.503"), want: false},
		{name: "away only", away: advancedFor("0.180", "0.450"), want: false},
		{name: "neither", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := matchupWith("", "")
			m.Home.Advanced, m.Away.Advanced = tt.home, tt.away

			if got := m.HasEfficiency(); got != tt.want {
				t.Errorf("HasEfficiency() = %v, want %v", got, tt.want)
			}
			if tt.want {
				return
			}
			// The metrics follow the gate rather than repeating it, so a
			// caller that forgets to ask cannot draw half a panel anyway.
			if m.EfficiencyMetrics() != nil {
				t.Error("metrics were produced for a panel that should not be drawn")
			}
		})
	}
}

// The gate rejects a row with no football behind it, and nothing more. The
// asymmetry matters: a defence that spent the game on the sideline has far
// fewer snaps than the offence it shares a row with, so a gate tuned to
// offensive volume silently drops matchups for winning time of possession.
func TestHasEfficiencyRejectsOnlyAFragment(t *testing.T) {
	tests := []struct {
		name  string
		plays int
		want  bool
	}{
		{name: "a fragment of one game", plays: 15, want: false},
		{
			// A real one-game defensive sample: Arkansas faced 45 snaps in its
			// 2026 opener while its own offence ran 84.
			name:  "a defence that saw 45 snaps still counts",
			plays: 45,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thin := advancedFor("0.241", "0.503")
			n := tt.plays
			thin.DefensePlays = &n

			m := matchupWith("", "")
			m.Home.Advanced, m.Away.Advanced = thin, advancedFor("0.180", "0.450")

			if got := m.HasEfficiency(); got != tt.want {
				t.Errorf("HasEfficiency() = %v, want %v", got, tt.want)
			}
		})
	}
}

// metricNamed finds one metric by label, so a test that is about formatting
// does not break every time the panel gains a row above the one it asserts on.
func metricNamed(t *testing.T, m *Matchup, label string) EfficiencyMetric {
	t.Helper()
	for _, metric := range m.EfficiencyMetrics() {
		if metric.Label == label {
			return metric
		}
	}
	t.Fatalf("no %q metric in the panel", label)
	return EfficiencyMetric{}
}

// Each metric carries both units, offense first, so the pair a reader wants to
// compare sits together.
func TestEfficiencyMetricsNestBothUnits(t *testing.T) {
	m := matchupWith("", "")
	m.Home.Advanced = advancedFor("0.241", "0.503")
	m.Away.Advanced = advancedFor("0.180", "0.450")

	metrics := m.EfficiencyMetrics()
	if len(metrics) == 0 {
		t.Fatal("no metrics")
	}

	for _, metric := range metrics {
		if len(metric.Units) != 2 {
			t.Errorf("%q has %d units, want 2", metric.Label, len(metric.Units))
			continue
		}
		if metric.Units[0].Label != "Offense" || metric.Units[1].Label != "Defense" {
			t.Errorf("%q nests %q then %q, want Offense then Defense",
				metric.Label, metric.Units[0].Label, metric.Units[1].Label)
		}
		// A metric label alone says nothing about which way the number runs,
		// and half of these invert between the two rows under it.
		if metric.Help == "" {
			t.Errorf("%q carries no hover text saying which way each unit wants it", metric.Label)
		}
	}
}

// Havoc is the one metric a defence wants more of, so its hover text must not be
// the one every other metric shares.
func TestHavocNoteRunsTheOtherWay(t *testing.T) {
	m := matchupWith("", "")
	m.Home.Advanced = advancedFor("0.241", "0.503")
	m.Away.Advanced = advancedFor("0.180", "0.450")

	havoc := metricNamed(t, m, "Havoc")
	ppa := metricNamed(t, m, "PPA / play")
	if havoc.Help == ppa.Help {
		t.Error("havoc carries the same direction note as a metric that runs the opposite way")
	}
}

func TestEfficiencyMetricsFormatPerUnit(t *testing.T) {
	m := matchupWith("", "")
	m.Home.Advanced = advancedFor("0.241", "0.503")
	m.Away.Advanced = advancedFor("-0.185", "0.287")

	// PPA is small and signed, and the sign is the whole number: a positive
	// offensive PPA and a negative one are opposite claims.
	offense := metricNamed(t, m, "PPA / play").Units[0]
	if got, want := offense.Home, "+0.24"; got != want {
		t.Errorf("home PPA = %q, want %q", got, want)
	}
	if got, want := offense.Away, "-0.19"; got != want {
		t.Errorf("away PPA = %q, want %q", got, want)
	}

	// A rate is stored 0-1 and read as a percentage. Nobody says a team has a
	// success rate of 0.503.
	if got, want := metricNamed(t, m, "Success rate").Units[0].Home, "50.3%"; got != want {
		t.Errorf("home success rate = %q, want %q", got, want)
	}

	// A unit the provider left null renders as nothing rather than as a zero,
	// which would read as a real and very bad number.
	if got := metricNamed(t, m, "PPA / play").Units[1].Home; got != "" {
		t.Errorf("an absent defensive PPA rendered as %q", got)
	}
}

// Every value in a column carries the same number of decimal places, or the
// column stops being one. decimal.String drops trailing zeros, so a
// points-per-opportunity of exactly 6 would otherwise render as "6" directly
// above "4.43".
func TestEfficiencyMetricsKeepTrailingZeros(t *testing.T) {
	whole := decimal.RequireFromString("6")
	half := decimal.RequireFromString("0.5")
	evenRate := decimal.RequireFromString("0.5")

	home := advancedFor("0.241", "0.503")
	home.OffensePointsPerOpportunity = &whole
	home.OffensePPA = &half
	home.OffenseSuccessRate = &evenRate

	m := matchupWith("", "")
	m.Home.Advanced, m.Away.Advanced = home, advancedFor("0.180", "0.450")

	for _, tt := range []struct {
		metric string
		want   string
	}{
		{metric: "PPA / play", want: "+0.50"},
		{metric: "Success rate", want: "50.0%"},
		{metric: "Points / opportunity", want: "6.00"},
	} {
		if got := metricNamed(t, m, tt.metric).Units[0].Home; got != tt.want {
			t.Errorf("%s = %q, want %q", tt.metric, got, tt.want)
		}
	}
}

func TestEfficiencySampleNamesTheVolume(t *testing.T) {
	m := matchupWith("", "")
	m.Home.Advanced = advancedFor("0.241", "0.503")
	m.Away.Advanced = advancedFor("0.180", "0.450")

	want := "Based on 143 offensive plays for SYR and 143 for PIT this season."
	if got := m.EfficiencySample(); got != want {
		t.Errorf("EfficiencySample() = %q, want %q", got, want)
	}

	// Nothing to say when nothing is drawn.
	empty := matchupWith("", "")
	if got := empty.EfficiencySample(); got != "" {
		t.Errorf("EfficiencySample() = %q for an undrawn panel, want empty", got)
	}
}

// Same both-sides rule again, and 0-0 is not a record: a team with no graded
// games against the spread has nothing to compare.
func TestHasATSNeedsGradedGamesOnBothSides(t *testing.T) {
	played := &models.TeamATSRecord{Games: 2, ATSWins: 1, ATSLosses: 1}
	none := &models.TeamATSRecord{}

	tests := []struct {
		name       string
		home, away *models.TeamATSRecord
		want       bool
	}{
		{name: "both graded", home: played, away: played, want: true},
		{name: "one side ungraded", home: played, away: none, want: false},
		{name: "one side missing", home: played, want: false},
		{name: "neither", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := matchupWith("", "")
			m.Home.ATS, m.Away.ATS = tt.home, tt.away
			if got := m.HasATS(); got != tt.want {
				t.Errorf("HasATS() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every one of these is called on a nil matchup by a template that reached the
// page before any sync ran.
func TestEfficiencyMethodsAreNilSafe(t *testing.T) {
	var absent *Matchup

	if absent.HasEfficiency() || absent.HasATS() {
		t.Error("an absent matchup claimed to have something to draw")
	}
	if absent.EfficiencyMetrics() != nil {
		t.Error("an absent matchup produced metrics")
	}
	if absent.EfficiencySample() != "" {
		t.Error("an absent matchup produced a sample note")
	}
}
