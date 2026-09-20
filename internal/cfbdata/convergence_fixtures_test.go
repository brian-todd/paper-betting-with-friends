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

// Two football feeds write the same game row and they disagree for minutes at a
// time. AGENTS.md documents four rules that keep that from flapping, all of them
// in SQL so neither writer has to read before writing, and until now all of them
// were tested against rows a test author built -- which is to say against a
// disagreement invented by someone who already knew which feed was right.
//
// Here the disagreement is the recorded one. /games for week 1 was captured on
// 2026-09-20, by which time all 455 of its games were completed with real
// scores. The scoreboard series was captured live on 2026-09-05, and its first
// snapshot has 41 of those same games not yet started and 16 being played, with
// partial scores. Every game in the scoreboard is in the /games set, so the two
// feeds are describing the same 99 rows and contradicting each other about 57 of
// them.
//
// The direction replayed is the stale feed arriving late: the schedule sync has
// written a final, and a scoreboard poll then reports the game as not started or
// still being played. That is the direction the guards exist for, and it is the
// one this fixture set can produce. The other direction -- the scoreboard
// finalizing a game before /games has noticed, which is the common case in
// production at five minutes against fifteen -- needs a /games capture taken
// mid-slate, and nothing in the set is one. Noted rather than skipped silently.
func TestTwoFeedsWritingOneGameConverge(t *testing.T) {
	db := testdb.Open(t)

	// An hour before the first scoreboard snapshot, so the instant /games stamps
	// on its results is distinguishable from the one the scoreboard would.
	// Nothing about that ordering is realistic -- it is the fixture set's
	// artefact, not the feed's -- but "kept the first" is only an assertion if
	// the two candidate values differ.
	gamesRunAt := firstSnapshot.Add(-time.Hour)
	seedReference(t, db, gamesRunAt)

	// What /games left behind, for the contested games only.
	type stored struct {
		status      models.GameStatus
		completed   bool
		homeScore   int
		awayScore   int
		finalizedAt *time.Time
	}
	before := make(map[int64]stored)
	contested := snapshotGames(t)
	for _, g := range contested {
		game, result := gameByExternalID(t, db, g.ID)
		if result == nil {
			t.Fatalf("game %d has no result after the /games seed; /games reports every week 1 "+
				"game completed with a score, so the convergence assertions have nothing to contest", g.ID)
		}
		before[g.ID] = stored{game.Status, game.Completed, result.HomeScore, result.AwayScore, result.FinalizedAt}
	}
	if len(before) == 0 {
		t.Fatal("no contested games; every assertion below would pass vacuously")
	}

	// One scoreboard poll, at the instant the snapshot was taken.
	newFeed(t, db, firstSnapshot).scoreboard(1)

	var walkedBack, rescored, refinalized, unfinished int
	for _, g := range contested {
		was := before[g.ID]
		game, result := gameByExternalID(t, db, g.ID)

		// Rule: UpdateReportedStatus only ever moves a game forward. A game
		// stored as final may not be reported back to scheduled or in_progress,
		// because cancelling a bet is gated on the game not being under way and
		// a fallback to scheduled would reopen the refund window.
		if was.status == models.GameStatusFinal && game.Status != models.GameStatusFinal {
			walkedBack++
			t.Errorf("game %d went from %s to %s: the scoreboard reported %q and advancesFrom "+
				"let it through", g.ID, was.status, game.Status, g.Status)
		}
		// Rule: completed is OR'd, never assigned.
		if was.completed && !game.Completed {
			unfinished++
			t.Errorf("game %d was completed and is not any more; the scoreboard reported %q", g.ID, g.Status)
		}

		if result == nil {
			t.Errorf("game %d lost its result row entirely", g.ID)
			continue
		}
		// Rule: a provisional write may not overwrite the score of an already
		// finalized result. EvaluateBetsForGame re-reads this row, so guarding
		// finalized_at without guarding what it certifies is half a rule.
		if was.finalizedAt != nil && (result.HomeScore != was.homeScore || result.AwayScore != was.awayScore) {
			rescored++
			t.Errorf("game %d was finalized at %d-%d and now reads %d-%d: the scoreboard was "+
				"reporting %q with %v-%v",
				g.ID, was.homeScore, was.awayScore, result.HomeScore, result.AwayScore,
				g.Status, points(g.HomeTeam.Points), points(g.AwayTeam.Points))
		}
		// Rule: the first finalized_at is kept, so the feed that sees a game
		// finish first is the one that dates it.
		if was.finalizedAt != nil {
			if result.FinalizedAt == nil {
				t.Errorf("game %d lost its finalized_at", g.ID)
			} else if !result.FinalizedAt.Equal(*was.finalizedAt) {
				refinalized++
				t.Errorf("game %d was finalized at %s and now reads %s",
					g.ID, was.finalizedAt.Format(time.RFC3339Nano), result.FinalizedAt.Format(time.RFC3339Nano))
			}
		}
	}

	// The counts are logged rather than asserted on: they are properties of the
	// capture, and a recapture moves them. What is asserted is that none of the
	// four rules broke on any of them.
	t.Logf("held %d contested games across four write rules (%d walked back, %d unfinished, "+
		"%d rescored, %d refinalized)", len(contested), walkedBack, unfinished, rescored, refinalized)
}

// snapshotGames is the games in the first scoreboard snapshot that /games
// disagrees with -- the ones the scoreboard has not seen finish.
//
// Read from the capture rather than from the database: the disagreement is
// between two recordings, and deriving one side of it from what the sync stored
// would be asserting the sync against itself.
func snapshotGames(t *testing.T) []cfbdata.APIScoreboardGame {
	t.Helper()

	base, _, stop, err := fixtureserver.Listen(fixtures.CFBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	all, err := cfbdata.NewClientAt(base, "").GetScoreboard(context.Background(), "fbs")
	if err != nil {
		t.Fatalf("reading the first scoreboard snapshot: %v", err)
	}

	var out []cfbdata.APIScoreboardGame
	for _, g := range all {
		if g.Status != cfbdata.ScoreboardStatusCompleted {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		t.Fatal("the first scoreboard snapshot reports every game complete, so it contradicts nothing")
	}
	return out
}

func points(p *int) any {
	if p == nil {
		return "none"
	}
	return *p
}
