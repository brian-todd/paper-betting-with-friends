package cbbdata_test

import (
	"context"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
	"github.com/shopspring/decimal"
)

// Level 2: the ratings and season figures of a real season, as the feed reported them.
func TestRatingsAndTeamStatsFromARealSeason(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	base, fake, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fake upstream: %v", err)
	}
	defer stop()

	sync := cbbdata.NewSyncService(cbbdata.NewClientAt(base, ""), db)
	sync.SetClock(timeutil.Fixed(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)))

	if err := sync.SyncReferenceForTest(ctx); err != nil {
		t.Fatalf("syncing reference data: %v", err)
	}
	if err := sync.SyncRatings(ctx); err != nil {
		t.Fatalf("SyncRatings: %v", err)
	}
	if err := sync.SyncTeamStats(ctx); err != nil {
		t.Fatalf("SyncTeamStats: %v", err)
	}
	if n := fake.Refusals(); n > 0 {
		t.Fatalf("the fake upstream refused %d requests", n)
	}

	team := func(t *testing.T, name string) models.Team {
		t.Helper()
		var found models.Team
		if err := db.First(&found, "sport = ? AND name = ?", models.SportBasketball, name).Error; err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return found
	}
	equal := func(t *testing.T, label string, got *decimal.Decimal, want string) {
		t.Helper()
		if got == nil || !got.Equal(decimal.RequireFromString(want)) {
			t.Errorf("%s = %v, want %s", label, got, want)
		}
	}

	t.Run("adjusted efficiency keeps its units and ranks", func(t *testing.T) {
		var rating models.TeamRating
		if err := db.First(&rating, "team_id = ? AND season = ? AND source = ?",
			team(t, "Michigan").ID, 2026, models.RatingSourceAdjusted).Error; err != nil {
			t.Fatalf("reading rating: %v", err)
		}
		equal(t, "net", &rating.Overall, "41.9")
		equal(t, "offense", rating.Offense, "130.6")
		equal(t, "defense", rating.Defense, "88.8")
		if *rating.OverallRank != 1 || *rating.OffenseRank != 3 || *rating.DefenseRank != 2 {
			t.Errorf("ranks = %d, %d, %d, want 1, 3, 2", *rating.OverallRank, *rating.OffenseRank, *rating.DefenseRank)
		}
	})

	t.Run("srs stores a headline and nothing else", func(t *testing.T) {
		var rating models.TeamRating
		if err := db.First(&rating, "team_id = ? AND season = ? AND source = ?",
			team(t, "Michigan").ID, 2026, models.RatingSourceSRS).Error; err != nil {
			t.Fatalf("reading rating: %v", err)
		}
		equal(t, "srs", &rating.Overall, "22.6")
		if rating.Offense != nil || rating.OverallRank != nil {
			t.Errorf("srs carries units it does not publish: %+v", rating)
		}
	})

	t.Run("season figures are on one scale and each on its own side", func(t *testing.T) {
		var stats models.TeamSeasonStats
		if err := db.First(&stats, "team_id = ? AND season = ?", team(t, "Duke").ID, 2026).Error; err != nil {
			t.Fatalf("reading stats: %v", err)
		}
		if stats.Games != 38 {
			t.Errorf("games = %d, want 38", stats.Games)
		}
		equal(t, "pace", stats.Pace, "66.5")
		equal(t, "offense", stats.OffenseRating, "122.9")
		equal(t, "defense", stats.DefenseRating, "95.6")
		equal(t, "eFG", stats.OffenseEffectiveFGPct, "56.7")
		equal(t, "opponent eFG", stats.DefenseEffectiveFGPct, "46.2")
		equal(t, "offensive rebounds", stats.OffenseReboundPct, "30.8")
		equal(t, "opponent offensive rebounds", stats.DefenseReboundPct, "32.1")
		equal(t, "free throw rate", stats.OffenseFreeThrowRate, "38.2")
		equal(t, "opponent free throw rate", stats.DefenseFreeThrowRate, "22.8")
		equal(t, "threes", stats.OffenseThreePct, "34.7")

		// The feed says 0.16: 405 turnovers on 2,523 possessions.
		if got := stats.OffenseTurnoverPct.Round(1); !got.Equal(decimal.RequireFromString("16.1")) {
			t.Errorf("turnover rate = %s, want 16.1", got)
		}
	})

	t.Run("no percentage on any team is a fraction or out of range", func(t *testing.T) {
		var rows []models.TeamSeasonStats
		if err := db.Where("season = ?", 2026).Find(&rows).Error; err != nil {
			t.Fatalf("reading stats: %v", err)
		}
		if len(rows) < 700 {
			t.Fatalf("read %d rows", len(rows))
		}
		one, hundred := decimal.NewFromInt(1), decimal.NewFromInt(100)
		for _, row := range rows {
			if row.Games < 10 || row.OffenseTurnoverPct == nil {
				continue
			}
			if row.OffenseTurnoverPct.LessThan(one) || row.OffenseTurnoverPct.GreaterThan(hundred) {
				t.Errorf("team %s: turnover rate %s", row.TeamID, row.OffenseTurnoverPct)
			}
			if row.OffenseEffectiveFGPct != nil && row.OffenseEffectiveFGPct.LessThan(one) {
				t.Errorf("team %s: eFG %s", row.TeamID, row.OffenseEffectiveFGPct)
			}
		}
	})

	t.Run("the record is the feed's", func(t *testing.T) {
		var record models.TeamRecord
		if err := db.First(&record, "team_id = ? AND season = ?", team(t, "Duke").ID, 2026).Error; err != nil {
			t.Fatalf("reading record: %v", err)
		}
		if record.Wins != 35 || record.Losses != 3 || record.Games != 38 {
			t.Errorf("record = %s over %d", record.Overall(), record.Games)
		}
	})
}
