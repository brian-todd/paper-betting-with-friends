package cbbdata

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestGameLineupsFrom(t *testing.T) {
	game, team := uuid.New(), uuid.New()
	athletes := models.LineupAthletes{{ID: 1, Name: "A"}}
	row := func(teamID int64, hash string, who models.LineupAthletes) APIGameLineup {
		return APIGameLineup{
			TeamID: teamID, IDHash: hash, Athletes: who, TotalSeconds: 90.6,
			TeamStats: APILineupSide{
				Points: 5, Possessions: 4,
				FieldGoals:    APIMadeAttempted{Made: 2, Attempted: 6},
				ThreePointers: APIMadeAttempted{Made: 1, Attempted: 3},
				FreeThrows:    APIMadeAttempted{Made: 0, Attempted: 2},
				Turnovers:     1, OffensiveRebounds: 2, DefensiveRebounds: 3,
			},
			Opponent: APILineupSide{
				Points: 3, Possessions: 5,
				FieldGoals:    APIMadeAttempted{Made: 1, Attempted: 7},
				ThreePointers: APIMadeAttempted{Made: 1, Attempted: 4},
				FreeThrows:    APIMadeAttempted{Made: 0, Attempted: 0},
				Turnovers:     2, OffensiveRebounds: 1, DefensiveRebounds: 4,
			},
		}
	}

	got := gameLineupsFrom(game, []APIGameLineup{
		row(72, "1", athletes),
		row(72, "1", athletes),  // the same unit twice would fail the whole insert
		row(999, "2", athletes), // a team we do not hold
		row(72, "", athletes),   // nothing to key it by
		row(72, "3", nil),       // nobody to show
	}, map[int64]uuid.UUID{72: team})

	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(got), got)
	}
	l := got[0]
	if l.GameID != game || l.TeamID != team || l.Seconds != 91 {
		t.Errorf("row = %+v", l)
	}
	if l.PointsFor != 5 || l.PossessionsFor != 4 || l.PointsAgainst != 3 || l.PossessionsAgainst != 5 {
		t.Errorf("totals = %+v, want the team's on one side and the opponent's on the other", l)
	}
	wantFor := models.LineupBox{FGMade: 2, FGAttempted: 6, ThreeMade: 1, ThreeAttempted: 3, FTAttempted: 2, Turnovers: 1, OffRebounds: 2, DefRebounds: 3}
	wantAgainst := models.LineupBox{FGMade: 1, FGAttempted: 7, ThreeMade: 1, ThreeAttempted: 4, Turnovers: 2, OffRebounds: 1, DefRebounds: 4}
	if l.For != wantFor || l.Against != wantAgainst {
		t.Errorf("boxes = %+v and %+v, want %+v and %+v", l.For, l.Against, wantFor, wantAgainst)
	}
}

// lineupRig is a database and a fake /lineups/game endpoint.
type lineupRig struct {
	t      *testing.T
	db     *gorm.DB
	sync   *SyncService
	now    time.Time
	asked  []int64
	answer func(id int64) (int, []APIGameLineup)
}

func newLineupRig(t *testing.T) *lineupRig {
	t.Helper()
	r := &lineupRig{t: t, db: testdb.Open(t), now: time.Date(2026, 12, 5, 18, 0, 0, 0, time.UTC)}
	r.answer = func(int64) (int, []APIGameLineup) { return http.StatusOK, nil }

	c := newTestClient(t, func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(strings.TrimPrefix(req.URL.Path, "/lineups/game/"), 10, 64)
		if err != nil {
			t.Errorf("unexpected path %s", req.URL.Path)
			return
		}
		r.asked = append(r.asked, id)
		status, rows := r.answer(id)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(rows)
	})
	r.sync = NewSyncService(c, r.db)
	r.sync.SetClock(func() time.Time { return r.now })
	return r
}

// game writes a finished basketball game with a line, tipping off long enough ago
// to be due. change adjusts it before it is written.
func (r *lineupRig) game(id int64, change func(*models.Game), withLine bool) *models.Game {
	r.t.Helper()
	g := models.Game{
		ExternalID:  &id,
		Sport:       models.SportBasketball,
		Status:      models.GameStatusFinal,
		ScheduledAt: r.now.Add(-12 * time.Hour),
	}
	if change != nil {
		change(&g)
	}
	game := testdb.InsertGame(r.t, r.db, g)
	if withLine {
		line := &models.OverUnderOdds{
			GameID: game.ID, Source: models.OddsSourceDraftKings, Total: decimal.NewFromInt(140),
			OverOdds: decimal.NewFromInt(-110), UnderOdds: decimal.NewFromInt(-110),
		}
		if err := r.db.Create(line).Error; err != nil {
			r.t.Fatalf("creating line: %v", err)
		}
	}
	return game
}

// run syncs and returns which games it asked about, in order.
func (r *lineupRig) run() ([]int64, error) {
	r.t.Helper()
	r.asked = nil
	err := r.sync.SyncLineups(context.Background())
	return r.asked, err
}

func (r *lineupRig) requireAsked(want ...int64) {
	r.t.Helper()
	got, err := r.run()
	if err != nil {
		r.t.Fatalf("SyncLineups: %v", err)
	}
	if len(got) != len(want) {
		r.t.Fatalf("asked about %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			r.t.Fatalf("asked about %v, want %v", got, want)
		}
	}
}

// Every one of these is a request spent on nothing if it is wrong, and the old-game
// case is thousands of them: seeding a past season leaves that season's every game
// final with a line.
func TestSyncLineupsAsksOnlyAboutGamesThatAreDue(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*models.Game)
		withLine bool
		want     bool
	}{
		{"a recently finished game with a line", nil, true, true},
		{"without a line", nil, false, false},
		{"not final", func(g *models.Game) { g.Status = models.GameStatusInProgress }, true, false},
		{"cancelled", func(g *models.Game) { g.Status = models.GameStatusCancelled }, true, false},
		{"football", func(g *models.Game) { g.Sport = models.SportFootball }, true, false},
		{"tipped off too recently for the feed to have it",
			func(g *models.Game) { g.ScheduledAt = time.Date(2026, 12, 5, 14, 0, 0, 0, time.UTC) }, true, false},
		{"from a past season",
			func(g *models.Game) { g.ScheduledAt = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC) }, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLineupRig(t)
			r.game(1, tt.change, tt.withLine)

			var want []int64
			if tt.want {
				want = []int64{1}
			}
			r.requireAsked(want...)
		})
	}
}

// The cap is what holds this job to its budget, however many games are waiting.
func TestSyncLineupsRespectsTheCapAndDrainsTheBacklog(t *testing.T) {
	r := newLineupRig(t)
	for id := int64(1); id <= lineupsPerRun+2; id++ {
		r.game(id, func(g *models.Game) { g.ScheduledAt = g.ScheduledAt.Add(-time.Duration(id) * time.Minute) }, true)
	}

	// Newest first.
	r.requireAsked(1, 2, 3)
	r.requireAsked(4, 5)
	r.requireAsked()
}

func TestSyncLineupsRetriesAnEmptyAnswerAFewTimes(t *testing.T) {
	r := newLineupRig(t)
	r.game(1, nil, true)

	r.requireAsked(1)
	r.requireAsked() // not again straight away

	r.now = r.now.Add(23 * time.Hour)
	r.requireAsked()
	r.now = r.now.Add(time.Hour)
	r.requireAsked(1)

	r.now = r.now.Add(24 * time.Hour)
	r.requireAsked(1)

	// Three attempts is all it gets.
	r.now = r.now.Add(24 * time.Hour)
	r.requireAsked()
}

func TestSyncLineupsStoresAnAnswerAndDoesNotAskAgain(t *testing.T) {
	r := newLineupRig(t)
	game := r.game(1, nil, true)

	team := testdb.InsertTeam(t, r.db, "")
	external := int64(72)
	if err := r.db.Model(team).Updates(map[string]any{"external_id": external, "sport": models.SportBasketball}).Error; err != nil {
		t.Fatalf("updating team: %v", err)
	}

	r.answer = func(int64) (int, []APIGameLineup) { return http.StatusOK, nil }
	r.requireAsked(1)

	// The retry finds it, which also records that it was found.
	r.now = r.now.Add(24 * time.Hour)
	r.answer = func(int64) (int, []APIGameLineup) {
		return http.StatusOK, []APIGameLineup{{
			TeamID: external, IDHash: "1-2-3-4-5", TotalSeconds: 600,
			Athletes:  models.LineupAthletes{{ID: 1, Name: "One"}},
			TeamStats: APILineupSide{Points: 20, Possessions: 18},
			Opponent:  APILineupSide{Points: 15, Possessions: 18},
		}}
	}
	r.requireAsked(1)

	var stored []models.GameLineup
	if err := r.db.Where("game_id = ?", game.ID).Find(&stored).Error; err != nil {
		t.Fatalf("reading lineups: %v", err)
	}
	if len(stored) != 1 || stored[0].TeamID != team.ID || stored[0].Athletes[0].Name != "One" {
		t.Errorf("stored = %+v", stored)
	}

	r.now = r.now.Add(24 * time.Hour)
	r.requireAsked()
}

// One game the feed always refuses must not eat the whole run, every run.
func TestSyncLineupsFailureDoesNotBlockTheQueue(t *testing.T) {
	r := newLineupRig(t)
	r.game(1, nil, true)
	r.game(2, func(g *models.Game) { g.ScheduledAt = g.ScheduledAt.Add(-time.Hour) }, true)
	r.answer = func(id int64) (int, []APIGameLineup) {
		if id == 1 {
			return http.StatusBadGateway, nil
		}
		return http.StatusOK, nil
	}

	asked, err := r.run()
	if err == nil {
		t.Error("SyncLineups() = nil with a failed request; the job has to report it")
	}
	if len(asked) != 2 {
		t.Errorf("asked about %v, want both games despite the first failing", asked)
	}

	// It waits its turn rather than being retried first.
	r.game(3, nil, true)
	r.answer = func(int64) (int, []APIGameLineup) { return http.StatusOK, nil }
	r.requireAsked(3)
}

// An outage says nothing about the games it touched, so they keep every attempt for
// when the feed can answer.
func TestSyncLineupsOutageDoesNotUseUpAttempts(t *testing.T) {
	r := newLineupRig(t)
	r.game(1, nil, true)

	r.answer = func(int64) (int, []APIGameLineup) { return http.StatusServiceUnavailable, nil }
	for range 5 {
		asked, err := r.run()
		if err == nil {
			t.Error("SyncLineups() = nil during an outage; the job has to report it")
		}
		if len(asked) != 1 {
			t.Fatalf("asked about %v during the outage, want the game once a day", asked)
		}
		if asked, _ := r.run(); len(asked) != 0 {
			t.Fatalf("asked about %v straight after a failure, want it to wait", asked)
		}
		r.now = r.now.Add(24 * time.Hour)
	}

	// The feed is back, and the game still gets its three real answers.
	r.answer = func(int64) (int, []APIGameLineup) { return http.StatusOK, nil }
	for range 3 {
		r.requireAsked(1)
		r.now = r.now.Add(24 * time.Hour)
	}
	r.requireAsked()
}

func TestSyncLineupsShutdownRecordsNothing(t *testing.T) {
	r := newLineupRig(t)
	r.game(1, nil, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.answer = func(int64) (int, []APIGameLineup) {
		cancel()
		return http.StatusBadGateway, nil
	}
	if err := r.sync.SyncLineups(ctx); err == nil {
		t.Error("SyncLineups() = nil on a cancelled run")
	}

	var fetches int64
	if err := r.db.Model(&models.GameLineupFetch{}).Count(&fetches).Error; err != nil {
		t.Fatalf("counting fetches: %v", err)
	}
	if fetches != 0 {
		t.Errorf("%d fetches recorded for a run cut short, want none", fetches)
	}
}

func TestSyncLineupsSkipsAGameWithOnlyACustomLine(t *testing.T) {
	r := newLineupRig(t)
	game := r.game(1, nil, false)
	custom := &models.OverUnderOdds{
		GameID: game.ID, Source: models.OddsSourceCustom, Total: decimal.NewFromInt(140),
		OverOdds: decimal.NewFromInt(-110), UnderOdds: decimal.NewFromInt(-110),
	}
	if err := r.db.Create(custom).Error; err != nil {
		t.Fatalf("creating custom line: %v", err)
	}
	r.requireAsked()
}

func TestRankingsMissing(t *testing.T) {
	r := newLineupRig(t) // December 2026, so season 2027
	team := testdb.InsertTeam(t, r.db, "")
	poll := func(season int, date time.Time) {
		t.Helper()
		if err := r.db.Create(&models.PollRank{
			TeamID: team.ID, Season: season, Poll: models.PollAP, PollDate: date, Rank: 1,
		}).Error; err != nil {
			t.Fatalf("creating poll: %v", err)
		}
	}
	july := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	if r.sync.RankingsMissing(july, time.UTC) {
		t.Error("missing out of season, which would spend a request a restart all summer")
	}
	if !r.sync.RankingsMissing(r.now, time.UTC) {
		t.Error("not missing with nothing stored, so a first deploy has no ranks")
	}

	poll(2026, time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	if !r.sync.RankingsMissing(r.now, time.UTC) {
		t.Error("last season's poll counted for this one")
	}

	// A month old, and that is not missing: AP goes weeks without publishing.
	poll(2027, r.now.AddDate(0, -1, 0))
	if r.sync.RankingsMissing(r.now, time.UTC) {
		t.Error("an old poll read as missing, which spends a request on every restart")
	}
}

func TestSyncRankingsFailsWhenItCouldPlaceNoRank(t *testing.T) {
	db := testdb.Open(t)
	rank := float64(1)
	var feed []APIPollRanking
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(feed)
	})
	sync := NewSyncService(client, db)
	sync.SetClock(func() time.Time { return time.Date(2026, 12, 5, 18, 0, 0, 0, time.UTC) })
	date := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if err := sync.SyncRankings(ctx); err != nil {
		t.Errorf("an empty feed: %v, want nil, since no poll may be published yet", err)
	}

	feed = []APIPollRanking{{Season: 2027, PollDate: &date, PollType: "AP Top 25", TeamID: 77, Ranking: &rank}}
	if err := sync.SyncRankings(ctx); err == nil {
		t.Error("nil with every rank dropped for a team not stored; the job reads as healthy")
	}

	team := testdb.InsertTeam(t, db, "")
	if err := db.Model(team).Updates(map[string]any{"external_id": 77, "sport": models.SportBasketball}).Error; err != nil {
		t.Fatalf("updating team: %v", err)
	}
	if err := sync.SyncRankings(ctx); err != nil {
		t.Errorf("with the team stored: %v", err)
	}
}
