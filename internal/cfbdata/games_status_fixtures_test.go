package cfbdata_test

import (
	"context"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// /games carries no status. It carries a `completed` flag and a start time, and
// syncGames turns the pair into one of three stored statuses by asking what
// time it is. For every division outside CFB_SCOREBOARD_CLASSIFICATIONS and
// every week outside the current one, that inference is the only status there
// is -- so it decides whether a game can be bet on, whether a placed bet can be
// edited, and whether it can be cancelled for a refund.
//
// Nothing tested it against a recorded response, because until the clock was
// injectable nothing could: weeks 1 and 2 of the captures report every game
// completed, so they infer `final` whatever the clock says. Week 6 was captured
// before it was played and is the other half -- 275 games, none completed, no
// points, and real kickoffs to compare a clock against.
func TestGamesStatusIsInferredFromTheClock(t *testing.T) {
	db := testdb.Open(t)

	// Before every week-6 kickoff. The reference seed's own clock is irrelevant
	// to week 6 and is set here only so the seeded week-1 rows are deterministic.
	beforeKickoffs := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	seedReference(t, db, beforeKickoffs)

	t.Run("before any kickoff every game is scheduled", func(t *testing.T) {
		newFeed(t, db, beforeKickoffs).games(futureWeek)

		got := statuses(t, db, futureWeek)
		if got[models.GameStatusScheduled] == 0 {
			t.Fatal("no week 6 games were written at all; every assertion here would pass vacuously")
		}
		if n := got[models.GameStatusInProgress]; n != 0 {
			t.Errorf("%d games read as in progress a week before the earliest kickoff", n)
		}
		if n := got[models.GameStatusFinal]; n != 0 {
			t.Errorf("%d games read as final, but /games reports none of week 6 completed", n)
		}
	})

	// Mid-slate on the Saturday of week 6. Some games have kicked off, some have
	// not, which is the only clock at which the inference is doing any work.
	midSlate := time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)

	t.Run("mid-slate splits the week by kickoff", func(t *testing.T) {
		newFeed(t, db, midSlate).games(futureWeek)

		got := statuses(t, db, futureWeek)
		if got[models.GameStatusScheduled] == 0 || got[models.GameStatusInProgress] == 0 {
			t.Fatalf("both sides of the inference need to be populated for this to mean anything: %v", got)
		}
		if n := got[models.GameStatusFinal]; n != 0 {
			t.Errorf("%d games read as final; the clock may not finish a game /games has not", n)
		}

		// The property, rather than a recount of the same arithmetic: nothing
		// reads as in progress whose stored kickoff is still ahead.
		var early int64
		if err := db.Model(&models.Game{}).
			Joins("JOIN weeks ON weeks.id = games.week_id").
			Where("games.sport = ? AND games.status = ? AND games.scheduled_at > ?",
				models.SportFootball, models.GameStatusInProgress, midSlate).
			Where("weeks.season = ? AND weeks.number = ? AND weeks.season_type = ?",
				fixtureYear, futureWeek, models.SeasonTypeRegular).
			Count(&early).Error; err != nil {
			t.Fatalf("counting: %v", err)
		}
		if early != 0 {
			t.Errorf("%d games read as in progress with a kickoff still in the future", early)
		}
	})

	// The case the feed marks and the sync did not read.
	//
	// A startTimeTBD game carries a placeholder instant -- midnight of the day
	// the feed expects it on -- not an unknown. applyScoreboardGame already
	// refuses to store one, with a comment saying why. syncGames never looks at
	// the flag, so it stores the placeholder and then infers a status from it,
	// and once that midnight is past the game reads as being played.
	t.Run("a game the feed has not scheduled is not in progress", func(t *testing.T) {
		newFeed(t, db, midSlate).games(futureWeek)

		tbd := tbdGames(t, futureWeek)
		var checked int
		for _, g := range tbd {
			game, _ := findGame(db, g.ID)
			if game == nil {
				// syncGames could not resolve a team, a venue or a week for it.
				continue
			}
			checked++
			if game.Status == models.GameStatusInProgress {
				t.Errorf("game %d (%s v %s) has no announced kickoff -- the feed sends startTimeTBD "+
					"with the placeholder %s -- and reads as in progress at %s. Betting is closed on "+
					"it, every bet already placed is neither editable nor cancellable, and "+
					"advancesFrom has no edge back to scheduled",
					g.ID, g.HomeTeam, g.AwayTeam, g.StartDate.Format(time.RFC3339), midSlate.Format(time.RFC3339))
				return // one is the whole finding; 39 of them is noise
			}
		}

		// And the half of it that is not fixed, asserted rather than left in a
		// comment, so that closing it has to come through here.
		//
		// The status no longer comes off the placeholder, but the placeholder is
		// still what lands in scheduled_at: the column is NOT NULL, the first
		// insert has nothing better, and the feed genuinely does not know the
		// time. So the betting cutoff -- which reads scheduled_at and not the
		// status -- still closes on these games at midnight of the day they are
		// played. Fixing that needs a column saying the instant is a placeholder,
		// and a decision about whether such a game takes bets at all.
		if checked == 0 {
			t.Fatalf("none of the %d startTimeTBD games in the capture reached the database, "+
				"so nothing above was checked", len(tbd))
		}
		t.Logf("checked %d of %d startTimeTBD games", checked, len(tbd))

		first := tbd[0]
		game, _ := gameByExternalID(t, db, first.ID)
		if !game.ScheduledAt.Equal(first.StartDate) {
			t.Errorf("game %d stores %s, want the feed's placeholder %s -- if this now "+
				"stores something better, the known gap above has been closed and this "+
				"assertion should say so",
				first.ID, game.ScheduledAt.Format(time.RFC3339), first.StartDate.Format(time.RFC3339))
		}
	})
}

// tbdGames is the games in a week the feed has not announced a kickoff for.
//
// Read from the capture rather than from the database, because the database has
// nowhere to put the flag -- which is the point. The input comes from the
// recording; the assertion is on what the sync did with it.
func tbdGames(t *testing.T, week int) []cfbdata.APIGame {
	t.Helper()

	base, _, stop, err := fixtureserver.Listen(fixtures.CFBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	all, err := cfbdata.NewClientAt(base, "").GetGames(context.Background(), fixtureYear, &week, nil)
	if err != nil {
		t.Fatalf("reading the week %d capture: %v", week, err)
	}

	var tbd []cfbdata.APIGame
	for _, g := range all {
		if g.StartTimeTBD {
			tbd = append(tbd, g)
		}
	}
	if len(tbd) == 0 {
		t.Fatalf("the week %d capture holds no startTimeTBD game, so this proves nothing", week)
	}
	return tbd
}
