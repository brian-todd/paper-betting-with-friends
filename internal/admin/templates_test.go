package admin

import (
	"bytes"
	"strings"
	"testing"
	"time"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func adminUser() *models.User {
	return &models.User{ID: uuid.New(), Username: "cfb-pbwf-admin", IsAdmin: true}
}

func newTestRenderer(t *testing.T) *templates.Renderer {
	t.Helper()

	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	return renderer
}

// render executes a page and fails on any template error.
//
// The admin pages used to reach for a request object the handlers never put in
// the data, so every page truncated at its first success banner -- invisibly,
// because the handlers discarded the render error. Asserting on it here is what
// keeps that from coming back.
func render(t *testing.T, name string, data map[string]any) string {
	t.Helper()

	data["User"] = adminUser()
	data["Title"] = name

	var buf bytes.Buffer
	if err := newTestRenderer(t).Render(&buf, name, data); err != nil {
		t.Fatalf("Render(%q) error = %v", name, err)
	}
	return buf.String()
}

func TestAdminDashboardRenders(t *testing.T) {
	html := render(t, "admin", map[string]any{})

	for _, link := range []string{"/admin/users", "/admin/leagues", "/admin/bets", "/admin/games", "/admin/sync", "/admin/audit"} {
		if !strings.Contains(html, link) {
			t.Errorf("dashboard is missing a link to %s", link)
		}
	}
}

// TestAdminUsersPageProtectsTheAdministrator is the template half of the
// singleton rule: the page must not offer a rename or delete the service would
// only refuse.
func TestAdminUsersPageProtectsTheAdministrator(t *testing.T) {
	users := []UserView{
		{
			User:      models.User{ID: uuid.New(), Username: "cfb-pbwf-admin", IsAdmin: true, CreatedAt: time.Now()},
			Protected: true,
		},
		{
			User:      models.User{ID: uuid.New(), Username: "testalice", CreatedAt: time.Now()},
			Protected: false,
			Purses: []models.Purse{
				{Balance: decimal.RequireFromString("1250.50"), League: models.League{Name: "Sunday Money"}},
			},
		},
	}

	html := render(t, "admin_users", map[string]any{"Users": users, "AdminUsername": "cfb-pbwf-admin"})

	if strings.Count(html, "/username") != 1 || strings.Count(html, "/delete") != 1 {
		t.Error("rename and delete should be offered for exactly the one unprotected user")
	}
	if !strings.Contains(html, "site administrator account") {
		t.Error("the protected account should say why its controls are missing")
	}
	if !strings.Contains(html, "1250.50") {
		t.Error("purse balances should be listed")
	}
}

func TestAdminUsersPageShowsBanners(t *testing.T) {
	data := map[string]any{"Users": []UserView{}, "Success": "Password updated."}
	if html := render(t, "admin_users", data); !strings.Contains(html, "Password updated.") {
		t.Error("success banner did not render")
	}

	data = map[string]any{"Users": []UserView{}, "Error": "Password must be at least 8 characters."}
	if html := render(t, "admin_users", data); !strings.Contains(html, "at least 8 characters") {
		t.Error("error banner did not render")
	}
}

func TestAdminLeaguesPageRenders(t *testing.T) {
	leagueID, userID := uuid.New(), uuid.New()
	leagues := []LeagueView{
		{
			League: models.League{
				ID: leagueID, Name: "Sunday Money", InviteCode: "abc12345",
				StartingBalance: decimal.RequireFromString("1000"),
				Creator:         models.User{Username: "cfb-pbwf-admin"},
				CreatedAt:       time.Now(),
			},
			Members: []MemberView{
				{
					Member:   models.LeagueMember{LeagueID: leagueID, UserID: userID, Role: "admin", User: models.User{Username: "testalice"}},
					Balance:  decimal.RequireFromString("980.25"),
					HasPurse: true,
				},
				{
					Member:   models.LeagueMember{LeagueID: leagueID, UserID: uuid.New(), Role: "member", User: models.User{Username: "testbob"}},
					HasPurse: false,
				},
			},
		},
	}

	html := render(t, "admin_leagues", map[string]any{
		"Leagues": leagues,
		"Users":   []models.User{{ID: userID, Username: "testalice"}},
	})

	if !strings.Contains(html, "980.25") {
		t.Error("member balance did not render")
	}
	// A member with no purse cannot bet at all, so the page has to say so
	// rather than showing a blank cell.
	if !strings.Contains(html, "no purse") {
		t.Error("a member without a purse should be flagged")
	}
	if !strings.Contains(html, "Type Sunday Money") {
		t.Error("league deletion should require the name to be typed")
	}
}

func TestAdminBetsPageRenders(t *testing.T) {
	leagueID := uuid.New()
	view := bets.BetView{
		ID:   uuid.New(),
		Type: bets.BetTypeSpread,
		Game: models.Game{
			ID:          uuid.New(),
			ScheduledAt: time.Now(),
			HomeTeam:    models.Team{Abbreviation: "BAMA"},
			AwayTeam:    models.Team{Abbreviation: "AUB"},
		},
		League:       models.League{ID: leagueID, Name: "Sunday Money"},
		User:         models.User{Username: "testalice"},
		Pick:         "BAMA",
		Line:         "-7",
		OddsSnapshot: decimal.RequireFromString("-110"),
		Stake:        decimal.RequireFromString("50"),
		Status:       models.BetStatusPending,
	}

	html := render(t, "admin_bets", map[string]any{
		"Bets":             []bets.BetView{view},
		"Users":            []models.User{{ID: uuid.New(), Username: "testalice"}},
		"Leagues":          []models.League{{ID: leagueID, Name: "Sunday Money"}},
		"SelectedLeague":   leagueID.String(),
		"SelectedUser":     "",
		"SelectedStatus":   "pending",
		"Statuses":         []models.BetStatus{models.BetStatusPending, models.BetStatusWon},
		"SettleableStatus": []models.BetStatus{models.BetStatusWon, models.BetStatusLost},
	})

	if !strings.Contains(html, "AUB @ BAMA") {
		t.Error("matchup did not render")
	}
	if !strings.Contains(html, "testalice") {
		t.Error("the bet's owner should be shown, since this list spans users")
	}
	// The selected filter has to come back selected or the page lies about
	// which subset is on screen.
	if !strings.Contains(html, `value="`+leagueID.String()+`" selected`) {
		t.Error("the selected league filter was not marked selected")
	}
}

func TestAdminSyncPageRenders(t *testing.T) {
	now := time.Now()
	jobs := []scheduler.Status{
		{Name: "cfb-games-and-lines", Label: "Football", LastRun: now, LastSuccess: now, NextRun: now.Add(time.Hour)},
		{Name: "cfb-calendar", LastRun: now, LastError: "upstream is down"},
		{Name: "cbb-games-and-lines", Label: "Basketball", Running: true},
	}

	health := SystemHealth{
		Counts:        repository.Stats{Users: 4, Leagues: 1, Games: 900, Results: 120, FinalResults: 118, PendingBets: 7},
		CFBConfigured: true,
		TimeZone:      "America/New_York",
		Env:           "development",
		Season:        2025,
		Week:          9,
		SeasonType:    models.SeasonTypeRegular,
		WeekFound:     true,
	}

	html := render(t, "admin_sync", map[string]any{"Jobs": jobs, "Health": health})

	// An internal job carries no Label and so stays out of the site footer, but
	// the operator page is exactly where it has to be visible.
	if !strings.Contains(html, "cfb-calendar") {
		t.Error("an unlabelled job should still be listed on the admin page")
	}
	if !strings.Contains(html, "upstream is down") {
		t.Error("the last error should be shown")
	}
	if !strings.Contains(html, "2025 week 9") {
		t.Error("the resolved current week should be shown")
	}
	if !strings.Contains(html, "basketball sync is disabled") {
		t.Error("a missing API key should be called out as disabling that sync")
	}
}

func TestAdminGamesPageRenders(t *testing.T) {
	html := render(t, "admin_games", map[string]any{
		"Query": "Alabama",
		"Games": []models.Game{
			{
				ID: uuid.New(), Sport: models.SportFootball, ScheduledAt: time.Now(),
				Status:   models.GameStatusFinal,
				HomeTeam: models.Team{Name: "Alabama"}, AwayTeam: models.Team{Name: "Auburn"},
				Result: &models.GameResult{HomeScore: 27, AwayScore: 24},
			},
		},
	})

	if !strings.Contains(html, "Auburn @ Alabama") {
		t.Error("matchup did not render")
	}
	// A result row exists as soon as a score is reported, which is not the same
	// as the game being over.
	if !strings.Contains(html, "provisional") {
		t.Error("a result with no FinalizedAt should be flagged provisional")
	}
}

func TestAdminGameDetailDistinguishesProvisionalFromFinal(t *testing.T) {
	game := models.Game{
		ID: uuid.New(), Sport: models.SportFootball, Season: 2025, SeasonType: "regular",
		ScheduledAt: time.Now(), Status: models.GameStatusFinal,
		HomeTeam: models.Team{Name: "Alabama", Abbreviation: "BAMA"},
		AwayTeam: models.Team{Name: "Auburn", Abbreviation: "AUB"},
	}

	provisional := render(t, "admin_game_detail", map[string]any{
		"Detail": &GameDetail{
			Game:   game,
			Result: &models.GameResult{HomeScore: 27, AwayScore: 24},
			SpreadOdds: []models.SpreadOdds{{
				Source:     models.OddsSourceDraftKings,
				HomeSpread: decimal.RequireFromString("-7"), AwaySpread: decimal.RequireFromString("7"),
				HomeOdds: decimal.RequireFromString("-110"), AwayOdds: decimal.RequireFromString("-110"),
			}},
		},
	})

	if !strings.Contains(provisional, "Mark Final") {
		t.Error("a provisional result should offer the finalize action")
	}
	if !strings.Contains(provisional, "DraftKings") && !strings.Contains(provisional, "draftkings") {
		t.Error("odds rows did not render")
	}

	finalized := time.Now()
	final := render(t, "admin_game_detail", map[string]any{
		"Detail": &GameDetail{
			Game:        game,
			Result:      &models.GameResult{HomeScore: 27, AwayScore: 24, FinalizedAt: &finalized},
			ResultFinal: true,
			FinalizedAt: finalized,
		},
	})

	// Offering it on a settled game invites a pointless second settlement.
	if strings.Contains(final, "Mark Final") {
		t.Error("a final result should not offer the finalize action")
	}
}

func TestAdminAuditPageRenders(t *testing.T) {
	target := uuid.New()
	html := render(t, "admin_audit", map[string]any{
		"Entries": []models.AuditLog{
			{
				ActorUsername: "cfb-pbwf-admin",
				Action:        models.AuditActionPurseSet,
				TargetType:    models.AuditTargetPurse,
				TargetID:      &target,
				Detail:        "1000.00 -> 500.00",
				CreatedAt:     time.Now(),
			},
		},
	})

	if !strings.Contains(html, "1000.00 -&gt; 500.00") {
		t.Error("the before/after detail did not render")
	}
	if !strings.Contains(html, models.AuditActionPurseSet) {
		t.Error("the action did not render")
	}
}
