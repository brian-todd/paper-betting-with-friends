package cfbdata_test

// Level 2 is fixture to database: the real client decoding real recorded bodies
// into the real SQL, with nothing stubbed but the socket. It is where the write
// rules documented in AGENTS.md get their first test, and those rules exist
// because two football feeds write the same row and disagree for minutes at a
// time -- a disagreement no hand-built fixture reproduces, because whoever
// builds it already knows which of the two is right.
//
// These tests are in package cfbdata_test rather than cfbdata: fixtureseed
// imports cfbdata, so an internal test file importing fixtureseed would be an
// import cycle.

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
)

// The weeks the committed captures cover, and what each is good for.
//
// Weeks 1 and 2 were captured on 2026-09-20, by which time every game in them
// had been played: /games reports completed on all of them, with real scores.
// That makes them the feed-says-final half of a convergence test and useless
// for anything about an unplayed game.
//
// Week 6 was captured the same day and had not been played: 275 games, none
// completed, no points, kickoffs spanning 2026-10-07 to 2026-10-11, and 39
// games the feed has not scheduled yet. It is the only capture in which the
// clock decides anything.
// fixtureYear is declared in client_fixtures_test.go, which is the same package.
const (
	playedWeek = 1
	futureWeek = 6
)

// Instants inside the scoreboard series. The names are the file names.
var (
	// The first snapshot of the live Saturday: 42 games already complete, 16
	// being played, 41 not started.
	firstSnapshot = time.Date(2026, 9, 5, 20, 49, 9, 0, time.UTC)
	// The last: 63 complete, 26 being played, 10 not started.
	lastSnapshot = time.Date(2026, 9, 6, 1, 43, 28, 0, time.UTC)
)

// A feed is one process talking to the fake upstream: a sync service, its own
// fixture server, and a clock.
//
// Its own server matters. The scoreboard route holds twenty-six captures and
// the server replays them oldest-first, one per request, so the sequence
// position is process state. Two feeds sharing a server would consume each
// other's snapshots, and a test asserting on "the third snapshot" would depend
// on how many requests every other test had made first.
type feed struct {
	t    *testing.T
	sync *cfbdata.SyncService
	fake *fixtureserver.Server
}

// newFeed wires a sync service to the fixtures at a chosen instant. The options
// go to the fixture server, for a test replaying a tree of its own.
func newFeed(t *testing.T, db *gorm.DB, at time.Time, opts ...fixtureserver.Option) *feed {
	t.Helper()

	base, fake, stop, err := fixtureserver.Listen(fixtures.CFBD, opts...)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	t.Cleanup(stop)

	sync := cfbdata.NewSyncService(cfbdata.NewClientAt(base, ""), db)
	sync.SetClock(timeutil.Fixed(at))

	return &feed{t: t, sync: sync, fake: fake}
}

// at moves this feed's clock.
func (f *feed) at(when time.Time) *feed {
	f.sync.SetClock(timeutil.Fixed(when))
	return f
}

// games replays /games for a week.
func (f *feed) games(week int) {
	f.t.Helper()
	if err := f.sync.SyncGames(context.Background(), fixtureYear, &week, nil); err != nil {
		f.t.Fatalf("syncing /games week %d: %v", week, err)
	}
	f.requireAnswered()
}

// scoreboard replays the next n snapshots of the live Saturday, in order.
//
// Replaying the run rather than jumping to a chosen snapshot is deliberate:
// production sees every snapshot, and a rule that holds against one arrival but
// not against the seventeen before it is not holding.
func (f *feed) scoreboard(n int) {
	f.t.Helper()
	for i := range n {
		if err := f.sync.SyncScoreboard(context.Background(), []string{"fbs"}); err != nil {
			f.t.Fatalf("syncing /scoreboard snapshot %d: %v", i+1, err)
		}
	}
	f.requireAnswered()
}

// requireAnswered fails on a request the fixture set had no answer for.
//
// Both syncs here tolerate a failed fetch in places -- logging and carrying on
// -- so a missing capture is otherwise a quieter pass than a real one.
func (f *feed) requireAnswered() {
	f.t.Helper()
	if n := f.fake.Refusals(); n > 0 {
		f.t.Fatalf("the fake upstream refused %d of %d requests; the server logged the paths",
			n, len(f.fake.Requests()))
	}
}

// seedReference puts venues, teams and the calendar in the transaction, which
// every /games run needs: syncGames skips a game whose team, venue or week it
// cannot find, logs a warning, and returns nil.
//
// It goes through the whole week-1 seed rather than the three endpoints alone
// because syncVenues, syncTeams and syncCalendar are unexported and exporting
// them for a test would be API nobody else wants. The week-1 games it also
// writes are what the convergence tests assert on.
func seedReference(t *testing.T, db *gorm.DB, at time.Time) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := fixtureseed.Football(ctx, db, fixtureYear, playedWeek, fixtureseed.At(at)); err != nil {
		t.Fatalf("seeding reference data: %v", err)
	}
}

// statuses counts the stored status of every football game in a week.
func statuses(t *testing.T, db *gorm.DB, week int) map[models.GameStatus]int {
	t.Helper()

	type row struct {
		Status models.GameStatus
		N      int
	}
	var rows []row
	err := db.Model(&models.Game{}).
		Select("games.status, count(*) as n").
		Joins("JOIN weeks ON weeks.id = games.week_id").
		Where("games.sport = ? AND weeks.season = ? AND weeks.number = ? AND weeks.season_type = ?",
			models.SportFootball, fixtureYear, week, models.SeasonTypeRegular).
		Group("games.status").
		Scan(&rows).Error
	if err != nil {
		t.Fatalf("counting statuses: %v", err)
	}

	out := make(map[models.GameStatus]int, len(rows))
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out
}

// findGame reads back one game with its result, or nil when the sync never wrote
// it.
//
// A missing game is not necessarily a failure: syncGames skips one whose home
// team, away team or week it cannot resolve, and the /teams capture does not
// cover every school the smaller divisions play. A caller iterating games out of
// a capture has to tolerate that, or a recapture turns a pass into a confusing
// fatal about a row nothing promised.
func findGame(db *gorm.DB, externalID int64) (*models.Game, *models.GameResult) {
	game, err := repository.NewGameRepository(db).FindByExternalID(externalID, models.SportFootball)
	if err != nil {
		return nil, nil
	}
	result, err := repository.NewGameResultRepository(db).FindByGameID(game.ID)
	if err != nil {
		return game, nil
	}
	return game, result
}

// A snap is one game's stored state, for a caller comparing many games across
// many arrivals.
type snap struct {
	Status      models.GameStatus
	Completed   bool
	HomeScore   *int
	AwayScore   *int
	FinalizedAt *time.Time
}

// snapAll reads the stored state of every football game named by external ID, in
// one query.
//
// One query rather than a lookup per game because the convergence test compares
// 57 games after each of 21 scoreboard arrivals, and a pair of round trips per
// game per arrival is most of that test's runtime. The scores are nullable here
// where models.GameResult has them as ints: this is a LEFT JOIN, so a game with
// no result row yet reads as nil rather than as 0-0.
func snapAll(t *testing.T, db *gorm.DB, externalIDs []int64) map[int64]snap {
	t.Helper()

	type row struct {
		ExternalID  int64
		Status      models.GameStatus
		Completed   bool
		HomeScore   *int
		AwayScore   *int
		FinalizedAt *time.Time
	}
	var rows []row
	err := db.Model(&models.Game{}).
		Select("games.external_id, games.status, games.completed, "+
			"game_results.home_score, game_results.away_score, game_results.finalized_at").
		Joins("LEFT JOIN game_results ON game_results.game_id = games.id").
		Where("games.sport = ? AND games.external_id IN ?", models.SportFootball, externalIDs).
		Scan(&rows).Error
	if err != nil {
		t.Fatalf("reading stored state for %d games: %v", len(externalIDs), err)
	}

	out := make(map[int64]snap, len(rows))
	for _, r := range rows {
		out[r.ExternalID] = snap{r.Status, r.Completed, r.HomeScore, r.AwayScore, r.FinalizedAt}
	}
	return out
}

// gameByExternalID is findGame for a caller that has already established the row
// must be there.
func gameByExternalID(t *testing.T, db *gorm.DB, externalID int64) (*models.Game, *models.GameResult) {
	t.Helper()

	game, err := repository.NewGameRepository(db).FindByExternalID(externalID, models.SportFootball)
	if err != nil {
		t.Fatalf("finding game %d: %v", externalID, err)
	}
	result, err := repository.NewGameResultRepository(db).FindByGameID(game.ID)
	if err != nil {
		return game, nil
	}
	return game, result
}
