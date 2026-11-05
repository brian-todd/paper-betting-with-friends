package games_test

// The game page's redirect for a URL naming the wrong sport, called directly so a test
// can see what the handler did before answering.

import (
	"net/http"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/pagetest"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
)

// countingBetReader counts how often the handler asks about the viewer's bets.
type countingBetReader struct{ calls int }

func (c *countingBetReader) UserBetSummaries(uuid.UUID, []uuid.UUID) (map[uuid.UUID]string, error) {
	c.calls++
	return nil, nil
}

func TestTheWrongSportRedirectsBeforeLoadingTheViewersData(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	user := env.Register("sport-checker", "correct-horse-battery-staple")
	game := testdb.InsertGame(t, env.DB, models.Game{
		Sport: models.SportBasketball, ScheduledAt: firstSnapshot.Add(24 * time.Hour),
	})

	service := games.NewService(env.DB, env.Location)
	service.SetClock(env.Now)
	handler := games.NewHandler(service, env.Renderer, env.DB)
	bets := &countingBetReader{}
	handler.SetUserBetReader(bets)

	show := func(t *testing.T, sport string) int {
		t.Helper()
		rec, req := env.GET("/games/"+sport+"/"+game.ID.String(), user)
		req.SetPathValue("sport", sport)
		req.SetPathValue("gameID", game.ID.String())
		handler.ShowGameDetail(rec, req)
		return rec.Code
	}

	if got := show(t, models.SportFootball); got != http.StatusMovedPermanently {
		t.Errorf("the wrong sport: status %d, want 301", got)
	}
	if bets.calls != 0 {
		t.Errorf("the redirect asked about the viewer's bets %d times first", bets.calls)
	}

	// So that the zero above means something: the page itself does ask.
	if got := show(t, models.SportBasketball); got != http.StatusOK {
		t.Errorf("the right sport: status %d, want 200", got)
	}
	if bets.calls != 1 {
		t.Errorf("the page asked about the viewer's bets %d times, want once", bets.calls)
	}
}
