package games_test

// Level 3 is database to page: the real handler, the real service, the real
// queries and the real templates, over rows a recorded CFBD response actually
// wrote. Only the network is missing, and by this point it has already done its
// work -- fixtureseed replayed it into the transaction before the first render.
//
// What that buys over the handler tests beside it is the shape of the data. A
// hand-built row is somebody's belief about the feed: internal/testdb's
// InsertTeam hard-codes a lower-case "fbs" and a comment says that is the case
// CFBD reports. The comment is right. But a filter compared against the wrong
// case would agree with a fixture written in the wrong case, and the page would
// come back empty with every test green -- which is exactly how
// CFB_SCOREBOARD_CLASSIFICATIONS=FBS fetched the right division and matched no
// team.
//
// package games_test, not games: fixtureseed imports cfbdata, and pagetest
// imports fixtureseed, so an internal test file reaching either would be an
// import cycle waiting to happen.

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/pagetest"
)

// The season and week the committed football fixtures cover in full, and the
// instant the page is rendered at.
//
// firstSnapshot is the first of the twenty-six live scoreboard captures, which
// makes it a real moment rather than one invented for a test: 2026-09-05 is a
// Saturday inside week 1's slate, with 306 of its 412 games already played and
// 106 still to come.
const (
	fixtureYear = 2026
	fixtureWeek = 1
)

var firstSnapshot = time.Date(2026, 9, 5, 20, 49, 9, 0, time.UTC)

// gameCardLink matches the anchor each game on the grid is wrapped in, which is
// the only per-game marker the page emits that a test can count without
// asserting on the styling around it.
var gameCardLink = regexp.MustCompile(`href="/games/([0-9a-f-]{36})"`)

func TestTheWeekPageRendersARealSlate(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	env.SeedFootball(fixtureYear, fixtureWeek)

	user := env.Register("grid-reader", "correct-horse-battery-staple")

	service := games.NewService(env.DB, env.Location)
	service.SetClock(env.Now)
	handler := games.NewHandler(service, env.Renderer, env.DB)

	// show renders one page of the week, with query the filter query string.
	show := func(t *testing.T, query string) (int, string) {
		t.Helper()

		target := "/games/" + strconv.Itoa(fixtureYear) + "/regular/" + strconv.Itoa(fixtureWeek)
		if query != "" {
			target += "?" + query
		}
		rec, req := env.GET(target, user)
		req.SetPathValue("season", strconv.Itoa(fixtureYear))
		req.SetPathValue("seasonType", string(models.SeasonTypeRegular))
		req.SetPathValue("week", strconv.Itoa(fixtureWeek))

		handler.ShowWeekGames(rec, req)
		return rec.Code, rec.Body.String()
	}

	// seeded is what the feed wrote, read straight from the transaction. Every
	// count below is compared against this rather than against a number typed
	// into the test, so a recapture moves the expectation with the fixture.
	var seeded int64
	if err := env.DB.Model(&models.Game{}).Count(&seeded).Error; err != nil {
		t.Fatalf("counting seeded games: %v", err)
	}
	if seeded == 0 {
		t.Fatal("the seed wrote no games, so nothing below is testing the page")
	}

	t.Run("the landing page narrows to bettable FBS", func(t *testing.T) {
		// /games with nothing selected is not /games with no filter: it
		// defaults to FBS games that carry a line, because the tiers below FBS
		// carry no lines at all and a card you cannot act on is not what a
		// landing page is for. Against the real slate that is a real cut --
		// 412 games down to the ones a reader could bet -- and the two halves
		// of the predicate are only both exercised by data that has games
		// failing each half separately.
		code, body := show(t, "")
		if code != http.StatusOK {
			t.Fatalf("status %d, want 200", code)
		}

		ids := gameIDsOn(body)
		switch {
		case len(ids) == 0:
			t.Fatal("the default view is empty; the landing page shows nothing at all")
		case int64(len(ids)) == seeded:
			t.Fatalf("the default view shows all %d games, so neither half of the default filter is doing anything", seeded)
		}

		fbs := gamesInTier(t, env, "fbs")
		priced := gamesWithALine(t, env)
		for _, id := range ids {
			if !fbs[id] {
				t.Errorf("the default view shows game %s, which is in no FBS matchup", id)
			}
			if !priced[id] {
				t.Errorf("the default view shows game %s, which carries no line to bet", id)
			}
		}
	})

	t.Run("the whole slate is reachable, one page at a time", func(t *testing.T) {
		pages := int((seeded + games.PageSize - 1) / games.PageSize)

		seen := map[string]bool{}
		for page := 1; page <= pages; page++ {
			code, body := show(t, "applied=1&page="+strconv.Itoa(page))
			if code != http.StatusOK {
				t.Fatalf("page %d: status %d, want 200", page, code)
			}

			ids := gameIDsOn(body)
			want := games.PageSize
			if page == pages {
				want = int(seeded) - (pages-1)*games.PageSize
			}
			if len(ids) != want {
				t.Errorf("page %d rendered %d games, want %d", page, len(ids), want)
			}
			for _, id := range ids {
				if seen[id] {
					t.Errorf("page %d repeats game %s from an earlier page", page, id)
				}
				seen[id] = true
			}
		}

		if len(seen) != int(seeded) {
			t.Errorf("paging through the week showed %d distinct games, want %d", len(seen), seeded)
		}
	})

	t.Run("every division checkbox the page offers matches something", func(t *testing.T) {
		// The tier codes are taken from TierOptions rather than written out
		// here, so this compares the list the template renders as checkboxes
		// against the case the feed stored -- which is the whole point. A tier
		// that matched nothing would render as an empty grid with a 200 and no
		// error anywhere, which is what CFB_SCOREBOARD_CLASSIFICATIONS=FBS did
		// to the scoreboard.
		covered := map[string]bool{}
		for _, tier := range games.TierOptions() {
			ids := allPages(t, show, "applied=1&tier="+tier.Code)
			if len(ids) == 0 {
				t.Errorf("tier %q (%s) matched no game in a slate holding all four divisions; "+
					"the stored classification case is probably not the one the filter compares against",
					tier.Code, tier.Label)
				continue
			}

			// A tier page must hold only that tier. The filter matches either
			// side of the matchup, so a game counts if either team is in it.
			inTier := gamesInTier(t, env, tier.Code)
			for _, id := range ids {
				if !inTier[id] {
					t.Errorf("tier %q page shows game %s, whose teams are in neither", tier.Code, id)
				}
				covered[id] = true
			}
		}

		// Between them the four checkboxes have to reach every game the feed
		// classified. What they cannot reach is a game whose teams CFBD left
		// unclassified, and there are a couple of those in every capture -- so
		// the claim is about the classified ones, and the query says which.
		var unreachable []string
		err := env.DB.Raw(`
			SELECT g.id
			FROM games g
			JOIN teams home ON home.id = g.home_team_id
			JOIN teams away ON away.id = g.away_team_id
			WHERE home.classification IN ('fbs','fcs','ii','iii')
			   OR away.classification IN ('fbs','fcs','ii','iii')`).Scan(&unreachable).Error
		if err != nil {
			t.Fatalf("listing the classified games: %v", err)
		}
		for _, id := range unreachable {
			if !covered[id] {
				t.Errorf("game %s is in a division the page offers, but no tier checkbox reaches it", id)
			}
		}
		if len(unreachable) == 0 {
			t.Fatal("no game in the slate has a classified team, so the check above proved nothing")
		}
	})

	t.Run("a played game carries its score onto the grid", func(t *testing.T) {
		// Week 1 was captured after it was played, so /games reports every game
		// completed with a real score. The grid renders a result only when one
		// is stored, which makes "the score is on the page" a claim about the
		// game_results join rather than about the template.
		var final struct {
			ID                   string
			HomeScore, AwayScore int
		}
		err := env.DB.Raw(`
			SELECT g.id, r.home_score, r.away_score
			FROM games g
			JOIN game_results r ON r.game_id = g.id
			WHERE r.finalized_at IS NOT NULL AND r.home_score <> r.away_score
			ORDER BY g.scheduled_at
			LIMIT 1`).Scan(&final).Error
		if err != nil || final.ID == "" {
			t.Fatalf("finding a finalized result: %v", err)
		}

		_, body := show(t, "applied=1&team="+url.QueryEscape(teamNameOf(t, env, final.ID)))
		if !strings.Contains(body, `href="/games/`+final.ID+`"`) {
			t.Fatalf("the game the team filter was built from is not on its own page")
		}

		card := cardFor(body, final.ID)
		for _, score := range []int{final.HomeScore, final.AwayScore} {
			want := `<span class="team-score">` + strconv.Itoa(score) + `</span>`
			if !strings.Contains(card, want) {
				t.Errorf("game %s renders no %q; card was:\n%s", final.ID, want, card)
			}
		}
	})
}

// allPages walks a filtered view to its end and returns every game on it, so a
// division with more than a page of games is not silently judged on its first
// hundred.
func allPages(t *testing.T, show func(*testing.T, string) (int, string), query string) []string {
	t.Helper()

	var ids []string
	seen := map[string]bool{}
	for page := 1; ; page++ {
		code, body := show(t, query+"&page="+strconv.Itoa(page))
		if code != http.StatusOK {
			t.Fatalf("%s page %d: status %d, want 200", query, page, code)
		}

		found := gameIDsOn(body)
		fresh := false
		for _, id := range found {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
				fresh = true
			}
		}
		// A page past the end clamps back onto the last one, so repeating
		// ourselves is how the walk knows it is done.
		if !fresh {
			return ids
		}
	}
}

// gameIDsOn pulls the game IDs out of a rendered grid, in the order the page
// put them.
func gameIDsOn(body string) []string {
	matches := gameCardLink.FindAllStringSubmatch(body, -1)
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match[1])
	}
	return ids
}

// cardFor is the slice of the page belonging to one game: from its own anchor
// to the next one, or to the end.
func cardFor(body, gameID string) string {
	start := strings.Index(body, `href="/games/`+gameID+`"`)
	if start < 0 {
		return ""
	}
	rest := body[start:]
	if next := gameCardLink.FindStringIndex(rest[1:]); next != nil {
		return rest[:next[0]+1]
	}
	return rest
}

// gamesWithALine is every game carrying an odds row of any market, which is the
// half of the default filter that is not about the division.
func gamesWithALine(t *testing.T, env *pagetest.Env) map[string]bool {
	t.Helper()

	var ids []string
	err := env.DB.Raw(`
		SELECT g.id FROM games g WHERE
			EXISTS (SELECT 1 FROM spread_odds o WHERE o.game_id = g.id) OR
			EXISTS (SELECT 1 FROM money_line_odds o WHERE o.game_id = g.id) OR
			EXISTS (SELECT 1 FROM over_under_odds o WHERE o.game_id = g.id)`).Scan(&ids).Error
	if err != nil {
		t.Fatalf("listing the priced games: %v", err)
	}

	priced := make(map[string]bool, len(ids))
	for _, id := range ids {
		priced[id] = true
	}
	return priced
}

// gamesInTier is every game with a team in the division, as a set. One query
// for the division rather than one per game: a page of a hundred cards checked
// a row at a time is four hundred round trips for an answer SQL gives once.
func gamesInTier(t *testing.T, env *pagetest.Env, tier string) map[string]bool {
	t.Helper()

	var ids []string
	err := env.DB.Raw(`
		SELECT g.id
		FROM games g
		JOIN teams home ON home.id = g.home_team_id
		JOIN teams away ON away.id = g.away_team_id
		WHERE home.classification = ? OR away.classification = ?`, tier, tier).Scan(&ids).Error
	if err != nil {
		t.Fatalf("listing the games in tier %q: %v", tier, err)
	}

	in := make(map[string]bool, len(ids))
	for _, id := range ids {
		in[id] = true
	}
	return in
}

// teamNameOf is a team name that narrows the grid to one game, so a score
// assertion does not have to page through four hundred cards to find it.
func teamNameOf(t *testing.T, env *pagetest.Env, gameID string) string {
	t.Helper()

	var name string
	err := env.DB.Raw(`
		SELECT home.name FROM games g JOIN teams home ON home.id = g.home_team_id WHERE g.id = ?`,
		gameID).Scan(&name).Error
	if err != nil || name == "" {
		t.Fatalf("reading the home team of game %s: %v", gameID, err)
	}
	return name
}
