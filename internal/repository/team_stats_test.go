package repository_test

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/shopspring/decimal"
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

// The baseline averages over teams holding both an adjusted offense and a pace. The
// season figures also cover teams outside Division I, which have no rating, and
// counting their pace is what once cost the projected total seven points.
func TestAdjustedBaselineAveragesTheRatedTeams(t *testing.T) {
	db := testdb.Open(t)
	repo := repository.NewTeamRatingRepository(db)
	stats := repository.NewTeamSeasonStatsRepository(db)
	const season = 2026
	fetched := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	if _, ok, err := repo.AdjustedBaseline(season); err != nil || ok {
		t.Fatalf("AdjustedBaseline() on an empty season = ok %v, err %v; want false, nil", ok, err)
	}

	team := func(offense, pace string) {
		t.Helper()
		id := testdb.InsertTeam(t, db, "").ID
		if offense != "" {
			o := decimal.RequireFromString(offense)
			if err := repo.Upsert(&models.TeamRating{
				TeamID: id, Season: season, Source: models.RatingSourceAdjusted, FetchedAt: fetched,
				Overall: decimal.Zero, Offense: &o,
			}); err != nil {
				t.Fatalf("storing rating: %v", err)
			}
		}
		p := decimal.RequireFromString(pace)
		if err := stats.Upsert(&models.TeamSeasonStats{TeamID: id, Season: season, FetchedAt: fetched, Games: 10, Pace: &p}); err != nil {
			t.Fatalf("storing season: %v", err)
		}
	}
	team("110", "70")
	team("100", "66")
	team("", "90") // outside Division I: a pace and no rating

	// An SRS row has no offense and is a different source.
	other := testdb.InsertTeam(t, db, "").ID
	if err := repo.Upsert(&models.TeamRating{
		TeamID: other, Season: season, Source: models.RatingSourceSRS, FetchedAt: fetched, Overall: decimal.RequireFromString("5"),
	}); err != nil {
		t.Fatalf("storing srs: %v", err)
	}

	got, ok, err := repo.AdjustedBaseline(season)
	if err != nil || !ok {
		t.Fatalf("AdjustedBaseline() = ok %v, err %v", ok, err)
	}
	if !got.Efficiency.Equal(decimal.RequireFromString("105")) || !got.Pace.Equal(decimal.RequireFromString("68")) || got.Teams != 2 {
		t.Errorf("AdjustedBaseline() = %s per 100 at %s pace over %d teams, want 105 at 68 over 2", got.Efficiency, got.Pace, got.Teams)
	}

	if _, ok, _ := repo.AdjustedBaseline(season + 1); ok {
		t.Error("another season's rows were averaged")
	}
}
