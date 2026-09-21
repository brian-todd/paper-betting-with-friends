package admin

import (
	"context"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
)

// The sync page reports two facts about "now" -- the current week and whether
// the scoreboard has anything live -- and until this it could only report them
// about the real now.
//
// That is worth a test rather than a comment because the page's stated job is to
// show what the jobs acted on, and half of it already went through a clocked
// service: GetCurrentWeek reads games.Service's clock, while the scoreboard state
// read the wall clock. A page rendered at a fixture instant answered one question
// from the fixture and the other from today.
func TestHealthResolvesTheScoreboardAgainstItsOwnClock(t *testing.T) {
	db := testdb.Open(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Week 6 is the captured week that had not been played, so its games are
	// still ahead of a clock placed before them and under way at one placed
	// mid-slate. Weeks 1 and 2 are all final, and scoreboardScoped excludes
	// final games, so neither could ever read as live.
	const futureWeek = 6
	midSlate := time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)

	// Two calls: week 6 has a /games capture and no rankings or lines, so the
	// reference data comes from the week that has the whole set.
	if _, err := fixtureseed.Football(ctx, db, fixtureseed.DefaultYear, fixtureseed.DefaultWeek,
		fixtureseed.At(midSlate)); err != nil {
		t.Fatalf("seeding reference data: %v", err)
	}
	if _, err := fixtureseed.FootballGames(ctx, db, fixtureseed.DefaultYear, futureWeek,
		fixtureseed.At(midSlate)); err != nil {
		t.Fatalf("seeding week %d: %v", futureWeek, err)
	}

	svc := &Service{
		statsRepo: repository.NewStatsRepository(db),
		gameRepo:  repository.NewGameRepository(db),
		games:     games.NewService(db, time.UTC),
		sched:     scheduler.New(nil),
		cfg: &config.Config{
			// The scoreboard block is gated on a key being configured, so
			// without one this asserts nothing.
			CFBDataAPIKey:                "fixture",
			CFBScoreboardClassifications: []string{"fbs"},
		},
	}

	svc.SetClock(timeutil.Fixed(midSlate))
	live, err := svc.Health()
	if err != nil {
		t.Fatalf("Health() mid-slate: %v", err)
	}

	// The Tuesday after, when nothing in the captured week is being played.
	quiet := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	svc.SetClock(timeutil.Fixed(quiet))
	idle, err := svc.Health()
	if err != nil {
		t.Fatalf("Health() on the quiet day: %v", err)
	}

	if !live.ScoreboardLive {
		t.Error("the page reports nothing live mid-slate on the captured Saturday")
	}
	if idle.ScoreboardLive {
		t.Error("the page reports something live on the Tuesday after the captured week")
	}
	// Both readings coming out the same is the failure this exists to catch: it
	// is what a wall clock here looks like, whatever instant the page is asked
	// about.
	if live.ScoreboardLive == idle.ScoreboardLive {
		t.Fatal("the reported scoreboard state does not move with the clock, so Health is not " +
			"reading the one it was given")
	}

	if live.Counts.Games == 0 {
		t.Error("no games counted, so the state above was resolved over an empty table")
	}
}
