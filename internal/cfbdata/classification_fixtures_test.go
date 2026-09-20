package cfbdata_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// The classification case bug, at the level that would have caught it.
//
// CFB_SCOREBOARD_CLASSIFICATIONS=FBS used to fetch the right division from the
// API while matching no row in the teams table, because the column holds what
// CFBD reports and CFBD reports "fbs". The cadence is decided by matching that
// list against that column, so the scoreboard polled hourly through every slate
// of the season while every run logged success. normalizeClassifications is the
// fix; this is the test that the fix reaches the database, which is the only
// place the mismatch ever showed.
//
// It needs real teams rows. testdb.InsertTeam hard-codes "fbs" and a comment
// says that is the case CFBD uses -- correct, and a comment, so nothing fails if
// the feed changes it and nothing would have failed if the comment had been
// wrong from the start. Here the rows come from the /teams capture.
func TestScoreboardClassificationsMatchTheStoredCase(t *testing.T) {
	db := testdb.Open(t)

	// Mid-slate on the Saturday of week 6, the only captured week with games
	// that are neither played nor final -- scoreboardScoped excludes final
	// games, so a week whose every row is completed reports nothing active
	// whatever the classifications say.
	midSlate := time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)
	seedReference(t, db, midSlate)
	newFeed(t, db, midSlate).games(futureWeek)

	games := repository.NewGameRepository(db)
	logger := slog.Default()

	t.Run("the feed's own case is what is stored", func(t *testing.T) {
		type row struct {
			Classification string
			N              int
		}
		var rows []row
		if err := db.Model(&models.Team{}).
			Select("classification, count(*) as n").
			Where("sport = ? AND classification IS NOT NULL", models.SportFootball).
			Group("classification").Scan(&rows).Error; err != nil {
			t.Fatalf("counting classifications: %v", err)
		}
		if len(rows) == 0 {
			t.Fatal("no classified teams were seeded")
		}

		seen := make(map[string]int, len(rows))
		for _, r := range rows {
			seen[r.Classification] = r.N
			if r.Classification != strings.ToLower(r.Classification) {
				t.Errorf("classification %q is stored in mixed case; every predicate that "+
					"compares against this column folds to lower", r.Classification)
			}
		}
		// The four divisions, because the spread is what the capture rule exists
		// to keep and what several past bugs needed in order to be visible.
		for _, want := range []string{"fbs", "fcs", "ii", "iii"} {
			if seen[want] == 0 {
				t.Errorf("no %q teams stored; the fixture set has lost its division spread", want)
			}
		}
		t.Logf("stored classifications: %v", seen)
	})

	t.Run("a mixed-case list matches nothing unnormalized", func(t *testing.T) {
		// The bug itself, reproduced. This is the call ResolveScoreboardState
		// would make if it did not fold the case first, and it has to find
		// nothing for the next subtest to be saying anything.
		active, err := games.HasActiveGames([]string{"FBS"}, midSlate, 6*time.Hour)
		if err != nil {
			t.Fatalf("HasActiveGames: %v", err)
		}
		if active {
			t.Skip("the stored classification now matches FBS unfolded, so this bug is " +
				"no longer reachable and the subtest below proves less than it claims")
		}
	})

	t.Run("either spelling resolves the same state", func(t *testing.T) {
		lower := cfbdata.ResolveScoreboardState(games, logger, midSlate, []string{"fbs"})
		upper := cfbdata.ResolveScoreboardState(games, logger, midSlate, []string{"FBS"})
		mixed := cfbdata.ResolveScoreboardState(games, logger, midSlate, []string{" Fbs "})

		if !lower.Active {
			t.Fatal("no FBS game reads as active mid-slate on the captured Saturday, so an " +
				"agreement between spellings here would be an agreement on nothing")
		}
		for name, got := range map[string]cfbdata.ScoreboardState{"FBS": upper, " Fbs ": mixed} {
			if got.Active != lower.Active {
				t.Errorf("%q resolved Active=%v, fbs resolved %v: the configured list is not "+
					"reaching the column in the case it is stored in", name, got.Active, lower.Active)
			}
		}
	})

	t.Run("the resolved state is what moves the cadence", func(t *testing.T) {
		// The consequence, so this test says why the case mattered rather than
		// only that it did. A state that never reads active is not a degraded
		// cadence but a stuck one.
		loc := time.UTC
		live := cfbdata.ScoreboardDelay(midSlate, loc,
			cfbdata.ResolveScoreboardState(games, logger, midSlate, []string{"FBS"}))

		quiet := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC) // the Tuesday after
		idle := cfbdata.ScoreboardDelay(quiet, loc,
			cfbdata.ResolveScoreboardState(games, logger, quiet, []string{"FBS"}))

		if live >= idle {
			t.Errorf("mid-slate delay %s is not shorter than the quiet-day delay %s; the "+
				"scoreboard is not speeding up for the games it exists to follow", live, idle)
		}
		t.Logf("mid-slate %s, quiet day %s", live, idle)
	})
}
