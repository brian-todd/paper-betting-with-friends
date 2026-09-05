package leagues

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
	"github.com/shopspring/decimal"
)

// renderLeagueDetail renders the league page for real. A parse succeeds happily
// on a field access that blows up at execution time, so executing the template
// is the only check worth having.
func renderLeagueDetail(t *testing.T, data map[string]any) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	isPublic := true
	page := map[string]any{
		"Title": "Test League",
		"User":  &models.User{Username: "tester"},
		"League": &models.League{
			ID:              uuid.New(),
			Name:            "Test League",
			IsPublic:        &isPublic,
			StartingBalance: decimal.RequireFromString("1000"),
			Creator:         models.User{Username: "tester"},
		},
		"IsMember":     true,
		"PurseBalance": decimal.RequireFromString("1000"),
	}
	maps.Copy(page, data)

	var buf bytes.Buffer
	if err := renderer.Render(&buf, "league_detail", page); err != nil {
		t.Fatalf("rendering league detail: %v", err)
	}
	return buf.String()
}

func TestLeaguePageRendersHolyLockRecord(t *testing.T) {
	html := renderLeagueDetail(t, map[string]any{
		"Leaderboard": []LeaderboardEntry{
			{Rank: 1, Username: "tester", Balance: decimal.RequireFromString("1200"), Wins: 8, Losses: 4, LockWins: 3, LockLosses: 1},
			{Rank: 2, Username: "alice", Balance: decimal.RequireFromString("900"), Wins: 2, Losses: 6},
		},
	})

	if !strings.Contains(html, ">Locks</th>") {
		t.Error("the leaderboard has no Locks column")
	}
	for _, want := range []string{`<span class="record-wins">3W</span>`, `<span class="record-losses">1L</span>`} {
		if !strings.Contains(html, want) {
			t.Errorf("the Holy Lock record is missing %q", want)
		}
	}
	// A member who has never had a lock settle gets a dash, not "0W-0L".
	if !strings.Contains(html, `<span class="text-muted">—</span>`) {
		t.Error("a member with no settled locks should render a dash")
	}
}

func TestLeaguePageRendersHolyLockSection(t *testing.T) {
	html := renderLeagueDetail(t, map[string]any{
		"HolyLocks": []HolyLockWeek{{
			Label: "2026 · Week 1",
			Rows: []HolyLockEntry{
				{
					Username:      "tester",
					IsCurrentUser: true,
					Matchup:       "CLEM @ GT",
					Pick:          "GT -7",
					Stake:         decimal.RequireFromString("50"),
					Status:        models.BetStatusWon,
					ScheduledAt:   time.Date(2026, 9, 5, 19, 30, 0, 0, time.UTC),
				},
				{
					Username:    "alice",
					Matchup:     "UGA @ FSU",
					Pick:        "Over 54.5",
					Stake:       decimal.RequireFromString("25"),
					Status:      models.BetStatusPending,
					ScheduledAt: time.Date(2026, 9, 5, 23, 0, 0, 0, time.UTC),
				},
			},
		}},
	})

	for _, want := range []string{
		">Holy Locks</h3>",
		">2026 · Week 1</summary>",
		"CLEM @ GT",
		"GT -7",
		"Over 54.5",
		"$50.00",
		`class="current-user-row"`,
		`badge-status-won`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the Holy Locks section is missing %q", want)
		}
	}
}

// The section is a whole card, so with no locks in the league it should not
// render an empty table header at all.
func TestLeaguePageOmitsHolyLockSectionWhenEmpty(t *testing.T) {
	html := renderLeagueDetail(t, map[string]any{"HolyLocks": []HolyLockWeek{}})

	if strings.Contains(html, "holy-locks-card") {
		t.Error("the Holy Locks section rendered with no locks to show")
	}
}

// renderLeaguesIndex renders the /leagues page for real, the same way
// renderLeagueDetail does for the league page.
func renderLeaguesIndex(t *testing.T, data map[string]any) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	page := map[string]any{
		"Title": "My Leagues",
		"User":  &models.User{Username: "tester"},
	}
	maps.Copy(page, data)

	var buf bytes.Buffer
	if err := renderer.Render(&buf, "leagues", page); err != nil {
		t.Fatalf("rendering leagues index: %v", err)
	}
	return buf.String()
}

// renderNamePartial renders one of the inline-rename fragments.
func renderNamePartial(t *testing.T, name string, data map[string]any) string {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}

	var buf bytes.Buffer
	if err := renderer.RenderPartial(&buf, name, data); err != nil {
		t.Fatalf("rendering partial %s: %v", name, err)
	}
	return buf.String()
}

// collapseSpace normalises indentation so markup written at two nesting depths
// can be compared.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func ownedLeagues(t *testing.T) (owned, joined UserLeague) {
	t.Helper()

	public := true
	return UserLeague{
		ID:       uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:     "My League",
		IsPublic: &public,
		Creator:  models.User{Username: "tester"},
		IsOwner:  true,
	}, UserLeague{
		ID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:     "Someone Elses League",
		IsPublic: &public,
		Creator:  models.User{Username: "alice"},
		IsOwner:  false,
	}
}

func TestLeaguesIndexOffersRenameToOwnerOnly(t *testing.T) {
	owned, joined := ownedLeagues(t)
	html := renderLeaguesIndex(t, map[string]any{"MyLeagues": []UserLeague{owned, joined}})

	if !strings.Contains(html, `hx-get="/leagues/`+owned.ID.String()+`/name/edit"`) {
		t.Error("the owner's card has no rename control")
	}
	if strings.Contains(html, `/leagues/`+joined.ID.String()+`/name`) {
		t.Error("a league the viewer only joined offers a rename control")
	}
	// Both cards still link through to the league itself.
	for _, league := range []UserLeague{owned, joined} {
		if !strings.Contains(html, `<a href="/leagues/`+league.ID.String()+`">`) {
			t.Errorf("league %s has no link to its page", league.Name)
		}
	}
}

// The page inlines the resting-state markup so a card starts in it, and the
// partial replaces that element once the name is edited. Nothing makes the two
// agree, so this is what notices when one of them changes alone.
func TestLeaguesIndexInlinesTheNamePartial(t *testing.T) {
	owned, _ := ownedLeagues(t)

	partial := renderNamePartial(t, "league_name", map[string]any{"League": &owned.League})
	html := renderLeaguesIndex(t, map[string]any{"MyLeagues": []UserLeague{owned}})

	if !strings.Contains(collapseSpace(html), collapseSpace(partial)) {
		t.Errorf("the leagues page no longer inlines templates/partials/league_name.html\nwant to find: %s", collapseSpace(partial))
	}
}

func TestLeagueNameEditPartial(t *testing.T) {
	owned, _ := ownedLeagues(t)
	id := owned.ID.String()

	t.Run("posts to the rename route with and without htmx", func(t *testing.T) {
		html := renderNamePartial(t, "league_name_edit", map[string]any{"League": &owned.League})

		for _, want := range []string{
			`action="/leagues/` + id + `/name" method="POST"`,
			`hx-post="/leagues/` + id + `/name"`,
			`hx-target="#league-name-` + id + `"`,
			`value="My League"`,
			// Cancel re-reads the stored name rather than hiding the form.
			`hx-get="/leagues/` + id + `/name"`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("the rename form is missing %q", want)
			}
		}
		if strings.Contains(html, "league-name-error") {
			t.Error("a form with no error should not render the error slot")
		}
	})

	t.Run("shows a validation message", func(t *testing.T) {
		html := renderNamePartial(t, "league_name_edit", map[string]any{
			"League": &models.League{ID: owned.ID, Name: ""},
			"Error":  "Enter a league name of 1 to 255 characters.",
		})

		if !strings.Contains(html, "Enter a league name of 1 to 255 characters.") {
			t.Error("the rename form dropped its error message")
		}
	})
}
