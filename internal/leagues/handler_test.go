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
