package cfbdata

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// applyScoreboardGame is where the scoreboard's reading of a game meets the
// three rows it is spread across, and the kickoff correction is the one part of
// it with a condition in front of the call rather than inside the repository.
// It needs a database because every write it makes is a repository call and the
// repositories are concrete types -- which is also what lets a test reach it,
// since NewSyncService takes a *gorm.DB and this path never touches the client.
func TestApplyScoreboardGameCorrectsTheKickoff(t *testing.T) {
	db := testdb.Open(t)
	sync := NewSyncService(nil, db)

	scheduled := time.Date(2026, 10, 3, 19, 0, 0, 0, time.UTC)
	moved := time.Date(2026, 10, 3, 20, 30, 0, 0, time.UTC)

	tests := []struct {
		name         string
		startDate    time.Time
		startTimeTBD bool
		want         time.Time
	}{
		{
			name:      "a moved kickoff is written",
			startDate: moved,
			want:      moved,
		},
		{
			// The feed sends a placeholder instant -- midnight of the day it
			// expects the game on -- rather than omitting the field, so a game
			// whose time is genuinely unknown arrives looking like one kicking
			// off at 00:00. Storing that would close betting at midnight and
			// tell the scoreboard's own cadence a game was being played.
			name:         "a time the feed has not settled on is not",
			startDate:    time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
			startTimeTBD: true,
			want:         scheduled,
		},
		{
			// A different absence, and startTimeTBD does not mark it: an
			// omitted or null startDate unmarshals to the zero time with the
			// flag left false. Year 1 is a kickoff long past, so writing it
			// would close betting on the game and freeze the bets already on
			// it -- not editable, not cancellable -- until /games rewrote the
			// row, which after the games/lines split is up to six hours.
			name:      "nor one the feed did not send at all",
			startDate: time.Time{},
			want:      scheduled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			game := testdb.InsertGame(t, db, models.Game{ScheduledAt: scheduled})

			err := sync.applyScoreboardGame(game.ID, APIScoreboardGame{
				ID:           4242,
				StartDate:    tt.startDate,
				StartTimeTBD: tt.startTimeTBD,
				Status:       ScoreboardStatusScheduled,
			})
			if err != nil {
				t.Fatalf("applyScoreboardGame: %v", err)
			}

			var stored models.Game
			if err := db.First(&stored, "id = ?", game.ID).Error; err != nil {
				t.Fatalf("reloading game: %v", err)
			}
			if !stored.ScheduledAt.Equal(tt.want) {
				t.Errorf("scheduled_at = %s, want %s", stored.ScheduledAt, tt.want)
			}
		})
	}
}

// The correction runs ahead of the status write, which decides more than the
// order of two statements: the guard reads the status still stored, so a game
// the scoreboard is finalizing in this very run is not yet final when its
// kickoff is judged, and the correction lands. A game that was already final
// before the run is refused.
//
// That asymmetry is the intended reading rather than an accident of ordering. A
// game that has just been played has an actual kickoff, and the feed reporting
// it complete is reporting the best value anything will ever have for it --
// better than the schedule it was drawn up with. It is pinned here because
// moving the correction below the status write would reverse it silently, with
// no test failing and nothing visible but a games grid sorting slightly wrong.
func TestApplyScoreboardGameCorrectsAKickoffAsTheGameFinishes(t *testing.T) {
	db := testdb.Open(t)
	sync := NewSyncService(nil, db)

	scheduled := time.Date(2026, 10, 3, 19, 0, 0, 0, time.UTC)
	actual := time.Date(2026, 10, 3, 20, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		// status is what the row holds before the run.
		status models.GameStatus
		want   time.Time
	}{
		{
			name:   "a game going final in this run still takes the correction",
			status: models.GameStatusInProgress,
			want:   actual,
		},
		{
			name:   "one already final keeps the time it was played at",
			status: models.GameStatusFinal,
			want:   scheduled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			game := testdb.InsertGame(t, db, models.Game{
				ScheduledAt: scheduled,
				Status:      tt.status,
			})

			points := 21
			g := APIScoreboardGame{
				ID:        4242,
				StartDate: actual,
				Status:    ScoreboardStatusCompleted,
			}
			g.HomeTeam.Points = &points
			g.AwayTeam.Points = &points

			if err := sync.applyScoreboardGame(game.ID, g); err != nil {
				t.Fatalf("applyScoreboardGame: %v", err)
			}

			var stored models.Game
			if err := db.First(&stored, "id = ?", game.ID).Error; err != nil {
				t.Fatalf("reloading game: %v", err)
			}
			if !stored.ScheduledAt.Equal(tt.want) {
				t.Errorf("scheduled_at = %s, want %s", stored.ScheduledAt, tt.want)
			}
			// The point of the ordering is that the status write still happens.
			if stored.Status != models.GameStatusFinal {
				t.Errorf("status = %s, want %s", stored.Status, models.GameStatusFinal)
			}
		})
	}
}
