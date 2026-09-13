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

func TestNextScoreboardSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.September, day, hour, minute, 0, 0, eastern)
	}

	tests := []struct {
		name     string
		now      time.Time
		inSeason bool
		want     time.Time
	}{
		// In season the rate is flat: five minutes, day and night, because that
		// is what makes a live score on the page a live score.
		{"saturday afternoon", at(5, 15, 31), true, at(5, 15, 35)},
		{"saturday 3am", at(5, 3, 2), true, at(5, 3, 5)},
		{"tuesday morning", at(8, 9, 0), true, at(8, 9, 5)},
		{"lands on the grid, not on the offset", at(5, 15, 33), true, at(5, 15, 35)},

		// Out of season it is an hourly pulse. Nothing is played between the
		// last bowl and next August.
		{"july afternoon", at(5, 15, 31), false, at(5, 16, 0)},
		{"july on the hour", at(5, 16, 0), false, at(5, 17, 0)},

		// Midnight rolls into the next day rather than overflowing the minute.
		{"rolls past midnight", at(5, 23, 58), true, at(6, 0, 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextScoreboardSync(tt.now, eastern, tt.inSeason); !got.Equal(tt.want) {
				t.Errorf("NextScoreboardSync(%v, inSeason=%v) = %v, want %v", tt.now, tt.inSeason, got, tt.want)
			}
		})
	}
}

// The scoreboard grid has to stay on the wall clock across both DST
// transitions. Falling back replays an hour of readings, and the naive next
// grid point during the second pass is in the past -- which the scheduler would
// read as a negative delay and poll flat out until the hour cleared.
func TestScoreboardDelayIsAlwaysPositiveAcrossDST(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	// Spring forward 2026-03-08, fall back 2026-11-01. A whole day either side
	// of each, stepped a minute at a time.
	for _, start := range []time.Time{
		time.Date(2026, time.March, 7, 0, 0, 0, 0, eastern),
		time.Date(2026, time.November, 1, 0, 0, 0, 0, eastern),
	} {
		for offset := range 48 * 60 {
			now := start.Add(time.Duration(offset) * time.Minute)
			for _, inSeason := range []bool{true, false} {
				delay := ScoreboardDelay(now, eastern, inSeason)
				if delay <= 0 {
					t.Fatalf("ScoreboardDelay(%v, inSeason=%v) = %v, want positive", now, inSeason, delay)
				}
				// The grid is wall-clock, so the step across the repeated hour
				// on the fall-back day is genuinely an hour longer in absolute
				// time. That is the intended behaviour; what is being checked
				// is that it stops there rather than skipping a whole cycle.
				if delay > scoreboardOffseasonInterval+time.Hour {
					t.Fatalf("ScoreboardDelay(%v, inSeason=%v) = %v, longer than a cadence stretched by the DST fall-back", now, inSeason, delay)
				}
			}
		}
	}
}

// The whole football sync now spends against a 30,000-request monthly
// allowance, and the scoreboard is most of it: a five-minute cadence is ~8,600
// calls per division. This walks real months at the real schedules so that
// raising a rate, or adding a division, cannot quietly overrun the plan.
//
// The cap leaves ~6,000 under the allowance for the calendar job (~800 a month,
// since each run walks every season since 2002), the rankings job (~120) and
// the occasional manual seed, none of which are on a fast cadence.
//
// The daily team stats job is counted here rather than left to the headroom,
// because it is the one whose request count per run could grow: it is four
// resources today and adding a fifth is a one-line change.
//
// Restarts are counted too. Both daily jobs run on startup -- without it a
// daily slot is longer than the gap between two deploys and they never run at
// all -- which makes deploys a line item in the plan rather than free, and one
// that grows with the request count of whatever those jobs fetch.
func TestFootballCadenceStaysWithinMonthlyCallBudget(t *testing.T) {
	const (
		gamesAndLinesCallsPerRun = 2

		// Three ratings, records, ATS and season efficiency.
		teamStatsCallsPerRun = 6

		// Pre-game win probability and the kickoff forecast. Both are fetched
		// season-wide with no week parameter, so neither scales with the
		// schedule -- see GetPregameWinProbabilities and GetGameWeather.
		gameContextCallsPerRun = 2

		// Deploys in a month, as a worst case rather than an observed rate.
		// Both daily jobs carry scheduler.RunOnStart, so each restart buys one
		// extra run of each on top of the schedule -- the point of the flag,
		// and a cost the plan should carry rather than discover. Two a working
		// day is a busier release cadence than this project has ever had.
		restartsPerMonth = 40

		monthlyCallsCap = 24000

		// The budget is checked at the widest division list an operator is
		// likely to configure, not at the FBS-only default -- the default
		// staying affordable says nothing about whether the knob is safe.
		scoreboardDivisions = 2
	)

	eastern := mustLoad(t, "America/New_York")

	countRuns := func(start, end time.Time, next func(time.Time) time.Time) int {
		runs := 0
		for now := start; ; runs++ {
			now = next(now)
			if !now.Before(end) {
				return runs
			}
		}
	}

	for year := 2026; year <= 2027; year++ {
		for month := time.January; month <= time.December; month++ {
			start := time.Date(year, month, 1, 0, 0, 0, 0, eastern)
			end := start.AddDate(0, 1, 0)

			// In season the whole month through, which is the worst case: the
			// offseason rate is twelve times cheaper.
			scoreboardRuns := countRuns(start, end, func(now time.Time) time.Time {
				return NextScoreboardSync(now, eastern, true)
			})
			syncRuns := countRuns(start, end, func(now time.Time) time.Time {
				return NextSync(now, eastern)
			})
			teamStatsRuns := countRuns(start, end, func(now time.Time) time.Time {
				return NextTeamStatsSync(now, eastern)
			})
			gameContextRuns := countRuns(start, end, func(now time.Time) time.Time {
				return NextGameContextSync(now, eastern)
			})

			// A startup run does not replace the scheduled one: both delays
			// target a wall-clock hour, so the job still fires at its usual
			// time afterwards and the restart runs are purely additive.
			restartCalls := restartsPerMonth * (teamStatsCallsPerRun + gameContextCallsPerRun)

			calls := scoreboardRuns*scoreboardDivisions +
				syncRuns*gamesAndLinesCallsPerRun +
				teamStatsRuns*teamStatsCallsPerRun +
				gameContextRuns*gameContextCallsPerRun +
				restartCalls
			if calls > monthlyCallsCap {
				t.Errorf("%d-%02d: %d scoreboard, %d games, %d team-stats and %d game-context runs plus %d restarts = %d calls, over the %d budget",
					year, month, scoreboardRuns, syncRuns, teamStatsRuns, gameContextRuns, restartsPerMonth, calls, monthlyCallsCap)
			}
		}
	}
}

// The daily team stats sync targets a wall-clock hour rather than running on a
// flat 24-hour interval, so a restart does not permanently move it to whatever
// time the process happened to come up.
func TestNextTeamStatsSyncTargetsTheSameHourEveryDay(t *testing.T) {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before the hour waits until it",
			now:  time.Date(2026, 9, 12, 1, 30, 0, 0, eastern),
			want: time.Date(2026, 9, 12, 4, 0, 0, 0, eastern),
		},
		{
			name: "after the hour waits for tomorrow",
			now:  time.Date(2026, 9, 12, 19, 0, 0, 0, eastern),
			want: time.Date(2026, 9, 13, 4, 0, 0, 0, eastern),
		},
		{
			// Exactly on the hour must move to tomorrow, not return a zero
			// delay and re-run immediately against a metered API.
			name: "exactly on the hour waits for tomorrow",
			now:  time.Date(2026, 9, 12, 4, 0, 0, 0, eastern),
			want: time.Date(2026, 9, 13, 4, 0, 0, 0, eastern),
		},
		{
			// A DST day is 23 or 25 hours long. Targeting the wall clock means
			// the run still lands at 4am local, which Add(24*time.Hour) would
			// miss by an hour.
			name: "clocks go forward, still 4am local",
			now:  time.Date(2026, 3, 7, 19, 0, 0, 0, eastern),
			want: time.Date(2026, 3, 8, 4, 0, 0, 0, eastern),
		},
		{
			name: "clocks go back, still 4am local",
			now:  time.Date(2026, 10, 31, 19, 0, 0, 0, eastern),
			want: time.Date(2026, 11, 1, 4, 0, 0, 0, eastern),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextTeamStatsSync(tt.now, eastern)
			if !got.Equal(tt.want) {
				t.Errorf("NextTeamStatsSync(%s) = %s, want %s", tt.now, got, tt.want)
			}
			if got.In(eastern).Hour() != teamStatsHour {
				t.Errorf("landed at hour %d, want %d", got.In(eastern).Hour(), teamStatsHour)
			}
			if delay := TeamStatsDelay(tt.now, eastern); delay < minDelay {
				t.Errorf("TeamStatsDelay = %s, want at least %s", delay, minDelay)
			}
		})
	}
}

// The per-game context job runs on the same daily grid as the team stats one,
// an hour later. The stagger is not load-bearing -- the two write different
// tables -- but it should not quietly disappear either.
func TestNextGameContextSyncStaggersBehindTeamStats(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before both hours waits for its own",
			now:  time.Date(2026, 9, 12, 1, 30, 0, 0, eastern),
			want: time.Date(2026, 9, 12, 5, 0, 0, 0, eastern),
		},
		{
			// The window between the two jobs. Team stats has already run
			// today; game context has not.
			name: "between the two hours still runs today",
			now:  time.Date(2026, 9, 12, 4, 30, 0, 0, eastern),
			want: time.Date(2026, 9, 12, 5, 0, 0, 0, eastern),
		},
		{
			name: "exactly on the hour waits for tomorrow",
			now:  time.Date(2026, 9, 12, 5, 0, 0, 0, eastern),
			want: time.Date(2026, 9, 13, 5, 0, 0, 0, eastern),
		},
		{
			name: "clocks go forward, still 5am local",
			now:  time.Date(2026, 3, 7, 19, 0, 0, 0, eastern),
			want: time.Date(2026, 3, 8, 5, 0, 0, 0, eastern),
		},
		{
			name: "clocks go back, still 5am local",
			now:  time.Date(2026, 10, 31, 19, 0, 0, 0, eastern),
			want: time.Date(2026, 11, 1, 5, 0, 0, 0, eastern),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextGameContextSync(tt.now, eastern)
			if !got.Equal(tt.want) {
				t.Errorf("NextGameContextSync(%s) = %s, want %s", tt.now, got, tt.want)
			}
			if delay := GameContextDelay(tt.now, eastern); delay < minDelay {
				t.Errorf("GameContextDelay = %s, want at least %s", delay, minDelay)
			}
		})
	}

	if teamStatsHour == gameContextHour {
		t.Errorf("the two daily jobs both target hour %d; the stagger is gone", teamStatsHour)
	}
}
