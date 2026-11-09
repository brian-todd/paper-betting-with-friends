package games

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/shopspring/decimal"
)

// The projection is drawn before tip-off and not after, while the rating rows it is
// built from stay on the page either way. Gated on the kickoff, so a game the sync
// still calls scheduled loses it the moment it tips, and on the status, so a game
// the scoreboard has called live loses it under a kickoff still in the future.
func TestGameDetailProjectsABasketballGameBeforeTipOffOnly(t *testing.T) {
	db := testdb.Open(t)
	const season = 2026
	tip := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	fetched := tip.Add(-6 * time.Hour)

	ratings := repository.NewTeamRatingRepository(db)
	stats := repository.NewTeamSeasonStatsRepository(db)
	rate := func(offense, defense, pace string) *models.Team {
		t.Helper()
		team := testdb.InsertTeam(t, db, "")
		o, d, p := decimal.RequireFromString(offense), decimal.RequireFromString(defense), decimal.RequireFromString(pace)
		if err := ratings.Upsert(&models.TeamRating{
			TeamID: team.ID, Season: season, Source: models.RatingSourceAdjusted, FetchedAt: fetched,
			Overall: o.Sub(d), Offense: &o, Defense: &d,
		}); err != nil {
			t.Fatalf("storing rating: %v", err)
		}
		if err := stats.Upsert(&models.TeamSeasonStats{TeamID: team.ID, Season: season, FetchedAt: fetched, Games: 10, Pace: &p}); err != nil {
			t.Fatalf("storing season: %v", err)
		}
		return team
	}
	home := rate("110", "95", "70")
	away := rate("100", "105", "70")

	game := testdb.InsertGame(t, db, models.Game{
		Sport: models.SportBasketball, Season: season, ScheduledAt: tip,
		HomeTeamID: home.ID, AwayTeamID: away.ID,
	})
	// DraftKings's total is stored first, so FanDuel's is the newer and leads the
	// list. The line beside the projection is still DraftKings's, to go with its
	// spread.
	for _, book := range []models.OddsSource{models.OddsSourceDraftKings, models.OddsSourceFanDuel} {
		total := "140.5"
		if book == models.OddsSourceFanDuel {
			total = "138.5"
		}
		if err := db.Create(&models.OverUnderOdds{
			GameID: game.ID, Source: book, Total: decimal.RequireFromString(total),
			OverOdds: decimal.RequireFromString("-110"), UnderOdds: decimal.RequireFromString("-110"),
		}).Error; err != nil {
			t.Fatalf("storing total: %v", err)
		}
	}
	homeSpread := decimal.RequireFromString("-7.5")
	if err := db.Create(&models.SpreadOdds{
		GameID: game.ID, Source: models.OddsSourceDraftKings,
		HomeSpread: homeSpread, AwaySpread: homeSpread.Neg(),
		HomeOdds: decimal.RequireFromString("-110"), AwayOdds: decimal.RequireFromString("-110"),
	}).Error; err != nil {
		t.Fatalf("storing spread: %v", err)
	}

	svc := NewService(db, time.UTC)
	detailAt := func(now time.Time) *GameDetail {
		t.Helper()
		svc.SetClock(func() time.Time { return now })
		detail, err := svc.GetGameDetail(game.ID)
		if err != nil {
			t.Fatalf("GetGameDetail() error: %v", err)
		}
		if detail.Matchup == nil || !detail.Matchup.HasRating(models.RatingSourceAdjusted) {
			t.Fatal("the matchup lost its Adjusted row")
		}
		return detail
	}

	// The national average is these two teams: 105 per 100 at 70 possessions. Home
	// scores 110 x 105 / 105 x 0.7 = 77 and away 100 x 95 / 105 x 0.7 = 63.33, then
	// 1.5 each way for home court: 78.5 and 61.83, 140.33 between them.
	before := detailAt(tip.Add(-time.Hour)).Matchup.ScoreProjection
	if before == nil {
		t.Fatal("no projection an hour before tip-off")
	}
	if before.Home != 78 || before.Away != 62 {
		t.Errorf("projection = %d-%d, want 78-62", before.Home, before.Away)
	}
	if before.Spread == nil || !before.Spread.HomeSpread.Equal(homeSpread) {
		t.Errorf("projection carried spread %+v, want the posted %s", before.Spread, homeSpread)
	}
	if before.OverUnder == nil || before.OverUnder.Source != models.OddsSourceDraftKings {
		t.Errorf("projection carried total %+v, want DraftKings's to go with its spread", before.OverUnder)
	}

	if after := detailAt(tip).Matchup.ScoreProjection; after != nil {
		t.Errorf("projection at tip-off = %+v, want none", after)
	}

	if err := db.Model(game).Update("status", models.GameStatusInProgress).Error; err != nil {
		t.Fatalf("marking the game live: %v", err)
	}
	if live := detailAt(tip.Add(-time.Hour)).Matchup.ScoreProjection; live != nil {
		t.Errorf("projection for a live game with a future kickoff = %+v, want none", live)
	}
}
