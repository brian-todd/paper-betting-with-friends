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
