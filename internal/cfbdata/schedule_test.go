package cfbdata

import (
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	return loc
}

// Reference week, Eastern: Sunday 2026-08-30 through Saturday 2026-09-05.
func TestNextSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.August, day, hour, minute, 0, 0, eastern)
	}

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		// Thursday, Friday, Saturday: every 15 minutes on the quarter hour.
		{"thursday afternoon", at(27, 14, 0), at(27, 14, 15)},
		{"friday morning", at(28, 9, 7), at(28, 9, 15)},
		{"saturday kickoff window", at(29, 15, 31), at(29, 15, 45)},

		// Everything else: every 30 minutes on the half hour.
		{"sunday afternoon", at(30, 13, 5), at(30, 13, 30)},
		{"monday morning", at(31, 8, 45), at(31, 9, 0)},

		// Overnight: hourly on the hour.
		{"monday small hours", at(31, 3, 20), at(31, 4, 0)},
		{"the last overnight run hands off at 6am", at(31, 5, 0), at(31, 6, 0)},

		// Sunday holds the game-day pace until 2am, for Saturday's late games.
		{"sunday 12:40am is still game day", at(30, 0, 40), at(30, 0, 45)},
		{"sunday 1:50am steps to the 2am handover", at(30, 1, 50), at(30, 2, 0)},
		{"sunday 2am has dropped to hourly", at(30, 2, 0), at(30, 3, 0)},
		{"sunday 5am hands off to the daytime pace", at(30, 5, 30), at(30, 6, 0)},

		// Midnight rollovers carry into the next day's rules.
		{"saturday night rolls into sunday", at(29, 23, 50), at(30, 0, 0)},
		{"wednesday night rolls into thursday overnight", at(26, 23, 40), at(27, 0, 0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextSync(tc.now, eastern); !got.Equal(tc.want) {
				t.Errorf("NextSync(%s) = %s, want %s",
					tc.now.Format(time.RFC1123), got.Format(time.RFC1123), tc.want.Format(time.RFC1123))
			}
		})
	}
}

// Runs sit on the clock, not on whenever the process happened to start, so two
// servers booted a minute apart converge on the same schedule.
func TestNextSyncIsAlignedToTheClock(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	// Saturday, 15-minute grid.
	base := time.Date(2026, time.August, 29, 14, 0, 0, 0, eastern)
	want := base.Add(15 * time.Minute)

	for offset := 1; offset < 15; offset++ {
		now := base.Add(time.Duration(offset) * time.Minute)
		if got := NextSync(now, eastern); !got.Equal(want) {
			t.Fatalf("NextSync(%s) = %s, want %s", now.Format(time.Kitchen), got.Format(time.Kitchen), want.Format(time.Kitchen))
		}
	}
}

// The schedule is about where the games are, so it has to be read in the
// configured zone. The server runs in UTC, where a Saturday-evening kickoff on
// the East Coast is already Sunday.
func TestNextSyncIsReadInTheConfiguredZone(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	// Saturday 8:05pm Eastern is Sunday 00:05 UTC.
	kickoff := time.Date(2026, time.August, 29, 20, 5, 0, 0, eastern)

	if got, want := SyncDelay(kickoff, eastern), 10*time.Minute; got != want {
		t.Errorf("eastern: SyncDelay = %v, want %v (saturday night is game day)", got, want)
	}
	// Read as UTC it is Sunday 00:05, which is inside the Sunday late-night
	// window and so still 15-minute paced -- but gridded off the wrong midnight.
	if got, want := SyncDelay(kickoff, time.UTC), 10*time.Minute; got != want {
		t.Errorf("utc sanity: SyncDelay = %v, want %v", got, want)
	}
	// The real divergence: Sunday 3am UTC is overnight, while the same instant
	// is Saturday 11pm Eastern, mid-slate.
	lateSlate := time.Date(2026, time.August, 29, 23, 5, 0, 0, eastern)
	if got, want := SyncDelay(lateSlate, eastern), 10*time.Minute; got != want {
		t.Errorf("eastern: SyncDelay = %v, want %v (saturday 11pm is game day)", got, want)
	}
	if got, want := SyncDelay(lateSlate, time.UTC), 55*time.Minute; got != want {
		t.Errorf("utc: SyncDelay = %v, want %v (sunday 3am utc reads as overnight)", got, want)
	}
}

func TestSyncDelayDefaultsToUTCWhenLocationIsNil(t *testing.T) {
	// Monday 12:05 UTC: an off day, outside the overnight window, either way.
	now := time.Date(2026, time.August, 31, 12, 5, 0, 0, time.UTC)
	if got, want := SyncDelay(now, nil), 25*time.Minute; got != want {
		t.Errorf("SyncDelay(nil location) = %v, want %v", got, want)
	}
}

// A DST change must not knock the runs off the wall-clock grid, and must never
// produce a wait the scheduler cannot use.
func TestNextSyncAcrossDSTTransitions(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	transitions := []struct {
		name  string
		start time.Time
	}{
		// Spring forward: 2am does not exist on 2026-03-08.
		{"spring forward", time.Date(2026, time.March, 7, 20, 0, 0, 0, eastern)},
		// Fall back: 1am-2am happens twice on 2026-11-01. Starting the night
		// before is the point — walking into the repeated hour is what once sent
		// the schedule backwards and pinned the sync at its floor until 2am.
		{"fall back", time.Date(2026, time.October, 31, 20, 0, 0, 0, eastern)},
	}

	for _, tc := range transitions {
		t.Run(tc.name, func(t *testing.T) {
			now := tc.start
			end := tc.start.AddDate(0, 0, 2)

			for now.Before(end) {
				next := NextSync(now, eastern)
				if !next.After(now) {
					t.Fatalf("NextSync(%s) = %s, which is not in the future",
						now.Format(time.RFC1123), next.Format(time.RFC1123))
				}
				// Every run lands on a quarter hour, whatever the offset did.
				if m := next.In(eastern).Minute(); m%15 != 0 {
					t.Fatalf("run at %s is off the grid (minute %d)", next.Format(time.RFC1123), m)
				}
				now = next
			}
		})
	}
}

// Walking a fortnight at the real cadence: every run lands on the grid its own
// tier defines, so the tiers cannot silently overlap.
func TestNextSyncAlwaysLandsOnItsOwnGrid(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	now := time.Date(2026, time.August, 30, 0, 0, 0, 0, eastern)
	end := now.AddDate(0, 0, 14)

	for now.Before(end) {
		step := int(intervalAt(now) / time.Minute)
		next := NextSync(now, eastern)

		if got := next.Sub(now); got <= 0 || got > time.Hour {
			t.Fatalf("NextSync(%s) is %v away, outside the range any tier allows",
				now.Format(time.RFC1123), got)
		}
		if m := next.In(eastern).Hour()*60 + next.In(eastern).Minute(); m%step != 0 && m != 0 {
			t.Fatalf("run at %s is not a multiple of its %d-minute interval",
				next.Format(time.RFC1123), step)
		}
		now = next
	}
}

// The cadence is a spending plan, not just a freshness setting, so the
// arithmetic behind it is worth pinning down: walking real calendar months at
// the real schedule keeps a future tweak to the intervals from quietly
// overrunning CFBD's monthly allowance.
//
// The cap leaves room underneath the 5,000 limit for the calendar job and the
// occasional manual seed.
func TestSyncCadenceStaysWithinMonthlyCallBudget(t *testing.T) {
	const (
		callsPerRun     = 2
		monthlyCallsCap = 4000
	)

	eastern := mustLoad(t, "America/New_York")

	// Two years, so the worst alignment of weekdays to month length is covered.
	for year := 2026; year <= 2027; year++ {
		for month := time.January; month <= time.December; month++ {
			start := time.Date(year, month, 1, 0, 0, 0, 0, eastern)
			end := start.AddDate(0, 1, 0)

			runs := 0
			for now := start; ; runs++ {
				now = NextSync(now, eastern)
				if !now.Before(end) {
					break
				}
			}

			if calls := runs * callsPerRun; calls > monthlyCallsCap {
				t.Errorf("%d-%02d: %d runs = %d calls, over the %d budget",
					year, month, runs, calls, monthlyCallsCap)
			}
		}
	}
}
