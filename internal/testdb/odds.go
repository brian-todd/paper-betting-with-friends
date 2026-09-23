package testdb

import (
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// An OddsSeries is one sportsbook's line for one market of a game, as the odds
// tables store it and as its history last recorded it.
type OddsSeries struct {
	Stored models.OddsMovement
	// Latest is the newest movement in the series, or nil if it has none.
	Latest *models.OddsMovement
}

// Agrees reports whether the history replayed to now gives the stored line.
func (s OddsSeries) Agrees() bool {
	return s.Latest != nil && SameLine(s.Stored, *s.Latest)
}

// OddsHistory reads every sportsbook line stored for the given games, each
// paired with the newest movement in its series.
//
// Every read here is a single table and the pairing happens in Go, on purpose.
// The obvious form -- the odds tables joined to games, with a LATERAL lookup of
// each series' latest movement -- is correct, and it measured 24 ms against a
// freshly created database and 10.4 s once autovacuum had tidied up after a
// previous run. Every test rolls back, so autovacuum records the tables as "no
// tuples in N pages"; the planner then estimates one row for each, and picks a
// nested loop that rescans all 6,318 basketball games for each of 10,688 spreads.
// A single-table scan has no join order to get wrong.
func OddsHistory(t *testing.T, db *gorm.DB, gameIDs []uuid.UUID) []OddsSeries {
	t.Helper()

	// The stored side is mapped here, column by column, rather than through
	// models.SpreadMovement and its siblings. Those are what the syncs record
	// with, so a constructor that read the wrong column would be wrong on both
	// sides of the comparison and agree with itself.
	var stored []models.OddsMovement
	var moneyLines []models.MoneyLineOdds
	readBookLines(t, db, gameIDs, &moneyLines)
	for _, o := range moneyLines {
		stored = append(stored, models.OddsMovement{
			GameID: o.GameID, Source: o.Source, Market: models.OddsMarketMoneyLine,
			HomeOdds: &o.HomeOdds, AwayOdds: &o.AwayOdds,
		})
	}
	var spreads []models.SpreadOdds
	readBookLines(t, db, gameIDs, &spreads)
	for _, o := range spreads {
		stored = append(stored, models.OddsMovement{
			GameID: o.GameID, Source: o.Source, Market: models.OddsMarketSpread,
			HomeSpread: &o.HomeSpread, HomeOdds: &o.HomeOdds, AwayOdds: &o.AwayOdds,
		})
	}
	var totals []models.OverUnderOdds
	readBookLines(t, db, gameIDs, &totals)
	for _, o := range totals {
		stored = append(stored, models.OddsMovement{
			GameID: o.GameID, Source: o.Source, Market: models.OddsMarketTotal,
			Total: &o.Total, OverOdds: &o.OverOdds, UnderOdds: &o.UnderOdds,
		})
	}

	var latest []models.OddsMovement
	err := db.Raw(`SELECT DISTINCT ON (game_id, market, source) *
		FROM odds_movements WHERE game_id IN ?
		ORDER BY game_id, market, source, recorded_at DESC`, gameIDs).
		Scan(&latest).Error
	if err != nil {
		t.Fatalf("reading the latest odds movements: %v", err)
	}
	bySeries := make(map[seriesKey]*models.OddsMovement, len(latest))
	for i := range latest {
		bySeries[keyOf(latest[i])] = &latest[i]
	}

	out := make([]OddsSeries, len(stored))
	for i, s := range stored {
		out[i] = OddsSeries{Stored: s, Latest: bySeries[keyOf(s)]}
	}
	return out
}

// RequireHistoryMatchesOdds fails unless every sportsbook line on the games
// equals the latest movement in its series, and returns the series it read.
//
// This is the invariant the history exists to keep -- replayed to now, it gives
// the line the game page shows -- and it checks the recorded values rather than
// only their count.
func RequireHistoryMatchesOdds(t *testing.T, db *gorm.DB, gameIDs []uuid.UUID) []OddsSeries {
	t.Helper()
	const shown = 5
	drifted := 0
	series := OddsHistory(t, db, gameIDs)
	for _, s := range series {
		if s.Agrees() {
			continue
		}
		drifted++
		if drifted <= shown {
			latest := "no movement at all"
			if s.Latest != nil {
				latest = describeLine(*s.Latest)
			}
			t.Errorf("%s %s on game %s is stored as %s, but its history says %s",
				s.Stored.Source, s.Stored.Market, s.Stored.GameID, describeLine(s.Stored), latest)
		}
	}
	if drifted > shown {
		t.Errorf("... and %d more lines that differ from their history", drifted-shown)
	}
	return series
}

// CountOddsMovements counts the movements recorded for the given games, or with
// a non-zero at, those recorded at that instant. A count rather than a read,
// because a basketball season holds tens of thousands.
func CountOddsMovements(t *testing.T, db *gorm.DB, gameIDs []uuid.UUID, at time.Time) int {
	t.Helper()
	q := db.Model(&models.OddsMovement{}).Where("game_id IN ?", gameIDs)
	if !at.IsZero() {
		q = q.Where("recorded_at = ?", at)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("counting odds movements: %v", err)
	}
	return int(n)
}

// OddsMovements reads the movements recorded for the given games, oldest first.
func OddsMovements(t *testing.T, db *gorm.DB, gameIDs []uuid.UUID) []models.OddsMovement {
	t.Helper()
	var movements []models.OddsMovement
	if err := db.Where("game_id IN ?", gameIDs).Order("recorded_at").Find(&movements).Error; err != nil {
		t.Fatalf("reading odds movements: %v", err)
	}
	return movements
}

// SameLine reports whether two movements carry the same line, whatever their
// instants. Numbers are compared by value, so -110 and -110.00 agree.
func SameLine(a, b models.OddsMovement) bool {
	return keyOf(a) == keyOf(b) &&
		sameDecimal(a.HomeSpread, b.HomeSpread) &&
		sameDecimal(a.Total, b.Total) &&
		sameDecimal(a.HomeOdds, b.HomeOdds) &&
		sameDecimal(a.AwayOdds, b.AwayOdds) &&
		sameDecimal(a.OverOdds, b.OverOdds) &&
		sameDecimal(a.UnderOdds, b.UnderOdds)
}

// describeLine prints a line's numbers, leaving out the columns its market does
// not use.
func describeLine(m models.OddsMovement) string {
	var parts []string
	for _, c := range []struct {
		name  string
		value *decimal.Decimal
	}{
		{"spread", m.HomeSpread}, {"total", m.Total},
		{"home", m.HomeOdds}, {"away", m.AwayOdds},
		{"over", m.OverOdds}, {"under", m.UnderOdds},
	} {
		if c.value != nil {
			parts = append(parts, c.name+" "+c.value.String())
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func readBookLines(t *testing.T, db *gorm.DB, gameIDs []uuid.UUID, dest any) {
	t.Helper()
	if err := db.Where("game_id IN ? AND source <> ?", gameIDs, models.OddsSourceCustom).
		Find(dest).Error; err != nil {
		t.Fatalf("reading sportsbook lines: %v", err)
	}
}

type seriesKey struct {
	game   uuid.UUID
	market models.OddsMarket
	source models.OddsSource
}

func keyOf(m models.OddsMovement) seriesKey {
	return seriesKey{m.GameID, m.Market, m.Source}
}

func sameDecimal(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
