package cbbdata

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

func TestMapGameStatus(t *testing.T) {
	tests := []struct {
		status string
		want   models.GameStatus
	}{
		{"scheduled", models.GameStatusScheduled},
		{"in_progress", models.GameStatusInProgress},
		{"final", models.GameStatusFinal},
		{"postponed", models.GameStatusPostponed},
		{"cancelled", models.GameStatusCancelled},
		{"canceled", models.GameStatusCancelled},
		{"FINAL", models.GameStatusFinal},
		{"something-new", models.GameStatusScheduled},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := mapGameStatus(tt.status); got != tt.want {
				t.Errorf("mapGameStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestGameResultFrom(t *testing.T) {
	gameID := uuid.New()
	now := time.Date(2026, 8, 28, 22, 30, 0, 0, time.UTC)
	home, away := 68, 71

	tests := []struct {
		name          string
		status        models.GameStatus
		wantFinalized bool
	}{
		{"final game is finalized", models.GameStatusFinal, true},
		// Settlement keys off FinalizedAt, so a game still being played must
		// leave it nil however lopsided the score looks.
		{"in-progress game is not finalized", models.GameStatusInProgress, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gameResultFrom(gameID, APIGame{
				HomePoints:       &home,
				AwayPoints:       &away,
				HomePeriodPoints: []int{30, 38},
				AwayPeriodPoints: []int{35, 36},
			}, tt.status, now)

			if got.GameID != gameID {
				t.Errorf("GameID = %v, want %v", got.GameID, gameID)
			}
			if got.HomeScore != home || got.AwayScore != away {
				t.Errorf("score = %d-%d, want %d-%d", got.HomeScore, got.AwayScore, home, away)
			}
			if got.IsFinal() != tt.wantFinalized {
				t.Errorf("IsFinal() = %v, want %v", got.IsFinal(), tt.wantFinalized)
			}
			// Basketball stores halves rather than quarters, but through the
			// same columns.
			if len(got.HomeLineScores) != 2 || len(got.AwayLineScores) != 2 {
				t.Errorf("period points = %v / %v, want both preserved", got.HomeLineScores, got.AwayLineScores)
			}
		})
	}
}
