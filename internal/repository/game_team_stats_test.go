package repository

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
)

// Each row is a reason a game does or does not spend a request, and a predicate that
// wrongly matches nothing returns no error.
func TestGamesAwaitingBox(t *testing.T) {
	db := testdb.Open(t)
	repo := NewGameTeamStatsRepository(db)

	start := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 12, 4, 1, 0, 0, 0, time.UTC)
	finalizedAt := end

	const homeScore, awayScore = 80, 70
	n := func(v int) *int { return &v }

	tests := []struct {
		name        string
		sport       string
		status      models.GameStatus
		scheduledAt time.Time
		noResult    bool
		provisional bool
		home, away  *models.TeamBox
		want        bool
	}{
		{name: "finished with no box", want: true},
		{name: "only the home side stored", home: &models.TeamBox{Points: n(homeScore)}, want: true},
		{name: "only the away side stored", away: &models.TeamBox{Points: n(awayScore)}, want: true},
		{name: "the home side disagrees", home: &models.TeamBox{Points: n(40)}, away: &models.TeamBox{Points: n(awayScore)}, want: true},
		{name: "the away side disagrees", home: &models.TeamBox{Points: n(homeScore)}, away: &models.TeamBox{Points: n(30)}, want: true},
		{name: "the sides are swapped", home: &models.TeamBox{Points: n(awayScore)}, away: &models.TeamBox{Points: n(homeScore)}, want: true},
		{name: "a side with no points", home: &models.TeamBox{}, away: &models.TeamBox{Points: n(awayScore)}, want: true},
		{name: "first instant of the window", scheduledAt: start, want: true},

		{name: "both sides agree", home: &models.TeamBox{Points: n(homeScore)}, away: &models.TeamBox{Points: n(awayScore)}},
		{name: "still being played", status: models.GameStatusInProgress},
		{name: "final status but a provisional score", provisional: true},
		{name: "final status but no score", noResult: true},
		{name: "before the window", scheduledAt: start.Add(-time.Second)},
		{name: "the window's end", scheduledAt: end},
		{name: "football", sport: models.SportFootball},
	}

	wants := make(map[int64]bool, len(tests))
	names := make(map[int64]string, len(tests))
	ids := make(map[int64]uuid.UUID, len(tests))
	for i, tt := range tests {
		sport, status, scheduledAt := tt.sport, tt.status, tt.scheduledAt
		if sport == "" {
			sport = models.SportBasketball
		}
		if status == "" {
			status = models.GameStatusFinal
		}
		if scheduledAt.IsZero() {
			scheduledAt = inWindow
		}

		external := int64(i + 1)
		game := testdb.InsertGame(t, db, models.Game{ExternalID: &external, Sport: sport, Status: status, ScheduledAt: scheduledAt})
		wants[external], names[external], ids[external] = tt.want, tt.name, game.ID

		if !tt.noResult {
			result := models.GameResult{GameID: game.ID, HomeScore: homeScore, AwayScore: awayScore}
			if !tt.provisional {
				result.FinalizedAt = &finalizedAt
			}
			if err := db.Create(&result).Error; err != nil {
				t.Fatalf("inserting result: %v", err)
			}
		}
		for team, box := range map[uuid.UUID]*models.TeamBox{game.HomeTeamID: tt.home, game.AwayTeamID: tt.away} {
			if box == nil {
				continue
			}
			row := models.GameTeamStats{GameID: game.ID, TeamID: team, FetchedAt: end, Box: *box}
			if err := repo.Upsert(&row); err != nil {
				t.Fatalf("storing box: %v", err)
			}
		}
	}

	// A game with no external ID cannot be matched to a feed row, so it must not spend one.
	noExternal := testdb.InsertGame(t, db, models.Game{Sport: models.SportBasketball, Status: models.GameStatusFinal, ScheduledAt: inWindow})
	if err := db.Create(&models.GameResult{GameID: noExternal.ID, HomeScore: homeScore, AwayScore: awayScore, FinalizedAt: &finalizedAt}).Error; err != nil {
		t.Fatalf("inserting result: %v", err)
	}

	got, err := repo.GamesAwaitingBox(start, end)
	if err != nil {
		t.Fatalf("GamesAwaitingBox: %v", err)
	}
	for external, want := range wants {
		id, waiting := got[external]
		if waiting != want {
			t.Errorf("%s: waiting = %v, want %v", names[external], waiting, want)
		}
		if waiting && id != ids[external] {
			t.Errorf("%s: id = %s, want %s", names[external], id, ids[external])
		}
	}
	if _, counted := got[0]; counted {
		t.Error("a game with no external ID counted as waiting")
	}
}
