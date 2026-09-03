package games

import (
	"bytes"
	"maps"
	"strings"
	"testing"
	"time"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
)

// renderGameDetail renders the bet slip for real. A parse succeeds happily on an
// index into a map that blows up at execution time, so executing is the check
// worth having.
func renderGameDetail(t *testing.T, leagueID uuid.UUID, data map[string]any) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	weekID := uuid.New()
	page := map[string]any{
		"Title": "CLEM @ GT",
		"User":  &models.User{Username: "tester"},
		"Game": models.Game{
			ID:          uuid.New(),
			WeekID:      &weekID,
			ScheduledAt: time.Now().Add(3 * time.Hour),
			Status:      models.GameStatusScheduled,
			HomeTeam:    models.Team{Abbreviation: "GT"},
			AwayTeam:    models.Team{Abbreviation: "CLEM"},
		},
		"UserLeagues":      []models.League{{ID: leagueID, Name: "Test League"}},
		"PurseBalances":    map[string]string{leagueID.String(): "1000.00"},
		"HolyLockEligible": true,
	}
	maps.Copy(page, data)

	var buf bytes.Buffer
	if err := renderer.Render(&buf, "game_detail", page); err != nil {
		t.Fatalf("rendering game detail: %v", err)
	}
	return buf.String()
}

func TestBetSlipOffersHolyLock(t *testing.T) {
	league := uuid.New()
	html := renderGameDetail(t, league, nil)

	// One checkbox per bet form, so the choice is there whichever tab is open.
	if got := strings.Count(html, `name="holy_lock"`); got != 3 {
		t.Errorf("bet slip has %d Holy Lock checkboxes, want 3 (one per form)", got)
	}
	for _, want := range []string{`id="spread-holy-lock"`, `id="moneyline-holy-lock"`, `id="overunder-holy-lock"`} {
		if !strings.Contains(html, want) {
			t.Errorf("bet slip is missing %q", want)
		}
	}
	// With no conflict the league option carries no marker for the script.
	if strings.Contains(html, "data-holy-lock=") {
		t.Error("a league with a free Holy Lock was marked as taken")
	}
}

// A basketball game has no week, so there is no slot its bets could occupy.
func TestBetSlipHidesHolyLockOnGameWithNoWeek(t *testing.T) {
	league := uuid.New()
	html := renderGameDetail(t, league, map[string]any{
		"Game": models.Game{
			ID:          uuid.New(),
			ScheduledAt: time.Now().Add(3 * time.Hour),
			Status:      models.GameStatusScheduled,
			HomeTeam:    models.Team{Abbreviation: "GT"},
			AwayTeam:    models.Team{Abbreviation: "CLEM"},
		},
		"HolyLockEligible": false,
	})

	if strings.Contains(html, `name="holy_lock"`) {
		t.Error("bet slip offered a Holy Lock on a game with no week")
	}
}

// The conflict rides on the league option so the script can name the bet
// holding the week the moment that league is selected.
func TestBetSlipMarksLeagueWithExistingHolyLock(t *testing.T) {
	league := uuid.New()
	html := renderGameDetail(t, league, map[string]any{
		"HolyLockConflicts": map[string]string{league.String(): "GT -7 (CLEM @ GT)"},
	})

	if !strings.Contains(html, `data-holy-lock="GT -7 (CLEM @ GT)"`) {
		t.Error("the league option does not carry the existing Holy Lock")
	}
	// The checkbox still renders; the script is what disables it.
	if !strings.Contains(html, `name="holy_lock"`) {
		t.Error("the Holy Lock checkbox vanished instead of being marked")
	}
}

// The page must render for a caller that never set the conflicts key at all --
// indexing an untyped nil is a template execution error, not a parse one.
func TestBetSlipRendersWithoutConflictData(t *testing.T) {
	league := uuid.New()
	html := renderGameDetail(t, league, map[string]any{"HolyLockConflicts": nil})

	if !strings.Contains(html, `name="holy_lock"`) {
		t.Error("bet slip did not render without conflict data")
	}
}
