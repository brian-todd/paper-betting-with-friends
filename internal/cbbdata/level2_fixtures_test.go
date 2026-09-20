package cbbdata_test

import (
	"context"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// Basketball at level 2. It is the thinner feed -- four endpoints to football's
// fifteen -- but it earns its place here: it is the one that reports a real
// status rather than inferring one, and the only one that writes `postponed`.
//
// This test could not be written until the seed could run inside a transaction,
// which it could not, and the reason was not the fixtures. See the seed's own
// package documentation.
func TestBasketballSeedsARealSeason(t *testing.T) {
	db := testdb.Open(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// A date inside the captured season, so the statuses the feed reports are
	// replayed against a clock that agrees with them.
	seededAt := time.Date(2026, 1, 15, 3, 0, 0, 0, time.UTC)
	counts, err := fixtureseed.Basketball(ctx, db, fixtureseed.DefaultSeason, fixtureseed.At(seededAt))
	if err != nil {
		t.Fatalf("seeding basketball from fixtures: %v", err)
	}
	t.Logf("seeded %v", counts)

	t.Run("every team in the capture is written", func(t *testing.T) {
		// The /teams capture holds 1,519 teams. 107 of them share a truncated
		// abbreviation with another, and the teams table has a unique index on
		// (abbreviation, sport) that Upsert does not arbitrate on -- so each
		// collision was a unique-violation the sync logged and continued past,
		// and the team was never written at all. Every game involving one was
		// then skipped by syncGames as "team not found", also logged, also
		// continued past. Two silent losses stacked, and a seed that exited 0.
		var teams int64
		if err := db.Model(&models.Team{}).
			Where("sport = ?", models.SportBasketball).Count(&teams).Error; err != nil {
			t.Fatalf("counting teams: %v", err)
		}
		if want := int64(1519); teams != want {
			t.Errorf("stored %d basketball teams, want %d from the capture: %d are missing",
				teams, want, want-teams)
		}
	})

	t.Run("two teams sharing a truncated abbreviation both survive", func(t *testing.T) {
		// UAlbany and Albright College both abbreviate to "ALB", and Colorado
		// State and Chico State both to "CSU". Named rather than counted, so a
		// regression says which pair it lost.
		for _, pair := range [][2]string{
			{"UAlbany", "Albright College"},
			{"Colorado State", "Chico State"},
			{"DePaul", "Depauw"},
		} {
			for _, name := range pair {
				var n int64
				if err := db.Model(&models.Team{}).
					Where("sport = ? AND name = ?", models.SportBasketball, name).
					Count(&n).Error; err != nil {
					t.Fatalf("counting %q: %v", name, err)
				}
				if n == 0 {
					t.Errorf("%q was not written; it collides on abbreviation with %q",
						name, pair[0]+"/"+pair[1])
				}
			}
		}
	})

	t.Run("the feed's own statuses are stored, not inferred", func(t *testing.T) {
		// Football infers a status from the clock for every division the
		// scoreboard does not poll. Basketball does not have to: the feed says.
		// So a captured season should hold more than one status, and in
		// particular should hold the finished ones -- if everything reads
		// scheduled, the status is being dropped on the way in.
		type row struct {
			Status models.GameStatus
			N      int
		}
		var rows []row
		if err := db.Model(&models.Game{}).
			Select("status, count(*) as n").
			Where("sport = ?", models.SportBasketball).
			Group("status").Scan(&rows).Error; err != nil {
			t.Fatalf("counting statuses: %v", err)
		}
		seen := make(map[models.GameStatus]int, len(rows))
		for _, r := range rows {
			seen[r.Status] = r.N
		}
		t.Logf("basketball statuses: %v", seen)

		if len(seen) < 2 {
			t.Errorf("a whole captured season holds only %v; the feed reports a real status "+
				"per game and more than one of them should survive the write", seen)
		}
		if seen[models.GameStatusFinal] == 0 {
			t.Error("no basketball game reads as final across a whole captured season")
		}
	})
}
