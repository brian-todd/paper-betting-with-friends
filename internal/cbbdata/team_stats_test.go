package cbbdata

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// The provider publishes no ratings for a season's first days, which is the state
// this season is in as this is written. That is a quiet success that stores nothing.
func TestAnEmptySeasonStoresNoRatingsAndIsNotAFailure(t *testing.T) {
	db := testdb.Open(t)
	asked := 0
	c := newTestClient(t, func(w http.ResponseWriter, req *http.Request) {
		asked++
		_, _ = w.Write([]byte("[]"))
	})
	sync := NewSyncService(c, db)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	sync.SetClock(func() time.Time { return now })

	if err := sync.SyncRatings(context.Background()); err != nil {
		t.Fatalf("SyncRatings: %v", err)
	}
	if asked != 2 {
		t.Errorf("asked %d times, want one request per rating table", asked)
	}
	var rows int64
	if err := db.Model(&models.TeamRating{}).Count(&rows).Error; err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Errorf("stored %d ratings from an empty answer", rows)
	}
	if !sync.RatingsMissing(now, time.UTC) {
		t.Error("a season with no ratings should fetch them on start")
	}
}

// Rows with nowhere to go mean the teams were never seeded, and reporting success
// would leave the job looking healthy with no ratings on any page.
func TestRatingsWithNoStoredTeamsFail(t *testing.T) {
	db := testdb.Open(t)
	c := newTestClient(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/ratings/adjusted":
			_, _ = w.Write([]byte(`[{"teamId":999999,"netRating":1.5}]`))
		case "/ratings/srs":
			_, _ = w.Write([]byte(`[{"teamId":999999,"rating":1.5}]`))
		case "/stats/team/season":
			_, _ = w.Write([]byte(`[{"teamId":999999,"games":1}]`))
		default:
			t.Errorf("unexpected path %s", req.URL.Path)
		}
	})
	sync := NewSyncService(c, db)

	if err := sync.SyncRatings(context.Background()); err == nil || !strings.Contains(err.Error(), "need seeding") {
		t.Errorf("SyncRatings error = %v, want one saying teams need seeding", err)
	}
	if err := sync.SyncTeamStats(context.Background()); err == nil || !strings.Contains(err.Error(), "need seeding") {
		t.Errorf("SyncTeamStats error = %v, want one saying teams need seeding", err)
	}
}

func TestOneRatingTableFailingStillStoresTheOther(t *testing.T) {
	db := testdb.Open(t)
	external := int64(77)
	team := testdb.InsertTeam(t, db, "")
	if err := db.Model(team).Updates(map[string]any{"external_id": external, "sport": models.SportBasketball}).Error; err != nil {
		t.Fatalf("updating team: %v", err)
	}
	c := newTestClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/ratings/adjusted" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`[{"teamId":77,"rating":4.2}]`))
	})
	sync := NewSyncService(c, db)
	sync.SetClock(func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) })

	if err := sync.SyncRatings(context.Background()); err == nil {
		t.Error("a failed table should fail the run")
	}
	var rows int64
	if err := db.Model(&models.TeamRating{}).Where("source = ?", models.RatingSourceSRS).Count(&rows).Error; err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 1 {
		t.Errorf("stored %d SRS ratings, want 1", rows)
	}
}

func TestTurnoverPctNeedsBothCounts(t *testing.T) {
	number := func(v float64) *float64 { return &v }

	side := APITeamSeasonSide{Possessions: number(200)}
	side.Turnovers.Total = number(30)
	if got := side.TurnoverPct(); got == nil || *got != 15 {
		t.Errorf("TurnoverPct = %v, want 15", got)
	}

	side.Possessions = number(0)
	if side.TurnoverPct() != nil {
		t.Error("no possessions should give no rate, not a division by zero")
	}
	side.Possessions = nil
	if side.TurnoverPct() != nil {
		t.Error("missing possessions should give no rate")
	}
}
