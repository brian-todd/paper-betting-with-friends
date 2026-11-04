package repository

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
)

func TestPollRankAsOf(t *testing.T) {
	db := testdb.Open(t)
	repo := NewPollRankRepository(db)

	team := testdb.InsertTeam(t, db, "").ID
	other := testdb.InsertTeam(t, db, "").ID

	week1 := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	week2 := week1.AddDate(0, 0, 7)
	lastSeason := week1.AddDate(-1, 0, 0)

	if err := repo.Upsert([]models.PollRank{
		{TeamID: team, Season: 2027, Poll: models.PollAP, PollDate: week1, Rank: 5},
		{TeamID: other, Season: 2027, Poll: models.PollAP, PollDate: week1, Rank: 6},
		{TeamID: team, Season: 2027, Poll: models.PollAP, PollDate: week2, Rank: 3},
		{TeamID: team, Season: 2027, Poll: "Coaches Poll", PollDate: week2, Rank: 9},
		{TeamID: team, Season: 2026, Poll: models.PollAP, PollDate: lastSeason, Rank: 20},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	tests := []struct {
		name string
		at   time.Time
		want map[uuid.UUID]int
	}{
		{"before the season's first poll there is nothing", week1.Add(-time.Hour), map[uuid.UUID]int{}},
		{"on the poll's date it counts", week1, map[uuid.UUID]int{team: 5, other: 6}},
		{"between polls the earlier one stands", week2.Add(-time.Hour), map[uuid.UUID]int{team: 5, other: 6}},
		// other dropped out of the newer poll, so it is unranked, not still sixth.
		{"a newer poll replaces it whole", week2, map[uuid.UUID]int{team: 3}},
		{"long after the last poll it still stands", week2.AddDate(0, 3, 0), map[uuid.UUID]int{team: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.AsOf(2027, models.PollAP, tt.at)
			if err != nil {
				t.Fatalf("AsOf: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("AsOf = %v, want %v", got, tt.want)
			}
			for id, rank := range tt.want {
				if got[id] != rank {
					t.Errorf("rank = %d, want %d", got[id], rank)
				}
			}
		})
	}

	t.Run("a re-run replaces a rank rather than duplicating it", func(t *testing.T) {
		if err := repo.Upsert([]models.PollRank{{TeamID: team, Season: 2027, Poll: models.PollAP, PollDate: week2, Rank: 4}}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got, err := repo.AsOf(2027, models.PollAP, week2)
		if err != nil {
			t.Fatalf("AsOf: %v", err)
		}
		if got[team] != 4 {
			t.Errorf("rank = %d, want 4", got[team])
		}
	})

	t.Run("the latest date is per season and poll", func(t *testing.T) {
		latest, ok, err := repo.LatestPollDate(2027, models.PollAP)
		if err != nil || !ok || !latest.Equal(week2) {
			t.Errorf("LatestPollDate = %v, %v, %v, want %v", latest, ok, err, week2)
		}
		if _, ok, err := repo.LatestPollDate(2030, models.PollAP); err != nil || ok {
			t.Errorf("LatestPollDate for an empty season = %v, %v, want not found", ok, err)
		}
	})
}
