package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"gorm.io/gorm"
)

// An ordinary Saturday evening kickoff. Both queries take the instant to judge
// against as a parameter, so nothing here depends on when the test runs and a
// fixed date keeps a failure message reproducible -- testdb.Open is what
// guarantees these rows are the only ones in the table.
var testKickoff = time.Date(2026, 10, 3, 19, 0, 0, 0, time.UTC)

// HasActiveGames decides how often the scoreboard is polled, and every way it
// can be wrong is quiet. Matching too much is a job that polls every five
// minutes through the off-season; matching too little is a job that sits at the
// hourly floor through a slate. Both report success on every run.
func TestGameRepositoryHasActiveGames(t *testing.T) {
	const window = 6 * time.Hour
	now := testKickoff.Add(30 * time.Minute)

	tests := []struct {
		name string
		// classification is the home team's, which is what the query joins to.
		classification string
		game           models.Game
		query          []string
		want           bool
	}{
		{
			name:           "a game past kickoff is being played",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff},
			want:           true,
		},
		{
			name:           "so is one the feed has called in progress",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff, Status: models.GameStatusInProgress},
			want:           true,
		},
		{
			name:           "a finished game is not",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff, Status: models.GameStatusFinal},
			want:           false,
		},
		{
			name:           "nor a cancelled one",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff, Status: models.GameStatusCancelled},
			want:           false,
		},
		{
			name:           "nor a postponed one",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff, Status: models.GameStatusPostponed},
			want:           false,
		},
		{
			name:           "a kickoff still ahead is not",
			classification: "fbs",
			game:           models.Game{ScheduledAt: now.Add(time.Minute)},
			want:           false,
		},
		{
			name:           "a kickoff exactly now is",
			classification: "fbs",
			game:           models.Game{ScheduledAt: now},
			want:           true,
		},
		{
			name:           "a game older than the window has stopped counting",
			classification: "fbs",
			game:           models.Game{ScheduledAt: now.Add(-window - time.Minute)},
			want:           false,
		},
		{
			name:           "and the window edge itself is past it",
			classification: "fbs",
			game:           models.Game{ScheduledAt: now.Add(-window)},
			want:           false,
		},
		{
			// The load-bearing one. /games and /teams are fetched unfiltered,
			// so the table holds divisions the scoreboard never polls and whose
			// status is inferred from the clock. Scoped by sport instead of by
			// division this predicate would be true most of a weekend and the
			// whole cadence a no-op that still looked like it worked.
			name:           "a game in a division we do not poll is not ours",
			classification: "fcs",
			game:           models.Game{ScheduledAt: testKickoff},
			want:           false,
		},
		{
			name:           "neither is basketball",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff, Sport: models.SportBasketball},
			want:           false,
		},
		{
			// Classification is stored in the case CFBD reports. A configured
			// value that has not been folded matches no team at all, which is
			// not a degraded cadence but a stuck one -- see
			// cfbdata.normalizeClassifications, which exists for this.
			name:           "and the comparison is case sensitive",
			classification: "fbs",
			game:           models.Game{ScheduledAt: testKickoff},
			query:          []string{"FBS"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testdb.Open(t)
			repo := NewGameRepository(db)

			tt.game.HomeTeamID = testdb.InsertTeam(t, db, tt.classification).ID
			testdb.InsertGame(t, db, tt.game)

			query := tt.query
			if query == nil {
				query = []string{"fbs"}
			}

			got, err := repo.HasActiveGames(query, now, window)
			if err != nil {
				t.Fatalf("HasActiveGames: %v", err)
			}
			if got != tt.want {
				t.Errorf("HasActiveGames = %v, want %v", got, tt.want)
			}
		})
	}
}

// An empty table is the off-season, and the gap between a week's last game and
// the next week's schedule arriving. Both wait at the idle rate.
func TestGameRepositoryHasActiveGamesFindsNothingInAnEmptyWindow(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameRepository(db)

	active, err := repo.HasActiveGames([]string{"fbs"}, testKickoff, 6*time.Hour)
	if err != nil {
		t.Fatalf("HasActiveGames: %v", err)
	}
	if active {
		t.Error("HasActiveGames = true with no games in the window")
	}
}

// NextKickoff is what lets the idle poll wake for a slate instead of sleeping
// through its first quarter, so returning too late a time costs freshness and
// returning too early costs calls.
func TestGameRepositoryNextKickoff(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameRepository(db)

	fbs := testdb.InsertTeam(t, db, "fbs").ID
	fcs := testdb.InsertTeam(t, db, "fcs").ID

	// Everything that should lose to the 19:00 kickoff, for a different reason
	// each: already under way, not a division we poll, and over.
	testdb.InsertGame(t, db, models.Game{HomeTeamID: fbs, ScheduledAt: testKickoff.Add(-2 * time.Hour)})
	testdb.InsertGame(t, db, models.Game{HomeTeamID: fcs, ScheduledAt: testKickoff.Add(-time.Hour)})
	testdb.InsertGame(t, db, models.Game{HomeTeamID: fbs, ScheduledAt: testKickoff.Add(time.Hour), Status: models.GameStatusFinal})
	testdb.InsertGame(t, db, models.Game{HomeTeamID: fbs, ScheduledAt: testKickoff.Add(4 * time.Hour)})
	testdb.InsertGame(t, db, models.Game{HomeTeamID: fbs, ScheduledAt: testKickoff})

	got, err := repo.NextKickoff([]string{"fbs"}, testKickoff.Add(-3*time.Hour))
	if err != nil {
		t.Fatalf("NextKickoff: %v", err)
	}

	// Compared with Equal rather than ==: the driver hands back a time in Local
	// and the column keeps microseconds, so neither the location nor the
	// monotonic reading survives the round trip.
	if want := testKickoff.Add(-2 * time.Hour); !got.Equal(want) {
		t.Errorf("NextKickoff = %s, want %s", got.UTC(), want.UTC())
	}
}

func TestGameRepositoryNextKickoffReportsNoneLeft(t *testing.T) {
	// Asked about this instant, with the one game in the table placed relative
	// to it.
	after := testKickoff.Add(-30 * time.Minute)

	tests := []struct {
		name           string
		classification string
		status         models.GameStatus
		kickoff        time.Time
	}{
		{
			name:           "every kickoff is behind us",
			classification: "fbs",
			status:         models.GameStatusScheduled,
			kickoff:        after.Add(-time.Hour),
		},
		{
			name:           "the only one left is in a division we do not poll",
			classification: "fcs",
			status:         models.GameStatusScheduled,
			kickoff:        after.Add(time.Hour),
		},
		{
			name:           "the only one left is already final",
			classification: "fbs",
			status:         models.GameStatusFinal,
			kickoff:        after.Add(time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testdb.Open(t)
			repo := NewGameRepository(db)

			testdb.InsertGame(t, db, models.Game{
				HomeTeamID:  testdb.InsertTeam(t, db, tt.classification).ID,
				ScheduledAt: tt.kickoff,
				Status:      tt.status,
			})

			_, err := repo.NextKickoff([]string{"fbs"}, after)
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Errorf("NextKickoff error = %v, want %v", err, gorm.ErrRecordNotFound)
			}
		})
	}
}
