package repository

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// FindLeagueBets flattens three tables through a UNION whose column names come
// from its first branch alone, and then joins five tables onto it. Either half
// failing reads as a league page with an empty weekly breakdown, so it runs
// against a real database with one bet of each type.
func TestBetRecordRepositoryFindLeagueBets(t *testing.T) {
	db := testdb.Open(t)
	dec := decimal.RequireFromString

	create := func(what string, row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("creating %s: %v", what, err)
		}
	}

	user := &models.User{Username: "bettor-" + uuid.NewString()[:8], PasswordHash: "unused"}
	create("user", user)
	league := &models.League{Name: "League " + uuid.NewString()[:8], CreatedBy: user.ID}
	create("league", league)
	other := &models.League{Name: "Other " + uuid.NewString()[:8], CreatedBy: user.ID}
	create("other league", other)

	week := &models.Week{
		Season: 2026, Number: 6, SeasonType: models.SeasonTypeRegular,
		StartDate: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	}
	create("week", week)
	kickoff := time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)
	game := testdb.InsertGame(t, db, models.Game{ScheduledAt: kickoff, WeekID: &week.ID, Season: 2026})

	var home, away models.Team
	if err := db.First(&home, "id = ?", game.HomeTeamID).Error; err != nil {
		t.Fatalf("reading home team: %v", err)
	}
	if err := db.First(&away, "id = ?", game.AwayTeamID).Error; err != nil {
		t.Fatalf("reading away team: %v", err)
	}

	spreadOdds := &models.SpreadOdds{GameID: game.ID, Source: models.OddsSourceDraftKings,
		HomeSpread: dec("-7"), AwaySpread: dec("7"), HomeOdds: dec("-110"), AwayOdds: dec("-110")}
	create("spread odds", spreadOdds)
	moneyLineOdds := &models.MoneyLineOdds{GameID: game.ID, Source: models.OddsSourceDraftKings,
		HomeOdds: dec("-180"), AwayOdds: dec("150")}
	create("money line odds", moneyLineOdds)
	totalOdds := &models.OverUnderOdds{GameID: game.ID, Source: models.OddsSourceDraftKings,
		Total: dec("54.5"), OverOdds: dec("-110"), UnderOdds: dec("-110")}
	create("total odds", totalOdds)

	create("spread bet", &models.SpreadBet{UserID: user.ID, LeagueID: league.ID, GameID: game.ID,
		SpreadOddsID: spreadOdds.ID, Pick: models.SpreadPickHome, SpreadSnapshot: dec("-7"),
		OddsSnapshot: dec("-110"), Stake: dec("25"), Status: models.BetStatusWon, IsHolyLock: true})
	create("money line bet", &models.MoneyLineBet{UserID: user.ID, LeagueID: league.ID, GameID: game.ID,
		MoneyLineOddsID: moneyLineOdds.ID, Pick: models.MoneyLinePickAway,
		OddsSnapshot: dec("150"), Stake: dec("40"), Status: models.BetStatusPending})
	create("total bet", &models.OverUnderBet{UserID: user.ID, LeagueID: league.ID, GameID: game.ID,
		OverUnderOddsID: totalOdds.ID, Pick: models.OverUnderPickUnder, TotalSnapshot: dec("54.5"),
		OddsSnapshot: dec("-110"), Stake: dec("10"), Status: models.BetStatusLost})
	// A bet in another league must not appear.
	create("other league's bet", &models.SpreadBet{UserID: user.ID, LeagueID: other.ID, GameID: game.ID,
		SpreadOddsID: spreadOdds.ID, Pick: models.SpreadPickAway, SpreadSnapshot: dec("7"),
		OddsSnapshot: dec("-110"), Stake: dec("99"), Status: models.BetStatusPending})

	rows, err := NewBetRecordRepository(db).FindLeagueBets(league.ID)
	if err != nil {
		t.Fatalf("FindLeagueBets: %v", err)
	}

	byType := make(map[string]LeagueBetRow)
	for _, row := range rows {
		byType[row.BetType] = row
	}
	if len(rows) != 3 || len(byType) != 3 {
		t.Fatalf("got %d rows over types %v, want one each of spread, moneyline and overunder", len(rows), byType)
	}

	for betType, row := range byType {
		if row.UserID != user.ID || row.Username != user.Username {
			t.Errorf("%s: user = %s %q, want %s %q", betType, row.UserID, row.Username, user.ID, user.Username)
		}
		if row.Season == nil || *row.Season != 2026 || row.Week == nil || *row.Week != 6 {
			t.Errorf("%s: season/week = %v/%v, want 2026/6", betType, row.Season, row.Week)
		}
		if row.HomeAbbr != home.Abbreviation || row.AwayAbbr != away.Abbreviation {
			t.Errorf("%s: teams = %s @ %s, want %s @ %s", betType, row.AwayAbbr, row.HomeAbbr, away.Abbreviation, home.Abbreviation)
		}
		if !row.ScheduledAt.Equal(kickoff) {
			t.Errorf("%s: kickoff = %s, want %s", betType, row.ScheduledAt, kickoff)
		}
	}

	tests := []struct {
		betType, status, pick, stake, odds string
		line                               *string
		lock                               bool
	}{
		{"spread", "won", "home", "25", "-110", new("-7.0"), true},
		{"moneyline", "pending", "away", "40", "150", nil, false},
		{"overunder", "lost", "under", "10", "-110", new("54.5"), false},
	}
	for _, tt := range tests {
		row := byType[tt.betType]
		if row.Status != tt.status || row.Pick != tt.pick || row.IsHolyLock != tt.lock {
			t.Errorf("%s: status/pick/lock = %s/%s/%v, want %s/%s/%v",
				tt.betType, row.Status, row.Pick, row.IsHolyLock, tt.status, tt.pick, tt.lock)
		}
		if !row.Stake.Equal(dec(tt.stake)) || !row.OddsSnapshot.Equal(dec(tt.odds)) {
			t.Errorf("%s: stake/odds = %s/%s, want %s/%s", tt.betType, row.Stake, row.OddsSnapshot, tt.stake, tt.odds)
		}
		switch {
		case tt.line == nil && row.LineValue != nil:
			t.Errorf("%s: line = %q, want none", tt.betType, *row.LineValue)
		case tt.line != nil && (row.LineValue == nil || *row.LineValue != *tt.line):
			t.Errorf("%s: line = %v, want %q", tt.betType, row.LineValue, *tt.line)
		}
	}
}
