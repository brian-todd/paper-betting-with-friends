package cbbdata

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestFinalIsCredible(t *testing.T) {
	tests := []struct {
		name       string
		home, away *int
		want       bool
	}{
		{"a decided game", new(70), new(64), true},
		{"a level score, which basketball cannot end on", new(70), new(70), false},
		{"both sides missing", nil, nil, false},
		{"one side missing", new(70), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := APIScoreboardGame{
				HomeTeam: APIScoreboardTeam{Points: tt.home},
				AwayTeam: APIScoreboardTeam{Points: tt.away},
			}
			if got := finalIsCredible(g); got != tt.want {
				t.Errorf("finalIsCredible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreboardResult(t *testing.T) {
	now := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		status        string
		home, away    *int
		wantRow       bool
		wantFinalized bool
	}{
		// What the feed really sends for a game nobody has played.
		{"a scheduled game's 0-0 is not a score", scoreboardStatusScheduled, new(0), new(0), false, false},
		{"nor a postponed game's", scoreboardStatusPostponed, new(0), new(0), false, false},
		{"nor a cancelled game's", scoreboardStatusCancelled, new(0), new(0), false, false},
		{"a live score is provisional", scoreboardStatusInProgress, new(30), new(28), true, false},
		{"a live game with no points yet has nothing to write", scoreboardStatusInProgress, nil, nil, false, false},
		{"a decided final is finalized", scoreboardStatusFinal, new(70), new(64), true, true},
		{"a level final is written but not finalized", scoreboardStatusFinal, new(70), new(70), true, false},
		{"a final missing a side writes nothing", scoreboardStatusFinal, new(70), nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := scoreboardResult(uuid.New(), APIScoreboardGame{
				Status:   tt.status,
				HomeTeam: APIScoreboardTeam{Points: tt.home},
				AwayTeam: APIScoreboardTeam{Points: tt.away},
			}, now)
			if ok != tt.wantRow {
				t.Fatalf("wrote a row = %v, want %v", ok, tt.wantRow)
			}
			if ok && got.IsFinal() != tt.wantFinalized {
				t.Errorf("IsFinal() = %v, want %v", got.IsFinal(), tt.wantFinalized)
			}
		})
	}
}

func TestHasScore(t *testing.T) {
	for status, want := range map[models.GameStatus]bool{
		models.GameStatusScheduled:  false,
		models.GameStatusPostponed:  false,
		models.GameStatusCancelled:  false,
		models.GameStatusInProgress: true,
		models.GameStatusFinal:      true,
	} {
		if got := hasScore(status); got != want {
			t.Errorf("hasScore(%q) = %v, want %v", status, got, want)
		}
	}
}

// scoreboardRig is a database and a fake upstream answering /scoreboard with
// whatever the test last set.
type scoreboardRig struct {
	t       *testing.T
	db      *gorm.DB
	sync    *SyncService
	feed    []APIScoreboardGame
	syncsAt time.Time
}

func newScoreboardRig(t *testing.T) *scoreboardRig {
	t.Helper()
	rig := &scoreboardRig{t: t, db: testdb.Open(t), syncsAt: tipOff.Add(2 * time.Hour)}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scoreboard" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(rig.feed)
	})
	rig.sync = NewSyncService(c, rig.db)
	rig.sync.SetClock(func() time.Time { return rig.syncsAt })
	return rig
}

var tipOff = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

func (r *scoreboardRig) game(externalID int64) *models.Game {
	r.t.Helper()
	return testdb.InsertGame(r.t, r.db, models.Game{
		ExternalID:  &externalID,
		Sport:       models.SportBasketball,
		ScheduledAt: tipOff,
	})
}

// run replaces what the feed says and syncs it.
func (r *scoreboardRig) run(games ...APIScoreboardGame) {
	r.t.Helper()
	r.feed = games
	if err := r.sync.SyncScoreboard(context.Background()); err != nil {
		r.t.Fatalf("SyncScoreboard: %v", err)
	}
}

func (r *scoreboardRig) stored(game *models.Game) (models.Game, *models.GameResult, *models.GameLiveState) {
	r.t.Helper()
	var g models.Game
	if err := r.db.Preload("Result").Preload("LiveState").First(&g, "id = ?", game.ID).Error; err != nil {
		r.t.Fatalf("reading game: %v", err)
	}
	return g, g.Result, g.LiveState
}

func scoreboardGame(id int64, status string, home, away *int) APIScoreboardGame {
	return APIScoreboardGame{
		ID:        id,
		StartDate: tipOff,
		Status:    status,
		HomeTeam:  APIScoreboardTeam{Points: home},
		AwayTeam:  APIScoreboardTeam{Points: away},
	}
}

func TestSyncScoreboardWritesWhatTheStatusAllows(t *testing.T) {
	r := newScoreboardRig(t)

	t.Run("a scheduled game gets its broadcast and no score", func(t *testing.T) {
		game := r.game(101)
		feed := scoreboardGame(101, scoreboardStatusScheduled, new(0), new(0))
		feed.TV = " ESPN2 "
		r.run(feed)

		g, result, state := r.stored(game)
		if g.Status != models.GameStatusScheduled {
			t.Errorf("status = %q, want scheduled", g.Status)
		}
		if result != nil {
			t.Errorf("stored a result %d-%d for a game nobody has played", result.HomeScore, result.AwayScore)
		}
		if got := state.Broadcast(); got != "ESPN2" {
			t.Errorf("Broadcast() = %q, want ESPN2", got)
		}
	})

	t.Run("a live game reads as basketball", func(t *testing.T) {
		game := r.game(102)
		feed := scoreboardGame(102, scoreboardStatusInProgress, new(30), new(28))
		feed.Period, feed.Clock = new(2), new("07:42")
		r.run(feed)

		g, result, state := r.stored(game)
		if g.Status != models.GameStatusInProgress {
			t.Errorf("status = %q, want in_progress", g.Status)
		}
		if result == nil || result.IsFinal() {
			t.Fatalf("result = %+v, want a provisional score", result)
		}
		if got := state.LiveLabel(g.Sport); got != "2H · 07:42" {
			t.Errorf("LiveLabel = %q, want 2H · 07:42", got)
		}
	})

	t.Run("a level final is not believed", func(t *testing.T) {
		game := r.game(103)
		r.run(scoreboardGame(103, scoreboardStatusInProgress, new(60), new(58)))
		r.run(scoreboardGame(103, scoreboardStatusFinal, new(70), new(70)))

		g, result, _ := r.stored(game)
		if g.Status != models.GameStatusInProgress {
			t.Errorf("status = %q, want it left at in_progress", g.Status)
		}
		if result == nil || result.IsFinal() {
			t.Fatalf("result = %+v, want the level score stored as provisional", result)
		}
		if result.HomeScore != 70 {
			t.Errorf("home score = %d, want the latest reading", result.HomeScore)
		}

		// Overtime settles it, and the same game finalizes on the next reading.
		r.run(scoreboardGame(103, scoreboardStatusFinal, new(78), new(75)))
		g, result, _ = r.stored(game)
		if g.Status != models.GameStatusFinal || !g.Completed {
			t.Errorf("status = %q completed = %v, want final and completed", g.Status, g.Completed)
		}
		if result == nil || !result.IsFinal() || result.HomeScore != 78 {
			t.Errorf("result = %+v, want a finalized 78-75", result)
		}
	})

	t.Run("a final the feed gives no score for is not believed", func(t *testing.T) {
		game := r.game(104)
		r.run(scoreboardGame(104, scoreboardStatusFinal, nil, nil))

		g, result, _ := r.stored(game)
		if g.Status != models.GameStatusScheduled {
			t.Errorf("status = %q, want it left at scheduled", g.Status)
		}
		if result != nil {
			t.Errorf("stored a result %+v with no points", result)
		}
	})

	t.Run("a finished game cannot be reopened", func(t *testing.T) {
		game := r.game(105)
		r.run(scoreboardGame(105, scoreboardStatusFinal, new(80), new(70)))
		r.run(scoreboardGame(105, scoreboardStatusInProgress, new(10), new(12)))
		r.run(scoreboardGame(105, scoreboardStatusFinal, new(10), new(10)))

		g, result, _ := r.stored(game)
		if g.Status != models.GameStatusFinal {
			t.Errorf("status = %q, want final", g.Status)
		}
		if result.HomeScore != 80 || result.AwayScore != 70 {
			t.Errorf("score = %d-%d, want the finalized 80-70", result.HomeScore, result.AwayScore)
		}
	})

	t.Run("a moved tip-off is corrected, but not to a placeholder", func(t *testing.T) {
		game := r.game(106)
		moved := tipOff.Add(time.Hour)

		placeholder := scoreboardGame(106, scoreboardStatusScheduled, new(0), new(0))
		placeholder.StartDate, placeholder.StartTimeTBD = moved, true
		r.run(placeholder)
		if g, _, _ := r.stored(game); !g.ScheduledAt.Equal(tipOff) {
			t.Errorf("scheduled_at = %v after a TBD reading, want it unchanged", g.ScheduledAt)
		}

		missing := scoreboardGame(106, scoreboardStatusScheduled, new(0), new(0))
		missing.StartDate = time.Time{}
		r.run(missing)
		if g, _, _ := r.stored(game); !g.ScheduledAt.Equal(tipOff) {
			t.Errorf("scheduled_at = %v after a missing startDate, want it unchanged", g.ScheduledAt)
		}

		real := scoreboardGame(106, scoreboardStatusScheduled, new(0), new(0))
		real.StartDate = moved
		r.run(real)
		if g, _, _ := r.stored(game); !g.ScheduledAt.Equal(moved) {
			t.Errorf("scheduled_at = %v, want the corrected %v", g.ScheduledAt, moved)
		}
	})

	t.Run("postponed and cancelled are left to the games feed", func(t *testing.T) {
		game := r.game(107)
		r.run(scoreboardGame(107, scoreboardStatusPostponed, new(0), new(0)))
		if g, _, _ := r.stored(game); g.Status != models.GameStatusScheduled {
			t.Errorf("status = %q, want it left at scheduled", g.Status)
		}
	})

	t.Run("a game the table does not hold is skipped, not an error", func(t *testing.T) {
		r.run(scoreboardGame(999_999, scoreboardStatusFinal, new(70), new(64)))
	})
}

// This is the whole basketball money path, feed to purse, and the first test to
// walk it: a bet is placed before tip-off, the scoreboard finalizes the game, and
// the settlement job pays it without any /games sync having run.
func TestScoreboardFinalPaysABasketballBet(t *testing.T) {
	r := newScoreboardRig(t)
	game := r.game(201)

	user := &models.User{Username: "bettor-" + uuid.NewString()[:8], PasswordHash: "x"}
	if err := r.db.Create(user).Error; err != nil {
		t.Fatalf("creating user: %v", err)
	}
	league := &models.League{Name: "hoops", CreatedBy: user.ID, StartingBalance: decimal.NewFromInt(1000)}
	if err := r.db.Create(league).Error; err != nil {
		t.Fatalf("creating league: %v", err)
	}
	if err := r.db.Create(&models.LeagueMember{LeagueID: league.ID, UserID: user.ID}).Error; err != nil {
		t.Fatalf("joining league: %v", err)
	}
	if err := r.db.Create(&models.Purse{UserID: user.ID, LeagueID: league.ID, Balance: decimal.NewFromInt(1000)}).Error; err != nil {
		t.Fatalf("creating purse: %v", err)
	}
	odds := &models.SpreadOdds{
		GameID:     game.ID,
		Source:     models.OddsSourceDraftKings,
		HomeSpread: decimal.NewFromInt(-5),
		AwaySpread: decimal.NewFromInt(5),
		HomeOdds:   decimal.NewFromInt(-110),
		AwayOdds:   decimal.NewFromInt(-110),
	}
	if err := r.db.Create(odds).Error; err != nil {
		t.Fatalf("creating odds: %v", err)
	}

	service := bets.NewService(r.db)
	now := tipOff.Add(-time.Hour)
	service.SetClock(func() time.Time { return now })

	bet, err := service.CreateSpreadBet(bets.CreateSpreadBetInput{
		UserID: user.ID, LeagueID: league.ID, GameID: game.ID,
		Pick: models.SpreadPickHome, Stake: decimal.NewFromInt(110), OddsID: &odds.ID,
	})
	if err != nil {
		t.Fatalf("placing the bet: %v", err)
	}

	balance := func() decimal.Decimal {
		t.Helper()
		var purse models.Purse
		if err := r.db.First(&purse, "user_id = ? AND league_id = ?", user.ID, league.ID).Error; err != nil {
			t.Fatalf("reading purse: %v", err)
		}
		return purse.Balance
	}
	status := func() models.BetStatus {
		t.Helper()
		var stored models.SpreadBet
		if err := r.db.First(&stored, "id = ?", bet.ID).Error; err != nil {
			t.Fatalf("reading bet: %v", err)
		}
		return stored.Status
	}
	settle := func(at time.Time) {
		t.Helper()
		now = at
		if err := service.SettleFinalGames(context.Background()); err != nil {
			t.Fatalf("SettleFinalGames: %v", err)
		}
	}

	// A level "final" late on must not pay anything out, at any point after it.
	r.run(scoreboardGame(201, scoreboardStatusFinal, new(70), new(70)))
	settle(tipOff.Add(3 * time.Hour))
	if got := status(); got != models.BetStatusPending {
		t.Fatalf("bet status = %q after a level final, want pending", got)
	}

	// Home by 8 covers -5. A $110 stake at -110 returns $210.
	r.run(scoreboardGame(201, scoreboardStatusFinal, new(78), new(70)))
	settle(tipOff.Add(3 * time.Hour))
	if got := status(); got != models.BetStatusWon {
		t.Fatalf("bet status = %q, want won", got)
	}
	if got, want := balance(), decimal.NewFromInt(1100); !got.Equal(want) {
		t.Errorf("balance = %s, want %s", got, want)
	}

	// A later sweep has nothing left to pay.
	settle(tipOff.Add(4 * time.Hour))
	if got, want := balance(), decimal.NewFromInt(1100); !got.Equal(want) {
		t.Errorf("balance = %s after a second sweep, want %s", got, want)
	}
}
