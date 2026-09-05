package repository

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// A reported status may only move a game forward. Two football feeds write the
// row and disagree for minutes at a time, and cancelling a bet is gated on the
// game not being in progress -- so a status that could fall back to scheduled
// would reopen the refund window on a game already being played.
func TestAdvancesFromOnlyEverMovesAGameForward(t *testing.T) {
	order := map[models.GameStatus]int{
		models.GameStatusScheduled:  0,
		models.GameStatusInProgress: 1,
		models.GameStatusFinal:      2,
	}

	for to, froms := range advancesFrom {
		toRank, ok := order[to]
		if !ok {
			t.Errorf("status %q is written but has no place in the order", to)
			continue
		}
		for _, from := range froms {
			fromRank, ok := order[from]
			if !ok {
				t.Errorf("status %q is advanced from but has no place in the order", from)
				continue
			}
			if fromRank >= toRank {
				t.Errorf("%q may replace %q, which is not forward", to, from)
			}
		}
	}

	// Nothing advances *to* scheduled, so it is absent and never written. A
	// game is scheduled by default; the feed reporting it again says nothing.
	if froms, ok := advancesFrom[models.GameStatusScheduled]; ok {
		t.Errorf("scheduled is writable from %v; it must not be", froms)
	}

	// The scoreboard never reports either of these, and a game parked on one by
	// hand must not be dragged off it by the feed.
	for _, status := range []models.GameStatus{models.GameStatusPostponed, models.GameStatusCancelled} {
		if _, ok := advancesFrom[status]; ok {
			t.Errorf("%q is written by the scoreboard, which never reports it", status)
		}
		for to, froms := range advancesFrom {
			for _, from := range froms {
				if from == status {
					t.Errorf("%q may be replaced by %q", status, to)
				}
			}
		}
	}
}
