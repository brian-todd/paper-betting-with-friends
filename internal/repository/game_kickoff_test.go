package repository

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"gorm.io/gorm"
)

// The kickoff a game was scheduled for, and the hour it moved to.
var (
	originalKickoff = time.Date(2026, 10, 3, 19, 0, 0, 0, time.UTC)
	movedKickoff    = time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
)

// UpdateScheduledAt is a conditional update, so the whole of it is in the WHERE
// clause -- and a WHERE clause that matches nothing looks exactly like a WHERE
// clause that matched and wrote the value it was given. Neither returns an
// error, which is why these run against a real database rather than asserting
// on the SQL.
func TestGameRepositoryUpdateScheduledAt(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameRepository(db)

	tests := []struct {
		name   string
		status models.GameStatus
		to     time.Time
		want   time.Time
	}{
		{
			name:   "a scheduled game's kickoff moves",
			status: models.GameStatusScheduled,
			to:     movedKickoff,
			want:   movedKickoff,
		},
		{
			// The case the guard exists for. /games reports no status, it
			// infers one five minutes past the kickoff it last saw, so a game
			// delayed an hour is already stored as in_progress before the
			// correction arrives -- and a guard on "scheduled" would refuse it.
			name:   "so does one /games has already inferred is under way",
			status: models.GameStatusInProgress,
			to:     movedKickoff,
			want:   movedKickoff,
		},
		{
			// Rescheduling is the normal end of a postponement, and nothing
			// about the row says when it will be played until the feed says so.
			name:   "and a postponed one, which is how it gets replayed",
			status: models.GameStatusPostponed,
			to:     movedKickoff,
			want:   movedKickoff,
		},
		{
			// The only status with no kickoff left to move.
			name:   "a finished game keeps the time it was played at",
			status: models.GameStatusFinal,
			to:     movedKickoff,
			want:   originalKickoff,
		},
		{
			// Moving earlier is the case neither feed catches quickly, and the
			// direction is nothing to the query -- worth pinning so it stays
			// that way.
			name:   "a kickoff can move earlier as well as later",
			status: models.GameStatusScheduled,
			to:     originalKickoff.Add(-2 * time.Hour),
			want:   originalKickoff.Add(-2 * time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			game := testdb.InsertGame(t, db, models.Game{
				ScheduledAt: originalKickoff,
				Status:      tt.status,
			})

			if err := repo.UpdateScheduledAt(game.ID, tt.to); err != nil {
				t.Fatalf("UpdateScheduledAt: %v", err)
			}

			if got := reloadKickoff(t, db, game.ID); !got.Equal(tt.want) {
				t.Errorf("scheduled_at = %s, want %s", got, tt.want)
			}
		})
	}
}

// The inequality on scheduled_at is the difference between a write per changed
// kickoff and a write per game per run, five minutes apart through a slate.
// Nothing downstream would notice, which is why it needs asserting: updated_at
// is the only evidence the row was touched.
func TestGameRepositoryUpdateScheduledAtSkipsAnUnchangedKickoff(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameRepository(db)

	game := testdb.InsertGame(t, db, models.Game{ScheduledAt: originalKickoff})

	// A marker far enough back that any write at all is unmistakable.
	marker := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Model(&models.Game{}).Where("id = ?", game.ID).
		UpdateColumn("updated_at", marker).Error; err != nil {
		t.Fatalf("setting updated_at: %v", err)
	}

	if err := repo.UpdateScheduledAt(game.ID, originalKickoff); err != nil {
		t.Fatalf("UpdateScheduledAt: %v", err)
	}

	var updatedAt time.Time
	if err := db.Model(&models.Game{}).Where("id = ?", game.ID).
		Pluck("updated_at", &updatedAt).Error; err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}
	if !updatedAt.Equal(marker) {
		t.Errorf("updated_at = %s, want it untouched at %s", updatedAt, marker)
	}
}

// The scoreboard walks a whole division per run, so a predicate that reached
// past its own row would move every kickoff in the table to whichever game the
// feed listed last.
func TestGameRepositoryUpdateScheduledAtLeavesOtherGamesAlone(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameRepository(db)

	target := testdb.InsertGame(t, db, models.Game{ScheduledAt: originalKickoff})
	other := testdb.InsertGame(t, db, models.Game{ScheduledAt: originalKickoff})

	if err := repo.UpdateScheduledAt(target.ID, movedKickoff); err != nil {
		t.Fatalf("UpdateScheduledAt: %v", err)
	}

	if got := reloadKickoff(t, db, other.ID); !got.Equal(originalKickoff) {
		t.Errorf("the other game's scheduled_at = %s, want %s", got, originalKickoff)
	}
}

// reloadKickoff reads a game's stored kickoff back.
func reloadKickoff(t *testing.T, db *gorm.DB, id any) time.Time {
	t.Helper()

	var game models.Game
	if err := db.First(&game, "id = ?", id).Error; err != nil {
		t.Fatalf("reloading game: %v", err)
	}
	return game.ScheduledAt
}
