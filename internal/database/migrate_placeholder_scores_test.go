package database_test

import (
	"io/fs"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/migrations"
	"github.com/google/uuid"
)

// Migration 000025 deletes rows from a table that also holds real scores, so what
// it leaves behind matters more than what it removes. A migration runs once, on
// real data, with nothing to catch a predicate that was too wide.
func TestPlaceholderScoreMigrationRemovesOnlyPlaceholders(t *testing.T) {
	db := testdb.Open(t)

	sql, err := fs.ReadFile(migrations.FS, "000025_remove_placeholder_basketball_scores.up.sql")
	if err != nil {
		t.Fatalf("reading the migration: %v", err)
	}

	finalizedAt := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		sport      string
		status     models.GameStatus
		home, away int
		finalized  *time.Time
		kept       bool
	}{
		{"a scheduled basketball game's 0-0", models.SportBasketball, models.GameStatusScheduled, 0, 0, nil, false},
		{"a postponed basketball game's 0-0", models.SportBasketball, models.GameStatusPostponed, 0, 0, nil, false},
		{"a cancelled basketball game's 0-0", models.SportBasketball, models.GameStatusCancelled, 0, 0, nil, false},
		{"a basketball game under way at 0-0", models.SportBasketball, models.GameStatusInProgress, 0, 0, nil, true},
		{"a final basketball score", models.SportBasketball, models.GameStatusFinal, 70, 64, &finalizedAt, true},
		{"a scheduled basketball game with real points", models.SportBasketball, models.GameStatusScheduled, 3, 1, nil, true},
		{"a finalized result on a game that reads scheduled", models.SportBasketball, models.GameStatusScheduled, 0, 0, &finalizedAt, true},
		{"a football game's 0-0", models.SportFootball, models.GameStatusScheduled, 0, 0, nil, true},
	}

	ids := make(map[string]uuid.UUID, len(tests))
	for _, tt := range tests {
		game := testdb.InsertGame(t, db, models.Game{Sport: tt.sport, Status: tt.status, ScheduledAt: finalizedAt})
		ids[tt.name] = game.ID
		result := &models.GameResult{GameID: game.ID, HomeScore: tt.home, AwayScore: tt.away, FinalizedAt: tt.finalized}
		if err := db.Create(result).Error; err != nil {
			t.Fatalf("creating result for %q: %v", tt.name, err)
		}
	}

	if err := db.Exec(string(sql)).Error; err != nil {
		t.Fatalf("running the migration: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n int64
			if err := db.Model(&models.GameResult{}).Where("game_id = ?", ids[tt.name]).Count(&n).Error; err != nil {
				t.Fatalf("counting: %v", err)
			}
			if kept := n == 1; kept != tt.kept {
				t.Errorf("kept = %v, want %v", kept, tt.kept)
			}
		})
	}
}
