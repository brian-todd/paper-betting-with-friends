package bets_test

// Level 3 for the bet slip: place a bet through the real handler, edit it
// through the real handler, and read the answer off the rendered page.
//
// The bug this is built around is a quiet one. Bet repositories save with
// Omit(clause.Associations) because FindByID preloads the odds row, and a plain
// Save writes that preloaded row's ID back over the foreign key -- so an edit
// that moved a bet onto a different line kept it pointing at the old one while
// the snapshot changed. Nothing errors, the purse moves correctly, the numbers
// on the bet are the new ones, and the only visible symptom is on a page: the
// edit form reopens preselecting the line the bet is no longer on, so the next
// edit silently moves it back.
//
// That is why this is a page test rather than a repository one. The row and the
// page disagree, and only the page says which.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/pagetest"
)

// firstSnapshot is the first of the twenty-six live scoreboard captures, and so
// a real moment inside week 1's slate rather than one invented here. At that
// instant 306 of the week's 412 games have been played and 106 have not, which
// is what makes a bet placeable at all: both the edit guard and the bet slip
// gate on the kickoff.
const (
	fixtureYear = 2026
	fixtureWeek = 1
)

var firstSnapshot = time.Date(2026, 9, 5, 20, 49, 9, 0, time.UTC)

func TestEditingABetMovesTheLineItsFormOffers(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	env.SeedFootball(fixtureYear, fixtureWeek)

	user := env.Register("line-mover", "correct-horse-battery-staple")
	league := env.League("Level Three", user, "1000")

	service := bets.NewService(env.DB)
	service.SetClock(env.Now)
	handler := bets.NewHandler(service, env.Renderer, env.DB)

	// A game the reader could still bet on at env.At, priced by two different
	// books. Both halves matter: one line to place on and a second to move to,
	// and a kickoff the edit guard has not closed.
	//
	// It is also a game the feed calls final, because week 1 was captured after
	// it was played. That is not a contrivance, it is the documented rule --
	// editable() and authorizeEdit() both gate on scheduled_at rather than
	// status, because status only advances when a sync runs. A fixture that
	// happens to be final and unkicked is the cheapest possible proof that the
	// guard reads the kickoff.
	var game struct {
		ID    string
		First string
		Last  string
	}
	err := env.DB.Raw(`
		SELECT g.id,
		       (SELECT o.id::text FROM spread_odds o WHERE o.game_id = g.id ORDER BY o.source LIMIT 1) AS first,
		       (SELECT o.id::text FROM spread_odds o WHERE o.game_id = g.id ORDER BY o.source DESC LIMIT 1) AS last
		FROM games g
		WHERE g.scheduled_at > ?
		  AND (SELECT count(*) FROM spread_odds o WHERE o.game_id = g.id) >= 2
		ORDER BY g.scheduled_at
		LIMIT 1`, env.At).Scan(&game).Error
	if err != nil || game.ID == "" {
		t.Fatalf("finding a game with two books and a kickoff still ahead of %s: %v", env.At, err)
	}
	if game.First == game.Last {
		t.Fatalf("game %s reported the same odds row twice", game.ID)
	}

	place := url.Values{
		"game_id":   {game.ID},
		"league_id": {league.ID.String()},
		"pick":      {string(models.SpreadPickHome)},
		"stake":     {"25"},
		"odds_id":   {game.First},
	}
	rec, req := env.POST("/bets/spread", place, user)
	handler.CreateSpreadBet(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("placing the bet: status %d (%s), want 303", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	betID := onlySpreadBet(t, env, user.ID)

	edit := url.Values{
		"pick":    {string(models.SpreadPickHome)},
		"stake":   {"25"},
		"odds_id": {game.Last},
	}
	rec, req = env.POST("/bets/spread/"+betID+"/edit", edit, user)
	req.SetPathValue("id", betID)
	handler.UpdateSpreadBet(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("editing the bet: status %d (%s)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	t.Run("the row points at the line it was moved to", func(t *testing.T) {
		var stored string
		err := env.DB.Raw(`SELECT spread_odds_id FROM spread_bets WHERE id = ?`, betID).Scan(&stored).Error
		if err != nil {
			t.Fatalf("reading the bet back: %v", err)
		}
		if stored != game.Last {
			t.Errorf("bet points at odds %s, want %s -- the preloaded association was written back over the foreign key",
				stored, game.Last)
		}
	})

	t.Run("the form reopens on the line the snapshot came from", func(t *testing.T) {
		rec, req := env.GET("/bets", user)
		handler.ListBets(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want 200", rec.Code)
		}
		body := rec.Body.String()

		selected := selectedOddsOn(body)
		if len(selected) != 1 {
			t.Fatalf("the edit form preselects %d lines, want exactly 1: %v", len(selected), selected)
		}
		if selected[0] != game.Last {
			t.Errorf("the edit form reopens on odds %s, want %s -- the page is offering to move the bet back",
				selected[0], game.Last)
		}

		// Both books have to be on offer, or "the right one is selected" is a
		// claim about a list with one entry in it.
		for _, id := range []string{game.First, game.Last} {
			if !strings.Contains(body, `value="`+id+`"`) {
				t.Errorf("odds %s is not among the lines the form offers", id)
			}
		}
	})
}

// selectedOption matches an odds choice the edit form reopens on.
var selectedOption = regexp.MustCompile(`<option value="([0-9a-f-]{36})" selected>`)

// selectedOddsOn is every preselected line in the rendered page's edit forms.
// There is one bet, so there should be one.
func selectedOddsOn(body string) []string {
	matches := selectedOption.FindAllStringSubmatch(body, -1)
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match[1])
	}
	return ids
}

// onlySpreadBet is the id of the user's one spread bet, and a failure if they
// have any other number of them.
func onlySpreadBet(t *testing.T, env *pagetest.Env, userID uuid.UUID) string {
	t.Helper()

	var ids []string
	if err := env.DB.Raw(`SELECT id FROM spread_bets WHERE user_id = ?`, userID).Scan(&ids).Error; err != nil {
		t.Fatalf("listing the user's spread bets: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("the user holds %d spread bets, want 1", len(ids))
	}
	return ids[0]
}
