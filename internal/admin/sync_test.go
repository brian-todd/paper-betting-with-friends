package admin

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/scheduler"
)

// The season is validated in the service rather than in the job because a job
// runs on a background goroutine, where a bad value can only be logged. Here it
// can be handed back to the person who typed it.
func TestTriggerSyncValidatesSeason(t *testing.T) {
	nextYear := strconv.Itoa(time.Now().Year() + 1)

	tests := []struct {
		name    string
		season  string
		wantErr bool
	}{
		{name: "blank means the current season", season: ""},
		{name: "earliest season the providers have", season: "2002"},
		{name: "next season, so it can be pre-seeded", season: nextYear},
		{name: "before the providers have data", season: "2001", wantErr: true},
		{name: "far future", season: "3000", wantErr: true},
		{name: "not a number", season: "twenty-twenty", wantErr: true},
		{name: "empty-looking but not empty", season: "0", wantErr: true},
		{name: "negative", season: "-2020", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No job is registered, so a season that passes validation falls
			// through to ErrUnknownJob. That is the signal validation allowed
			// it: the two errors are distinguishable, which is all this needs.
			svc := &Service{sched: scheduler.New(nil)}

			err := svc.TriggerSync(nil, "cfb-seed", tt.season)

			if tt.wantErr && !errors.Is(err, ErrInvalidSeason) {
				t.Fatalf("TriggerSync(%q) = %v, want ErrInvalidSeason", tt.season, err)
			}
			if !tt.wantErr && errors.Is(err, ErrInvalidSeason) {
				t.Fatalf("TriggerSync(%q) = ErrInvalidSeason, want it accepted", tt.season)
			}
		})
	}
}

// A service built without a scheduler must not dereference it.
func TestTriggerSyncWithoutScheduler(t *testing.T) {
	svc := &Service{}
	if err := svc.TriggerSync(nil, "cfb-seed", ""); !errors.Is(err, scheduler.ErrUnknownJob) {
		t.Errorf("TriggerSync() = %v, want ErrUnknownJob", err)
	}
}
