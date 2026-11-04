package cbbdata_test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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

	t.Run("the seed fetches the season's ranks", func(t *testing.T) {
		var ranks int64
		if err := db.Model(&models.PollRank{}).Where("season = ?", fixtureseed.DefaultSeason).Count(&ranks).Error; err != nil {
			t.Fatalf("counting ranks: %v", err)
		}
		if ranks == 0 {
			t.Error("a seeded season holds no ranks")
		}
	})

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
		// The expected number comes from the capture rather than a literal, so a
		// recapture that adds or drops a school moves the expectation with it
		// instead of failing for the wrong reason.
		want := int64(len(capturedTeams(t)))
		if teams != want {
			t.Errorf("stored %d basketball teams, want the %d in the /teams capture: %d missing",
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

	// Line history rides on this seed rather than a seed of its own: a
	// basketball season is the most expensive thing in the suite, and it has
	// already put every captured quote through the recorder.
	var gameIDs []uuid.UUID
	if err := db.Model(&models.Game{}).Where("sport = ?", models.SportBasketball).
		Pluck("id", &gameIDs).Error; err != nil {
		t.Fatalf("reading basketball game IDs: %v", err)
	}

	t.Run("the seed records every line's first value once", func(t *testing.T) {
		series := testdb.RequireHistoryMatchesOdds(t, db, gameIDs)
		if len(series) == 0 {
			t.Fatal("the season stored no sportsbook lines, so the history has nothing to agree with")
		}
		if movements := testdb.CountOddsMovements(t, db, gameIDs, time.Time{}); movements != len(series) {
			t.Errorf("recorded %d movements for %d series; a seed is every series' first sync",
				movements, len(series))
		}
	})

	t.Run("replaying a month's lines records nothing", func(t *testing.T) {
		// Every basketball number arrives as a float and goes through
		// NewFromFloat -- the moneyline included, which football reads as an
		// int. A value that compared unequal to its own stored form would
		// record every book on every run of the incremental sync.
		base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
		if err != nil {
			t.Fatalf("starting the fake upstream: %v", err)
		}
		defer stop()

		replayedAt := seededAt.Add(15 * time.Minute)
		sync := cbbdata.NewSyncService(cbbdata.NewClientAt(base, ""), db)
		sync.SetClock(timeutil.Fixed(replayedAt))

		// April, the smallest captured month: 13 games and 41 lines. The code
		// path is the same for every month, and a replay costs a round trip
		// per quote -- March's 637 games added 7.5s to what is already the
		// suite's longest test.
		season := fixtureseed.DefaultSeason
		start, end := "2026-04-01T00:00:00.000Z", "2026-05-01T00:00:00.000Z"
		if err := sync.SyncLinesForTest(ctx, cbbdata.LineQueryOpts{
			Season: &season, StartDateRange: &start, EndDateRange: &end,
		}); err != nil {
			t.Fatalf("replaying April's lines: %v", err)
		}
		if n := fake.Refusals(); n > 0 {
			t.Fatalf("the fake upstream refused %d requests; the replay asked for a month nobody captured", n)
		}

		if n := testdb.CountOddsMovements(t, db, gameIDs, replayedAt); n != 0 {
			t.Errorf("recorded %d movements replaying lines already stored", n)
		}

		// Only April's games were written, so only they can have drifted.
		var april []uuid.UUID
		if err := db.Model(&models.Game{}).
			Where("sport = ? AND scheduled_at >= ? AND scheduled_at < ?", models.SportBasketball, start, end).
			Pluck("id", &april).Error; err != nil {
			t.Fatalf("reading April's games: %v", err)
		}
		if series := testdb.RequireHistoryMatchesOdds(t, db, april); len(series) == 0 {
			t.Error("April holds no stored lines, so the replay compared nothing")
		}
	})
}

// An unplayed day, recorded the morning before it: 66 games, none played, and the
// feed reporting every one of them 0-0. The sync used to store that as a score on
// every upcoming game.
//
// Like football's week 6, this capture is worth more stale than fresh. Re-recording
// it after 2026-11-05 returns a played day and removes the premise, which the first
// assertion here is what notices.
func TestAnUnplayedDayStoresNoScores(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	sync := cbbdata.NewSyncService(cbbdata.NewClientAt(base, ""), db)
	sync.SetClock(timeutil.Fixed(time.Date(2026, 11, 4, 13, 6, 20, 0, time.UTC)))

	if err := sync.SyncReferenceForTest(ctx); err != nil {
		t.Fatalf("syncing reference data: %v", err)
	}
	start, end := "2026-11-05T05:00:00.000Z", "2026-11-06T05:00:00.000Z"
	if err := sync.SyncGamesForTest(ctx, cbbdata.GameQueryOpts{StartDateRange: &start, EndDateRange: &end}); err != nil {
		t.Fatalf("syncing the day: %v", err)
	}
	if n := fake.Refusals(); n > 0 {
		t.Fatalf("the fake upstream refused %d requests", n)
	}

	var gameIDs []uuid.UUID
	if err := db.Model(&models.Game{}).
		Where("sport = ? AND status = ?", models.SportBasketball, models.GameStatusScheduled).
		Pluck("id", &gameIDs).Error; err != nil {
		t.Fatalf("reading games: %v", err)
	}
	if len(gameIDs) == 0 {
		t.Fatal("no scheduled games were stored; this capture only proves anything about unplayed games")
	}
	t.Logf("%d scheduled games stored", len(gameIDs))

	var results int64
	if err := db.Model(&models.GameResult{}).Where("game_id IN ?", gameIDs).Count(&results).Error; err != nil {
		t.Fatalf("counting results: %v", err)
	}
	if results != 0 {
		t.Errorf("stored %d results for games nobody has played", results)
	}
}

// The real 2025-26 polls, whose quirks are the reason ranks are read by date: a
// third of the rows rank nobody, AP skipped a week, and neither poll publishes for
// most of the tournament.
func TestRankingsFromARealSeason(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	sync := cbbdata.NewSyncService(cbbdata.NewClientAt(base, ""), db)
	sync.SetClock(timeutil.Fixed(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)))

	if err := sync.SyncReferenceForTest(ctx); err != nil {
		t.Fatalf("syncing reference data: %v", err)
	}
	if err := sync.SyncRankings(ctx); err != nil {
		t.Fatalf("SyncRankings: %v", err)
	}
	if n := fake.Refusals(); n > 0 {
		t.Fatalf("the fake upstream refused %d requests", n)
	}

	repo := repository.NewPollRankRepository(db)
	at := func(year int, month time.Month, day int) map[uuid.UUID]int {
		t.Helper()
		ranks, err := repo.AsOf(2026, models.PollAP, time.Date(year, month, day, 23, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("AsOf: %v", err)
		}
		return ranks
	}

	t.Run("a poll ranks 25 teams, not everyone receiving votes", func(t *testing.T) {
		if got := len(at(2026, time.January, 6)); got != 25 {
			t.Errorf("%d ranked teams, want 25", got)
		}
	})

	t.Run("the week AP skipped keeps the poll before it", func(t *testing.T) {
		// AP has no rows dated 2025-12-29.
		before, during := at(2025, time.December, 23), at(2025, time.December, 30)
		if len(during) != 25 || len(before) != len(during) {
			t.Fatalf("%d ranked before the gap, %d during it", len(before), len(during))
		}
		for team, rank := range before {
			if during[team] != rank {
				t.Errorf("a rank moved across a week with no poll: %d to %d", rank, during[team])
			}
		}
	})

	t.Run("the tournament keeps the last poll before it", func(t *testing.T) {
		if got := len(at(2026, time.March, 28)); got != 25 {
			t.Errorf("%d ranked teams during the tournament, want 25", got)
		}
	})

	t.Run("a second run stores nothing new", func(t *testing.T) {
		var before, after int64
		db.Model(&models.PollRank{}).Count(&before)
		if err := sync.SyncRankings(ctx); err != nil {
			t.Fatalf("SyncRankings: %v", err)
		}
		db.Model(&models.PollRank{}).Count(&after)
		if before == 0 || before != after {
			t.Errorf("rows went from %d to %d", before, after)
		}
	})
}

// Real lineups through the real sync. Duke's two games share eight units, which is
// what adding a season up depends on: the expected totals are summed from the raw
// responses here rather than through anything the sync uses, and the ratings are
// held against the numbers the feed itself reports.
func TestLineupsFromRealGames(t *testing.T) {
	const duke1, duke2, overtime, lowMajor = 214309, 214348, 209761, 215243

	db := testdb.Open(t)
	ctx := context.Background()

	base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	client := cbbdata.NewClientAt(base, "")
	sync := cbbdata.NewSyncService(client, db)
	sync.SetClock(timeutil.Fixed(now))

	if err := sync.SyncReferenceForTest(ctx); err != nil {
		t.Fatalf("syncing reference data: %v", err)
	}

	gameIDs := make(map[int64]uuid.UUID)
	for i, external := range []int64{duke1, duke2, overtime, lowMajor} {
		game := testdb.InsertGame(t, db, models.Game{
			ExternalID:  &external,
			Sport:       models.SportBasketball,
			Season:      2026,
			Status:      models.GameStatusFinal,
			ScheduledAt: now.Add(-time.Duration(24*(i+1)) * time.Hour),
		})
		gameIDs[external] = game.ID
		line := &models.OverUnderOdds{
			GameID: game.ID, Source: models.OddsSourceDraftKings, Total: decimal.NewFromInt(140),
			OverOdds: decimal.NewFromInt(-110), UnderOdds: decimal.NewFromInt(-110),
		}
		if err := db.Create(line).Error; err != nil {
			t.Fatalf("creating line: %v", err)
		}
	}

	// Two runs, because a run is capped below the four games waiting.
	for range 2 {
		if err := sync.SyncLineups(ctx); err != nil {
			t.Fatalf("SyncLineups: %v", err)
		}
	}
	if n := fake.Refusals(); n > 0 {
		t.Fatalf("the fake upstream refused %d requests", n)
	}

	var duke models.Team
	if err := db.First(&duke, "sport = ? AND name = ?", models.SportBasketball, "Duke").Error; err != nil {
		t.Fatalf("reading Duke: %v", err)
	}
	repo := repository.NewGameLineupRepository(db)

	first, err := client.GetGameLineups(ctx, duke1)
	if err != nil {
		t.Fatalf("reading capture: %v", err)
	}
	second, err := client.GetGameLineups(ctx, duke2)
	if err != nil {
		t.Fatalf("reading capture: %v", err)
	}

	t.Run("a stored rating matches the feed's own", func(t *testing.T) {
		stored, err := repo.TopForGame(gameIDs[duke1], duke.ID, 100)
		if err != nil {
			t.Fatalf("TopForGame: %v", err)
		}
		if len(stored) == 0 {
			t.Fatal("no Duke units stored")
		}
		byPlayers := make(map[string]float64)
		for _, unit := range first {
			if unit.TeamID == *duke.ExternalID {
				byPlayers[unit.IDHash] = unit.NetRating
			}
		}
		if len(stored) != len(byPlayers) {
			t.Errorf("stored %d units, the capture holds %d", len(stored), len(byPlayers))
		}

		feed := make(map[string]float64, len(first))
		for _, unit := range first {
			names := models.LineupTotals{Athletes: unit.Athletes}.Players()
			feed[names] = unit.NetRating
		}
		for _, unit := range stored {
			net, ok := unit.NetRating()
			want, inFeed := feed[unit.Players()]
			if !inFeed {
				t.Errorf("stored a unit the feed does not list: %s", unit.Players())
				continue
			}
			if ok && math.Abs(net-want) > 0.11 {
				t.Errorf("%s: net %.2f, feed says %.1f", unit.Players(), net, want)
			}
		}
	})

	t.Run("a season adds a unit up across games", func(t *testing.T) {
		want := make(map[string]int)
		for _, game := range [][]cbbdata.APIGameLineup{first, second} {
			for _, unit := range game {
				if unit.TeamID == *duke.ExternalID {
					want[models.LineupTotals{Athletes: unit.Athletes}.Players()] += int(unit.TotalSeconds)
				}
			}
		}

		totals, err := repo.TopForSeason(duke.ID, 2026, now, 100)
		if err != nil {
			t.Fatalf("TopForSeason: %v", err)
		}
		if len(totals) != len(want) {
			t.Fatalf("%d units across two games, want %d", len(totals), len(want))
		}
		for i, unit := range totals {
			if unit.Seconds != want[unit.Players()] {
				t.Errorf("%s: %d seconds, want %d", unit.Players(), unit.Seconds, want[unit.Players()])
			}
			if i > 0 && unit.Seconds > totals[i-1].Seconds {
				t.Error("units are not ordered by minutes")
			}
		}
	})

	t.Run("an opponent the feed does not cover has no units", func(t *testing.T) {
		var teams []uuid.UUID
		if err := db.Model(&models.GameLineup{}).Where("game_id = ?", gameIDs[lowMajor]).
			Distinct().Pluck("team_id", &teams).Error; err != nil {
			t.Fatalf("reading teams: %v", err)
		}
		if len(teams) != 1 {
			t.Errorf("%d teams with units, want the one Division I side", len(teams))
		}
	})
}

// capturedTeams is the /teams capture, read through the real client.
//
// The count of these is what a seed has to write. Deriving it here rather than
// writing 1,519 into the test keeps the expectation and the fixture in step, and
// the number is the whole assertion: the bug this guards against was 107 of them
// silently not arriving.
func capturedTeams(t *testing.T) []cbbdata.APITeam {
	t.Helper()

	base, _, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	teams, err := cbbdata.NewClientAt(base, "").GetTeams(context.Background())
	if err != nil {
		t.Fatalf("reading the /teams capture: %v", err)
	}
	if len(teams) == 0 {
		t.Fatal("the /teams capture is empty, so the count assertion would pass vacuously")
	}
	return teams
}

// The real team lists of both feeds. The cases are the ones a name match got wrong or
// could not reach, and the count is what says the picture holds across Division I.
func TestBasketballLogosFromRealTeams(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	if _, err := fixtureseed.Football(ctx, db, 2026, 1); err != nil {
		t.Fatalf("seeding football: %v", err)
	}

	base, _, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()
	sync := cbbdata.NewSyncService(cbbdata.NewClientAt(base, ""), db)
	if err := sync.SyncReferenceForTest(ctx); err != nil {
		t.Fatalf("syncing reference data: %v", err)
	}

	var teams []models.Team
	if err := db.Where("sport = ?", models.SportBasketball).Find(&teams).Error; err != nil {
		t.Fatalf("reading teams: %v", err)
	}
	logo := make(map[string]string, len(teams))
	withLogo, divisionOne, divisionOneWithLogo := 0, 0, 0
	for _, team := range teams {
		if team.LogoURL != nil {
			logo[team.Name] = *team.LogoURL
			withLogo++
		}
		if team.Conference != "Independent" {
			divisionOne++
			if team.LogoURL != nil {
				divisionOneWithLogo++
			}
		}
	}

	const football = "https://cdn.collegefootballdata.com/logos/"
	for _, tt := range []struct{ school, wantPrefix string }{
		{"Duke", football},
		{"Charlotte", football},                    // a football stub shares the name
		{"Troy", football},                         // and here
		{"Saint Mary's", "https://a.espncdn.com/"}, // football's row has no logo
		{"St. Francis (PA)", football},             // football says Saint Francis, and no conference
		{"VCU", "https://a.espncdn.com/"},          // football has no logo
		{"DePaul", "https://a.espncdn.com/"},
	} {
		if !strings.HasPrefix(logo[tt.school], tt.wantPrefix) {
			t.Errorf("%s logo = %q, want it from %s", tt.school, logo[tt.school], tt.wantPrefix)
		}
	}
	if got := logo["Culver-Stockton"]; got != "" {
		t.Errorf("Culver-Stockton logo = %q, want none: ESPN has no such image", got)
	}
	if divisionOne < 360 || divisionOneWithLogo != divisionOne {
		t.Errorf("%d of %d conference teams have a logo, want all of them", divisionOneWithLogo, divisionOne)
	}
	t.Logf("%d of %d teams have a logo", withLogo, len(teams))
}
