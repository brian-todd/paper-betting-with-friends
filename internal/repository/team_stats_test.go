package repository_test

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

func TestBasketballSplits(t *testing.T) {
	db := testdb.Open(t)
	repo := repository.NewTeamRecordRepository(db)

	home := testdb.InsertTeam(t, db, "")
	away := testdb.InsertTeam(t, db, "")
	at := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	finalized := at.Add(3 * time.Hour)

	play := func(season int, sport string, neutral bool, homeScore, awayScore int, finalizedAt *time.Time) {
		t.Helper()
		at = at.Add(24 * time.Hour)
		game := testdb.InsertGame(t, db, models.Game{
			Sport: sport, Season: season, ScheduledAt: at, NeutralSite: neutral,
			HomeTeamID: home.ID, AwayTeamID: away.ID,
		})
		result := models.GameResult{GameID: game.ID, HomeScore: homeScore, AwayScore: awayScore, FinalizedAt: finalizedAt}
		if err := db.Create(&result).Error; err != nil {
			t.Fatalf("creating result: %v", err)
		}
	}

	play(2026, models.SportBasketball, false, 80, 70, &finalized) // home win, away loss
	play(2026, models.SportBasketball, false, 60, 70, &finalized) // home loss, away win
	play(2026, models.SportBasketball, false, 90, 70, &finalized) // home win, away loss
	play(2026, models.SportBasketball, true, 50, 70, &finalized)  // neutral: neither
	play(2026, models.SportBasketball, false, 10, 70, nil)        // not over
	play(2025, models.SportBasketball, false, 10, 70, &finalized) // another season
	play(2026, models.SportFootball, false, 10, 70, &finalized)   // another sport

	splits, err := repo.BasketballSplits(2026)
	if err != nil {
		t.Fatalf("BasketballSplits: %v", err)
	}
	if got, want := splits[home.ID], (repository.TeamSplit{HomeWins: 2, HomeLosses: 1}); got != want {
		t.Errorf("home team = %+v, want %+v", got, want)
	}
	if got, want := splits[away.ID], (repository.TeamSplit{AwayWins: 1, AwayLosses: 2}); got != want {
		t.Errorf("away team = %+v, want %+v", got, want)
	}
}
