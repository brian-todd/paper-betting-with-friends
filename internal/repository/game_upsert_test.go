package repository

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
)

// Upsert's status rule sits in the conflict clause, so it only runs against a row
// already stored, and a CASE that picks the wrong arm returns no error.
func TestGameRepositoryUpsertStatus(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameRepository(db)

	tests := []struct {
		name     string
		sport    string
		stored   models.GameStatus
		incoming models.GameStatus
		want     models.GameStatus
	}{
		{"basketball's lagging /games cannot unstart a live game", models.SportBasketball,
			models.GameStatusInProgress, models.GameStatusScheduled, models.GameStatusInProgress},
		{"a basketball game can still start", models.SportBasketball,
			models.GameStatusScheduled, models.GameStatusInProgress, models.GameStatusInProgress},
		{"a live basketball game can still be postponed", models.SportBasketball,
			models.GameStatusInProgress, models.GameStatusPostponed, models.GameStatusPostponed},
		{"a postponed basketball game can be rescheduled", models.SportBasketball,
			models.GameStatusPostponed, models.GameStatusScheduled, models.GameStatusScheduled},
		{"a basketball final stays final", models.SportBasketball,
			models.GameStatusFinal, models.GameStatusScheduled, models.GameStatusFinal},
		// Inferred from the kickoff, so this is a game the feed has moved.
		{"football's moved game goes back to scheduled", models.SportFootball,
			models.GameStatusInProgress, models.GameStatusScheduled, models.GameStatusScheduled},
		{"a football final stays final", models.SportFootball,
			models.GameStatusFinal, models.GameStatusInProgress, models.GameStatusFinal},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			externalID := int64(900_000 + i)
			stored := testdb.InsertGame(t, db, models.Game{
				ExternalID: &externalID,
				Sport:      tt.sport,
				Status:     tt.stored,
			})

			incoming := *stored
			incoming.ID = uuid.Nil
			incoming.Status = tt.incoming
			if err := repo.Upsert(&incoming); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			var got models.Game
			if err := db.First(&got, "id = ?", stored.ID).Error; err != nil {
				t.Fatalf("reading game: %v", err)
			}
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}
