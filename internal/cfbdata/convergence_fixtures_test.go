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
// Three of the four are genuinely exercised here, each confirmed by mutating the
// rule and watching this fail. The fourth -- `completed` being OR'd -- is
// asserted but cannot fail against this fixture set; the comment at the check
// says why.
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

	// What /games left behind, before the scoreboard says anything.
	contested := snapshotGames(t)
	ids := make([]int64, 0, len(contested))
	for _, g := range contested {
		ids = append(ids, g.ID)
	}

	before := snapAll(t, db, ids)
	for _, g := range contested {
		was, ok := before[g.ID]
		if !ok {
			// syncGames skips a game whose team, venue or week it cannot
			// resolve. The scoreboard is FBS and /teams is unfiltered, so this
			// should not happen -- but if it does, the game simply is not part of
			// the comparison rather than a failure.
			continue
		}
		if was.FinalizedAt == nil {
			t.Fatalf("game %d has no finalized result after the /games seed; /games reports "+
				"every week 1 game completed with a score, so there is nothing to contest", g.ID)
		}
		// The seed's clock actually took effect. Without this, fixtureseed.At
		// could be silently ignored -- or wrong by years -- and every assertion
		// below would still pass, because they compare the stored value against
		// itself. It is also this test's premise: "keeps the first finalized_at"
		// only means something if the instant /games stamped differs from the one
		// the scoreboard would.
		if !was.FinalizedAt.Equal(gamesRunAt) {
			t.Fatalf("game %d was finalized at %s, want the seed's clock %s -- fixtureseed.At "+
				"is not reaching the sync, so nothing here is replaying at a chosen instant",
				g.ID, was.FinalizedAt.Format(time.RFC3339Nano), gamesRunAt.Format(time.RFC3339Nano))
		}
	}
	if len(before) == 0 {
		t.Fatal("no contested games; every assertion below would pass vacuously")
	}

	// The whole live Saturday, one snapshot at a time, in the order the feed
	// produced them.
	//
	// Not one arrival: a rule that holds against a single poll but not against
	// the twenty after it is not holding, and production sees all of them. The
	// series is also where the interesting shape is -- a game moves scheduled to
	// in_progress to completed across it, while the /games row it contradicts
	// stays final throughout.
	//
	// Twenty-one, not twenty-six. The last five captures are from 2026-09-12,
	// which the calendar puts in week 2, and week 2's games are not seeded here;
	// they would be counted as unknown rather than refused, so requireAnswered
	// would not notice.
	const saturdaySnapshots = 21

	feed := newFeed(t, db, firstSnapshot)

	var walkedBack, rescored, refinalized, unfinished int

	// previous is each game's status as of the last arrival checked, so a
	// regression anywhere in the series is caught at the step it happens rather
	// than only if it survives to the end.
	previous := make(map[int64]models.GameStatus, len(before))
	for id, was := range before {
		previous[id] = was.Status
	}

	for snapshot := 1; snapshot <= saturdaySnapshots; snapshot++ {
		feed.scoreboard(1)

		now := snapAll(t, db, ids)
		for id, was := range before {
			got, ok := now[id]
			if !ok {
				t.Fatalf("game %d vanished at snapshot %d", id, snapshot)
			}

			// Monotonic across the series. advancesFrom permits scheduled to
			// in_progress to final and nothing backwards, so the stored status
			// may only move forward however many times either feed writes.
			if rank(got.Status) < rank(previous[id]) {
				walkedBack++
				t.Errorf("snapshot %d: game %d went from %s to %s",
					snapshot, id, previous[id], got.Status)
			}
			previous[id] = got.Status

			// Rule: completed is OR'd, never assigned.
			//
			// This one cannot fail against this fixture set, and it is kept
			// labelled rather than deleted. Only Upsert assigns `completed`, and
			// only /games calls Upsert -- the scoreboard goes through
			// UpdateReportedStatus -- so exercising the OR needs a /games
			// response reporting `completed: false` for a game already stored as
			// completed. The week 1 capture reports every game completed, so
			// replacing the OR with a plain assignment writes `true` over `true`
			// and this passes. Confirmed by mutation.
			//
			// It needs the same missing capture as the other convergence
			// direction: a /games response recorded mid-slate. With one, this
			// becomes a real assertion without changing.
			if was.Completed && !got.Completed {
				unfinished++
				t.Errorf("snapshot %d: game %d was completed and is not any more", snapshot, id)
			}

			// Rule: a provisional write may not overwrite the score of an
			// already finalized result. EvaluateBetsForGame re-reads this row, so
			// guarding finalized_at without guarding what it certifies is half a
			// rule.
			if got.HomeScore == nil || got.AwayScore == nil {
				t.Errorf("snapshot %d: game %d lost its score", snapshot, id)
			} else if *got.HomeScore != *was.HomeScore || *got.AwayScore != *was.AwayScore {
				rescored++
				t.Errorf("snapshot %d: game %d was finalized at %d-%d and now reads %d-%d",
					snapshot, id, *was.HomeScore, *was.AwayScore, *got.HomeScore, *got.AwayScore)
			}

			// Rule: the first finalized_at is kept, so the feed that sees a game
			// finish first is the one that dates it.
			if got.FinalizedAt == nil {
				t.Errorf("snapshot %d: game %d lost its finalized_at", snapshot, id)
			} else if !got.FinalizedAt.Equal(*was.FinalizedAt) {
				refinalized++
				t.Errorf("snapshot %d: game %d was finalized at %s and now reads %s",
					snapshot, id, was.FinalizedAt.Format(time.RFC3339Nano),
					got.FinalizedAt.Format(time.RFC3339Nano))
			}
		}

		// Bail rather than repeat the same failure twenty more times.
		if t.Failed() {
			t.Fatalf("stopping at snapshot %d of %d", snapshot, saturdaySnapshots)
		}
	}

	// And the outcome the guards exist for, stated rather than only the path to
	// it: a game /games called final is still final when the series ends.
	after := snapAll(t, db, ids)
	for id, was := range before {
		if was.Status == models.GameStatusFinal && after[id].Status != models.GameStatusFinal {
			walkedBack++
			t.Errorf("game %d ended the series as %s, having been final before it",
				id, after[id].Status)
		}
	}

	// The counts are logged rather than asserted on: they are properties of the
	// capture, and a recapture moves them. What is asserted is that none of the
	// four rules broke on any of them.
	t.Logf("held %d contested games across %d scoreboard arrivals and four write rules "+
		"(%d walked back, %d unfinished, %d rescored, %d refinalized)",
		len(contested), saturdaySnapshots, walkedBack, unfinished, rescored, refinalized)
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

// rank orders the statuses this feed writes, so "forward" is comparable.
// advancesFrom is the rule; this is the same order stated as a number, and the
// two agreeing is what the test relies on -- see the mutation check in the
// commit that added it.
func rank(s models.GameStatus) int {
	switch s {
	case models.GameStatusScheduled:
		return 0
	case models.GameStatusInProgress:
		return 1
	case models.GameStatusFinal:
		return 2
	default:
		// postponed and cancelled are off this line entirely and neither feed
		// here reports them. Treating one as ahead of everything keeps it from
		// reading as a regression if that ever changes.
		return 3
	}
}
