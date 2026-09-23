package repository

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Record decides in one statement whether a line moved, and its failure mode is
// the quiet kind this package's tests exist for: a comparison that is always true
// records nothing and reports no error, and one that is always false records
// every sync and looks like a lively market. Each case below says which of the
// two it is guarding against.

// Sync instants, fifteen minutes apart as on a football Saturday.
var (
	syncAt0 = time.Date(2026, 10, 10, 16, 0, 0, 0, time.UTC)
	syncAt1 = syncAt0.Add(15 * time.Minute)
	syncAt2 = syncAt0.Add(30 * time.Minute)
	syncAt3 = syncAt0.Add(45 * time.Minute)
	syncAt4 = syncAt0.Add(60 * time.Minute)
)

// spreadLine is the movement a sync would offer for a spread, in the form it
// builds one: a models.SpreadOdds turned into a movement.
func spreadLine(gameID uuid.UUID, source models.OddsSource, home, price decimal.Decimal, at time.Time) models.OddsMovement {
	return models.SpreadMovement(models.SpreadOdds{
		GameID:     gameID,
		Source:     source,
		HomeSpread: home,
		AwaySpread: home.Neg(),
		HomeOdds:   price,
		AwayOdds:   price,
	}, at)
}

func moneyLine(gameID uuid.UUID, source models.OddsSource, home, away decimal.Decimal, at time.Time) models.OddsMovement {
	return models.MoneyLineMovement(models.MoneyLineOdds{
		GameID: gameID, Source: source, HomeOdds: home, AwayOdds: away,
	}, at)
}

func totalLine(gameID uuid.UUID, source models.OddsSource, total decimal.Decimal, at time.Time) models.OddsMovement {
	return models.OverUnderMovement(models.OverUnderOdds{
		GameID: gameID, Source: source, Total: total,
		OverOdds: decimal.NewFromInt(-110), UnderOdds: decimal.NewFromInt(-110),
	}, at)
}

// record offers a movement and fails the test if the decision is not the one
// expected.
func record(t *testing.T, repo *OddsMovementRepository, m models.OddsMovement, want bool) {
	t.Helper()
	got, err := repo.Record(m)
	if err != nil {
		t.Fatalf("recording %s %s at %s: %v", m.Market, m.Source, m.RecordedAt.Format(time.Kitchen), err)
	}
	if got != want {
		t.Errorf("recording %s %s at %s: recorded = %v, want %v",
			m.Market, m.Source, m.RecordedAt.Format(time.Kitchen), got, want)
	}
}

func TestRecordKeepsOnlyTheSyncsThatMovedTheLine(t *testing.T) {
	db := testdb.Open(t)
	repo := NewOddsMovementRepository(db)
	game := testdb.InsertGame(t, db, models.Game{})

	d := decimal.RequireFromString
	dk := models.OddsSourceDraftKings

	// One series walked through a Saturday, step by step. The order matters,
	// so these are sequential assertions on one series rather than a table of
	// independent cases.
	t.Run("the first value a series sees is recorded", func(t *testing.T) {
		// An empty series has no latest row to compare against. If that read
		// as "unchanged", nothing would ever be recorded -- and nothing would
		// ever look wrong.
		record(t, repo, spreadLine(game.ID, dk, d("-3.5"), d("-110"), syncAt0), true)
	})

	t.Run("the same line at the next sync is not", func(t *testing.T) {
		record(t, repo, spreadLine(game.ID, dk, d("-3.5"), d("-110"), syncAt1), false)
	})

	t.Run("the same line spelled differently is not", func(t *testing.T) {
		// Basketball's feed sends numbers as floats and the sync builds them
		// with NewFromFloat; the columns hold them at a fixed scale. -110 and
		// -110.00, or a float -3.5, are the same line, and a comparison that
		// saw them as different would record every book on every sync.
		record(t, repo, spreadLine(game.ID, dk, decimal.NewFromFloat(-3.5), d("-110.00"), syncAt1.Add(time.Minute)), false)
	})

	t.Run("a moved number is recorded", func(t *testing.T) {
		record(t, repo, spreadLine(game.ID, dk, d("-4.0"), d("-110"), syncAt2), true)
	})

	t.Run("moving back to an earlier number is recorded", func(t *testing.T) {
		// -3.5 has been this series' value before, but not its latest. A
		// comparison against any earlier row rather than the latest one would
		// find it and drop the move back.
		record(t, repo, spreadLine(game.ID, dk, d("-3.5"), d("-110"), syncAt3), true)
	})

	t.Run("a moved price on an unchanged number is recorded", func(t *testing.T) {
		record(t, repo, spreadLine(game.ID, dk, d("-3.5"), d("-115"), syncAt4), true)
	})

	t.Run("the history reads back as the steps the line took", func(t *testing.T) {
		movements := testdb.OddsMovements(t, db, []uuid.UUID{game.ID})

		want := []struct {
			at     time.Time
			spread string
			price  string
		}{
			{syncAt0, "-3.5", "-110"},
			{syncAt2, "-4", "-110"},
			{syncAt3, "-3.5", "-110"},
			{syncAt4, "-3.5", "-115"},
		}
		if len(movements) != len(want) {
			t.Fatalf("history holds %d movements, want %d: %+v", len(movements), len(want), movements)
		}
		for i, w := range want {
			m := movements[i]
			if !m.RecordedAt.Equal(w.at) {
				t.Errorf("movement %d recorded at %s, want %s", i, m.RecordedAt, w.at)
			}
			if m.Market != models.OddsMarketSpread || m.Source != dk {
				t.Errorf("movement %d is %s %s, want spread %s", i, m.Market, m.Source, dk)
			}
			if m.HomeSpread == nil || !m.HomeSpread.Equal(d(w.spread)) {
				t.Errorf("movement %d home spread = %v, want %s", i, m.HomeSpread, w.spread)
			}
			if m.HomeOdds == nil || !m.HomeOdds.Equal(d(w.price)) {
				t.Errorf("movement %d home odds = %v, want %s", i, m.HomeOdds, w.price)
			}
			if m.Total != nil || m.OverOdds != nil || m.UnderOdds != nil {
				t.Errorf("movement %d is a spread carrying a total's columns: %+v", i, m)
			}
		}
	})
}

func TestRecordKeepsEachSeriesToItself(t *testing.T) {
	db := testdb.Open(t)
	repo := NewOddsMovementRepository(db)
	game := testdb.InsertGame(t, db, models.Game{})
	other := testdb.InsertGame(t, db, models.Game{})

	d := decimal.RequireFromString
	dk, fd := models.OddsSourceDraftKings, models.OddsSourceFanDuel

	// A series is game, market and source. Every line here has a sibling that
	// differs in exactly one of the three, and a key missing any of them would
	// read the sibling's value as this series' latest.
	slate := func(at time.Time) []models.OddsMovement {
		return []models.OddsMovement{
			moneyLine(game.ID, dk, d("-150"), d("130"), at),
			moneyLine(game.ID, fd, d("-150"), d("130"), at),
			moneyLine(other.ID, dk, d("-150"), d("130"), at),
			spreadLine(game.ID, dk, d("-3.5"), d("-110"), at),
			totalLine(game.ID, dk, d("47.5"), at),
		}
	}

	t.Run("each series records its own first value", func(t *testing.T) {
		for _, m := range slate(syncAt0) {
			record(t, repo, m, true)
		}
	})

	t.Run("an unchanged slate records nothing", func(t *testing.T) {
		for _, m := range slate(syncAt1) {
			record(t, repo, m, false)
		}
	})

	t.Run("a move in one series records only that series", func(t *testing.T) {
		moved := slate(syncAt2)
		moved[1] = moneyLine(game.ID, fd, d("-160"), d("140"), syncAt2)
		for i, m := range moved {
			record(t, repo, m, i == 1)
		}
	})
}

func TestRecordSettlesTwoQuotesAtOneInstantOnTheLast(t *testing.T) {
	db := testdb.Open(t)
	repo := NewOddsMovementRepository(db)
	game := testdb.InsertGame(t, db, models.Game{})

	d := decimal.RequireFromString
	espn := models.OddsSourceESPN

	// The syncs fold a book's quotes to one per run before writing, so this
	// is a backstop rather than a path they take: a caller that did write a
	// series twice at one instant still gets one row, holding the later value,
	// rather than two rows the next run cannot order.
	record(t, repo, totalLine(game.ID, espn, d("47.5"), syncAt0), true)
	record(t, repo, totalLine(game.ID, espn, d("48.5"), syncAt1), true)
	record(t, repo, totalLine(game.ID, espn, d("49.5"), syncAt1), true)

	movements := testdb.OddsMovements(t, db, []uuid.UUID{game.ID})
	if len(movements) != 2 {
		t.Fatalf("history holds %d movements, want 2 -- one per instant: %+v", len(movements), movements)
	}
	if got := movements[1].Total; got == nil || !got.Equal(d("49.5")) {
		t.Errorf("the second instant holds a total of %v, want the later quote's 49.5", got)
	}

	// And the next sync compares against that later quote.
	record(t, repo, totalLine(game.ID, espn, d("49.5"), syncAt2), false)
}

func TestRecordRefusesAMovementShapedForAnotherMarket(t *testing.T) {
	db := testdb.Open(t)
	game := testdb.InsertGame(t, db, models.Game{})

	d := decimal.RequireFromString
	missingAway := moneyLine(game.ID, models.OddsSourceBovada, d("-150"), d("130"), syncAt0)
	missingAway.AwayOdds = nil
	spreadWithTotal := spreadLine(game.ID, models.OddsSourceBovada, d("-3.5"), d("-110"), syncAt0)
	spreadWithTotal.Total = new(d("47.5"))
	totalAsMoneyLine := totalLine(game.ID, models.OddsSourceBovada, d("47.5"), syncAt0)
	totalAsMoneyLine.Market = models.OddsMarketMoneyLine

	// The CHECK is what keeps a row readable as its market. A refused
	// statement aborts the transaction, so each case runs inside a savepoint
	// and rolls back to it -- otherwise the first refusal would make every
	// later case fail for the wrong reason.
	for name, m := range map[string]models.OddsMovement{
		"a money line without its away side": missingAway,
		"a spread carrying a total":          spreadWithTotal,
		"a total labelled as a money line":   totalAsMoneyLine,
	} {
		t.Run(name, func(t *testing.T) {
			withSavepoint(t, db, func(repo *OddsMovementRepository) {
				if _, err := repo.Record(m); err == nil {
					t.Errorf("recorded %+v; the market shape check should refuse it", m)
				}
			})
		})
	}
}

// withSavepoint runs fn against the transaction and rolls back to where it
// started, so a statement fn expects to fail does not abort the rest.
func withSavepoint(t *testing.T, db *gorm.DB, fn func(*OddsMovementRepository)) {
	t.Helper()
	if err := db.SavePoint("odds_movement_case").Error; err != nil {
		t.Fatalf("setting savepoint: %v", err)
	}
	fn(NewOddsMovementRepository(db))
	if err := db.RollbackTo("odds_movement_case").Error; err != nil {
		t.Fatalf("rolling back to savepoint: %v", err)
	}
}

func TestRecordComparesAgainstTheLineAsOfItsOwnInstant(t *testing.T) {
	db := testdb.Open(t)
	repo := NewOddsMovementRepository(db)
	game := testdb.InsertGame(t, db, models.Game{})

	d := decimal.RequireFromString
	bovada := models.OddsSourceBovada

	// Production records in time order, but a replay need not: a fixture seed
	// run at an earlier instant than a sync already stored. The question a
	// movement answers is "did the line differ from what it was just before
	// this", so the comparison is against the latest row at or before the
	// incoming instant, not the latest row there is.
	record(t, repo, moneyLine(game.ID, bovada, d("-150"), d("130"), syncAt0), true)
	record(t, repo, moneyLine(game.ID, bovada, d("-170"), d("150"), syncAt2), true)

	// At syncAt1 the line was still -150, so -170 then is a move -- the
	// earliest instant it is known to have held.
	record(t, repo, moneyLine(game.ID, bovada, d("-170"), d("150"), syncAt1), true)
	// And a minute later the line as of then is that -170, not the -150 before
	// it, so seeing -170 again is no move.
	record(t, repo, moneyLine(game.ID, bovada, d("-170"), d("150"), syncAt1.Add(time.Minute)), false)
}
