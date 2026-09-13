package games

import (
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// Every row the panel draws has hover text, and the underline that advertises
// it is drawn from the same call -- so a label with no entry here is a label
// wearing an affordance that does nothing when the reader takes it up.
func TestEveryDrawnRowIsExplained(t *testing.T) {
	labels := []string{
		"Record",
		"Away / Home",
		"Against the spread",
		models.RatingSourceSP.Label(),
		models.RatingSourceFPI.Label(),
		models.RatingSourceCORE.Label(),
		"Elo",
		"Win probability",
		"Recent",
	}

	drawn := make(map[string]bool, len(labels))
	m := matchupWith("14.2", "8.4")
	for _, label := range labels {
		drawn[label] = true
		if m.Explain(label) == "" {
			t.Errorf("the %q row has no hover text", label)
		}
	}

	// And nothing else does: an entry keyed to a label no row carries is a
	// tooltip nobody will ever see.
	for label := range metricHelp {
		if !drawn[label] {
			t.Errorf("metricHelp explains %q, which no row is drawn with", label)
		}
	}
}

// The rating rows say how to read the units under them, and they say it by
// composing RatingSummary rather than by restating it -- so the two cannot
// drift. Nothing is said when no units are drawn.
func TestExplainAppendsTheUnitScaleOnlyWhenUnitsAreDrawn(t *testing.T) {
	bare := matchupWith("14.2", "8.4")
	summary := ratingUnitScales[models.RatingSourceSP].summary

	if got := bare.Explain("SP+"); strings.Contains(got, summary) {
		t.Error("the SP+ hover text explained unit rows that are not on the page")
	}

	withUnits := matchupWith("14.2", "8.4")
	for _, side := range []*TeamStats{&withUnits.Home, &withUnits.Away} {
		setSPUnits(side, "34.7", "18.2", "1.4")
	}
	if got := withUnits.Explain("SP+"); !strings.Contains(got, summary) {
		t.Errorf("Explain(%q) = %q, which never says how to read the units beneath it", "SP+", got)
	}
}

// Called on a matchup a page reached before any sync ran.
func TestExplainIsNilSafe(t *testing.T) {
	var absent *Matchup

	if got := absent.Explain("SP+"); got == "" {
		t.Error("an absent matchup dropped the explanation of a row it still draws")
	}
	if got := absent.Explain("no such row"); got != "" {
		t.Errorf("Explain(%q) = %q, want empty", "no such row", got)
	}
}
