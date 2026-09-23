package cfbdata_test

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// Line history at level 2: the real /lines capture through the real sync into
// odds_movements.
//
// The repository tests prove the recorder's decision on values a test chose.
// What only the feed can show is what the sync hands it: spreads rebuilt by
// parseSpread from a float and a team name, totals through NewFromFloat, a book
// under two spellings with one of them dropped. Any of those producing a value
// that differs from the stored one by representation alone would record every
// book on every sync -- a history that looks like a busy market and is noise.
//
// Every /lines route holds one capture, so no recording contains a line that
// moved. The moved response here is derived from the real one: the same games,
// books and fields, with chosen markets pushed a step further from even. That
// keeps everything the sync reads real except the numbers the test is about.

// linesCapturedAt is the instant the week-1 /lines capture was recorded, which
// is its file name.
var linesCapturedAt = time.Date(2026, 9, 20, 12, 47, 51, 0, time.UTC)

// lines replays /lines for a week.
func (f *feed) lines(week int) {
	f.t.Helper()
	if err := f.sync.SyncLines(context.Background(), fixtureYear, &week, nil); err != nil {
		f.t.Fatalf("syncing /lines week %d: %v", week, err)
	}
	f.requireAnswered()
}

func TestSyncLinesRecordsOnlyTheLinesThatMoved(t *testing.T) {
	db := testdb.Open(t)

	// The seed runs SyncLines itself, so this is the first sync of the week.
	seedReference(t, db, linesCapturedAt)
	week := weekGames(t, db, playedWeek)

	series := testdb.OddsHistory(t, db, week.ids)
	if len(series) == 0 {
		t.Fatal("the week-1 seed stored no sportsbook lines, so nothing below could fail")
	}

	t.Run("the first sync records every series once, at its own instant", func(t *testing.T) {
		movements := testdb.CountOddsMovements(t, db, week.ids, time.Time{})
		if movements != len(series) {
			t.Errorf("recorded %d movements for %d series; the first sync is every series' first value",
				movements, len(series))
		}
		if dated := testdb.CountOddsMovements(t, db, week.ids, linesCapturedAt); dated != movements {
			t.Errorf("%d of %d movements are dated at the seed's clock %s; the rest came from somewhere else",
				dated, movements, linesCapturedAt)
		}
		testdb.RequireHistoryMatchesOdds(t, db, week.ids)
	})

	t.Run("the stored away spread is always the home spread negated", func(t *testing.T) {
		// This is why odds_movements has no away_spread column. If either
		// sync ever stores a spread whose sides are not one number seen from
		// both ends, the history is dropping something real.
		var spreads []models.SpreadOdds
		if err := db.Where("game_id IN ?", week.ids).Find(&spreads).Error; err != nil {
			t.Fatalf("reading spreads: %v", err)
		}
		for _, s := range spreads {
			if !s.AwaySpread.Equal(s.HomeSpread.Neg()) {
				t.Errorf("%s spread on game %s is %s home, %s away -- not one number from both sides",
					s.Source, s.GameID, s.HomeSpread, s.AwaySpread)
			}
		}
	})

	dir, moved := movedLinesTree(t)
	f := newFeed(t, db, linesCapturedAt, fixtureserver.WithFixtures(fixtures.FromDir(dir)))

	resyncAt := linesCapturedAt.Add(15 * time.Minute)
	t.Run("an unchanged response records nothing", func(t *testing.T) {
		// The capture exactly as recorded, fifteen minutes on -- the next
		// scheduled run on a quiet line. Every value the sync builds has to
		// compare equal to what it stored from the same bytes.
		f.at(resyncAt).lines(playedWeek)
		if got := testdb.CountOddsMovements(t, db, week.ids, resyncAt); got != 0 {
			t.Errorf("recorded %d movements from the same response a second time", got)
		}
		testdb.RequireHistoryMatchesOdds(t, db, week.ids)
	})

	movedAt := linesCapturedAt.Add(30 * time.Minute)
	t.Run("a moved response records exactly the series it moved", func(t *testing.T) {
		f.at(movedAt).lines(playedWeek)

		// Books quoting each game's market, and movements recorded for it now.
		books := make(map[gameMarket]int)
		for _, s := range testdb.OddsHistory(t, db, week.ids) {
			books[gameMarket{week.external[s.Stored.GameID], s.Stored.Market}]++
		}
		recorded := make(map[gameMarket]int)
		for _, m := range testdb.OddsMovements(t, db, week.ids) {
			if m.RecordedAt.Equal(movedAt) {
				recorded[gameMarket{week.external[m.GameID], m.Market}]++
			}
		}

		markets := map[models.OddsMarket]int{}
		for key, n := range books {
			want := 0
			if moved[key] {
				want = n
				markets[key.market] += n
			}
			if got := recorded[key]; got != want {
				t.Errorf("game %d %s: recorded %d movements, want %d (moved: %v)",
					key.externalID, key.market, got, want, moved[key])
			}
		}
		for key, got := range recorded {
			if _, ok := books[key]; !ok {
				t.Errorf("game %d %s: recorded %d movements for a series with no odds row", key.externalID, key.market, got)
			}
		}

		// Guard against passing vacuously: every market has to have moved
		// somewhere, or the check above says nothing about it.
		for _, market := range []models.OddsMarket{models.OddsMarketMoneyLine, models.OddsMarketSpread, models.OddsMarketTotal} {
			if markets[market] == 0 {
				t.Errorf("no %s series moved in the derived response, so its recording went untested", market)
			}
		}
		t.Logf("moved series: %v", markets)

		// And each recorded value is the one the odds row now holds.
		testdb.RequireHistoryMatchesOdds(t, db, week.ids)
	})

	// Last, because it adds a book: the moved-response check above counts
	// every book on a game, and ESPN is in none of its captures.
	t.Run("a book quoted under two spellings records once, and never again", func(t *testing.T) {
		dir, aliased := aliasedLinesTree(t)
		if aliased == 0 {
			t.Fatal("no game in the capture carries a DraftKings quote to alias")
		}
		f := newFeed(t, db, linesCapturedAt, fixtureserver.WithFixtures(fixtures.FromDir(dir)))

		// Two runs of one response. The first is ESPN's first sight of the
		// week, so each of its series records once. The second must record
		// nothing at all: before the quotes were folded, the "ESPN" quote read
		// as a move away from the "ESPN Bet" value the first run kept, and the
		// "ESPN Bet" quote as the move back, one phantom row per series per run.
		firstAt, secondAt := movedAt.Add(15*time.Minute), movedAt.Add(30*time.Minute)
		f.at(firstAt).lines(playedWeek)
		f.at(secondAt).lines(playedWeek)

		espn := make(map[gameMarket]models.OddsMovement)
		dk := make(map[gameMarket]models.OddsMovement)
		for _, s := range testdb.OddsHistory(t, db, week.ids) {
			key := gameMarket{week.external[s.Stored.GameID], s.Stored.Market}
			switch s.Stored.Source {
			case models.OddsSourceESPN:
				espn[key] = s.Stored
			case models.OddsSourceDraftKings:
				dk[key] = s.Stored
			}
		}
		if len(espn) == 0 {
			t.Fatal("no ESPN line was stored from the aliased response")
		}

		recordedFirst := 0
		for _, m := range testdb.OddsMovements(t, db, week.ids) {
			if m.RecordedAt.Equal(firstAt) && m.Source == models.OddsSourceESPN {
				recordedFirst++
			}
		}
		if recordedFirst != len(espn) {
			t.Errorf("the first run recorded %d ESPN movements for %d ESPN series", recordedFirst, len(espn))
		}
		if n := testdb.CountOddsMovements(t, db, week.ids, secondAt); n != 0 {
			t.Errorf("the second run of the same response recorded %d movements", n)
		}

		// The later spelling wins, as it did in the odds row before the fold:
		// every ESPN line is DraftKings' number, never the step-off one.
		for key, line := range espn {
			want, ok := dk[key]
			want.Source = line.Source
			if !ok || !testdb.SameLine(line, want) {
				t.Errorf("game %d %s: ESPN stored %+v, want the later quote's (DraftKings') line", key.externalID, key.market, line)
			}
		}
		testdb.RequireHistoryMatchesOdds(t, db, week.ids)
	})
}

// A gameMarket names every series of one market on one game, across books.
type gameMarket struct {
	externalID int64
	market     models.OddsMarket
}

// movedLinesTree writes a fixture tree for week 1's /lines holding two
// captures: the recorded one, and the same response fifteen minutes later with
// chosen markets moved. It reports which markets on which games it moved.
//
// Games are dealt round-robin over "move the spread", "move the total", "move
// the money line" and "move nothing", and every book's quote in a dealt market
// is moved, so the expectation needs no knowledge of which books the sync
// stores. Each number moves away from even -- a spread of -7 to -8, a money
// line of +130 to +140 -- which keeps the favourite the favourite: parseSpread
// takes the side from the formatted string, which is left as recorded.
func movedLinesTree(t *testing.T) (string, map[gameMarket]bool) {
	t.Helper()

	recorded, games := recordedLines(t)
	moved := make(map[gameMarket]bool)
	for i, game := range games {
		quotes, _ := game["lines"].([]any)
		id, _ := game["id"].(float64)
		var market models.OddsMarket
		switch i % 4 {
		case 0:
			market = models.OddsMarketSpread
		case 1:
			market = models.OddsMarketTotal
		case 2:
			market = models.OddsMarketMoneyLine
		default:
			continue
		}
		for _, q := range quotes {
			quote := q.(map[string]any)
			switch market {
			case models.OddsMarketSpread:
				awayFromEven(quote, "spread", 1)
			case models.OddsMarketTotal:
				awayFromEven(quote, "overUnder", 1)
			case models.OddsMarketMoneyLine:
				awayFromEven(quote, "homeMoneyline", 10)
				awayFromEven(quote, "awayMoneyline", 10)
			}
		}
		moved[gameMarket{int64(id), market}] = true
	}

	return writeLinesTree(t, map[time.Time][]byte{
		recorded.At:                       recorded.Body,
		recorded.At.Add(15 * time.Minute): encode(t, games),
	}), moved
}

// aliasedLinesTree writes a fixture tree for week 1's /lines holding one
// capture: the recorded response with every game DraftKings quotes also quoted
// by ESPN twice, under both of the spellings that map to it. "ESPN" comes first
// with every number a step off, then "ESPN Bet" with DraftKings' own. It
// reports how many games it added the pair to.
//
// No capture has a book under two live spellings -- CFBD's second DraftKings is
// dropped by the mapping -- but "ESPN" and "ESPN Bet" are both mapped, and the
// feed has used each.
func aliasedLinesTree(t *testing.T) (string, int) {
	t.Helper()

	recorded, games := recordedLines(t)
	aliased := 0
	for _, game := range games {
		quotes, _ := game["lines"].([]any)
		for _, q := range quotes {
			quote := q.(map[string]any)
			if quote["provider"] != "DraftKings" {
				continue
			}
			early, late := maps.Clone(quote), maps.Clone(quote)
			early["provider"], late["provider"] = "ESPN", "ESPN Bet"
			for field, step := range map[string]float64{
				"spread": 1, "overUnder": 1, "homeMoneyline": 10, "awayMoneyline": 10,
			} {
				awayFromEven(early, field, step)
			}
			game["lines"] = append(quotes, early, late)
			aliased++
			break
		}
	}
	return writeLinesTree(t, map[time.Time][]byte{recorded.At: encode(t, games)}), aliased
}

// recordedLines is the week-1 /lines capture, and its body decoded loosely
// enough to edit and re-encode without losing a field.
func recordedLines(t *testing.T) (fixtures.Capture, []map[string]any) {
	t.Helper()

	captures, err := fixtures.Embedded().Sequence(fixtures.CFBD, "/lines", linesQuery)
	if err != nil {
		t.Fatalf("reading the week-1 /lines capture: %v", err)
	}
	recorded := captures[len(captures)-1]
	if !recorded.At.Equal(linesCapturedAt) {
		t.Fatalf("the week-1 /lines capture is from %s, want %s -- a recapture moves this test's instants",
			recorded.At, linesCapturedAt)
	}

	var games []map[string]any
	if err := json.Unmarshal(recorded.Body, &games); err != nil {
		t.Fatalf("decoding the /lines capture: %v", err)
	}
	return recorded, games
}

// linesQuery is the week-1 /lines request, as its capture directory names it.
const linesQuery = "week=1&year=2026"

// writeLinesTree writes captures of week 1's /lines into a fresh fixture tree,
// each named for its instant, and returns the tree's root.
func writeLinesTree(t *testing.T, captures map[time.Time][]byte) string {
	t.Helper()

	dir := t.TempDir()
	leaf := filepath.Join(dir, fixtures.CFBD, "lines", linesQuery)
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	for at, body := range captures {
		if err := os.WriteFile(filepath.Join(leaf, at.Format(fixtures.InstantLayout)+".json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func encode(t *testing.T, games []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(games)
	if err != nil {
		t.Fatalf("encoding a derived capture: %v", err)
	}
	return body
}

// awayFromEven moves a quoted number by step away from zero, and leaves a null
// alone -- the sync skips a market it has no number for.
func awayFromEven(quote map[string]any, field string, step float64) {
	v, ok := quote[field].(float64)
	if !ok {
		return
	}
	if v < 0 {
		quote[field] = v - step
	} else {
		quote[field] = v + step
	}
}

// A week's football games: their IDs, and the external ID each goes by in the
// capture.
type games struct {
	ids      []uuid.UUID
	external map[uuid.UUID]int64
}

// weekGames reads the games of one captured week. Two single-table reads rather
// than a join -- see testdb.OddsHistory for what a join costs here.
func weekGames(t *testing.T, db *gorm.DB, number int) games {
	t.Helper()

	var week models.Week
	if err := db.Where("season = ? AND number = ? AND season_type = ?",
		fixtureYear, number, models.SeasonTypeRegular).First(&week).Error; err != nil {
		t.Fatalf("finding week %d: %v", number, err)
	}
	var rows []models.Game
	if err := db.Where("week_id = ? AND sport = ?", week.ID, models.SportFootball).
		Find(&rows).Error; err != nil {
		t.Fatalf("reading week %d's games: %v", number, err)
	}

	g := games{external: make(map[uuid.UUID]int64, len(rows))}
	for _, row := range rows {
		g.ids = append(g.ids, row.ID)
		if row.ExternalID != nil {
			g.external[row.ID] = *row.ExternalID
		}
	}
	return g
}
