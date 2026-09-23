package cbbdata

import (
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/apibudget"
	"github.com/brian/paper-betting-with-friends/internal/config"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q) error = %v", name, err)
	}
	return loc
}

func TestInSeason(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"the night before it opens", time.Date(2026, time.October, 24, 23, 59, 0, 0, eastern), false},
		{"the day it opens", time.Date(2026, time.October, 25, 0, 0, 0, 0, eastern), true},
		{"new year's eve", time.Date(2026, time.December, 31, 23, 59, 0, 0, eastern), true},
		{"new year's day", time.Date(2027, time.January, 1, 0, 0, 0, 0, eastern), true},
		{"the last night of it", time.Date(2027, time.April, 15, 23, 59, 0, 0, eastern), true},
		{"the day after it closes", time.Date(2027, time.April, 16, 0, 0, 0, 0, eastern), false},
		{"midsummer", time.Date(2027, time.July, 4, 12, 0, 0, 0, eastern), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InSeason(tt.at, eastern); got != tt.want {
				t.Errorf("InSeason(%s) = %v, want %v", tt.at.Format(time.DateTime), got, tt.want)
			}
		})
	}
}

func TestNextGamesSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")
	const interval = 15 * time.Minute

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"in season, the next point on the interval's grid",
			time.Date(2026, time.December, 5, 19, 7, 0, 0, eastern),
			time.Date(2026, time.December, 5, 19, 15, 0, 0, eastern)},
		{"out of season, the next midnight",
			time.Date(2026, time.July, 4, 19, 7, 0, 0, eastern),
			time.Date(2026, time.July, 5, 0, 0, 0, 0, eastern)},
		{"the last in-season run lands on the midnight that closes it",
			time.Date(2027, time.April, 15, 23, 50, 0, 0, eastern),
			time.Date(2027, time.April, 16, 0, 0, 0, 0, eastern)},
		{"and the run on that midnight waits a day",
			time.Date(2027, time.April, 16, 0, 0, 0, 0, eastern),
			time.Date(2027, time.April, 17, 0, 0, 0, 0, eastern)},
		{"the off-season run lands on the midnight that opens the season",
			time.Date(2026, time.October, 24, 0, 0, 0, 0, eastern),
			time.Date(2026, time.October, 25, 0, 0, 0, 0, eastern)},
		{"and the run on that midnight takes the season's rate",
			time.Date(2026, time.October, 25, 0, 0, 0, 0, eastern),
			time.Date(2026, time.October, 25, 0, 15, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextGamesSync(tt.now, eastern, interval); !got.Equal(tt.want) {
				t.Errorf("NextGamesSync(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

// The server runs in UTC. The season's first day is a calendar fact about
// where the games are, so it has to be read in the schedule's location: at
// 10pm Eastern on October 24 it is already October 25 in UTC, and reading it
// there would put the fast rate on a day early.
func TestNextGamesSyncReadsTheSeasonInItsLocation(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")
	now := time.Date(2026, time.October, 25, 2, 0, 0, 0, time.UTC)

	want := time.Date(2026, time.October, 25, 0, 0, 0, 0, eastern)
	if got := NextGamesSync(now, eastern, 15*time.Minute); !got.Equal(want) {
		t.Errorf("NextGamesSync(%v, Eastern) = %v, want Eastern midnight %v", now, got, want)
	}

	want = time.Date(2026, time.October, 25, 2, 15, 0, 0, time.UTC)
	if got := NextGamesSync(now, nil, 15*time.Minute); !got.Equal(want) {
		t.Errorf("NextGamesSync(%v, nil) = %v, want UTC's own grid at %v", now, got, want)
	}
}

// The lines run looks up games the games run writes, so in every slot it has
// to come after the games run and before the next one -- at the rate of every
// interval configuration accepts, and across both edges of the season.
func TestLinesRunBetweenOneGamesRunAndTheNext(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	for _, interval := range validIntervals(t) {
		for _, start := range []time.Time{
			time.Date(2026, time.October, 23, 0, 0, 0, 0, eastern),
			time.Date(2027, time.April, 14, 0, 0, 0, 0, eastern),
		} {
			end := start.AddDate(0, 0, 4)
			for games := NextGamesSync(start, eastern, interval); games.Before(end); {
				next := NextGamesSync(games, eastern, interval)
				lines := NextLinesSync(games, eastern, interval)
				if !lines.After(games) || !lines.Before(next) {
					t.Fatalf("interval %v: games runs at %v and %v, lines at %v -- not between them",
						interval, games, next, lines)
				}
				games = next
			}
		}
	}
}

// validIntervals is every interval config.Validate accepts.
func validIntervals(t *testing.T) []time.Duration {
	t.Helper()
	var intervals []time.Duration
	for mins := config.MinCBBSyncIntervalMins; mins <= config.MaxCBBSyncIntervalMins; mins++ {
		if config.ValidCBBSyncInterval(mins) {
			intervals = append(intervals, time.Duration(mins)*time.Minute)
		}
	}
	if len(intervals) == 0 {
		t.Fatal("config accepts no basketball interval at all")
	}
	return intervals
}

func TestDelaysAreAlwaysPositiveAcrossDST(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	// Both transitions fall in the season, so both are crossed at the fast
	// rate. A whole day either side of each, stepped a minute at a time.
	for _, start := range []time.Time{
		time.Date(2026, time.March, 7, 0, 0, 0, 0, eastern),
		time.Date(2026, time.November, 1, 0, 0, 0, 0, eastern),
	} {
		for offset := range 48 * 60 {
			now := start.Add(time.Duration(offset) * time.Minute)
			for _, interval := range validIntervals(t) {
				for name, delay := range map[string]time.Duration{
					"games": GamesDelay(now, eastern, interval),
					"lines": LinesDelay(now, eastern, interval),
				} {
					if delay < minDelay {
						t.Fatalf("%s delay at %v, interval %v = %v, under the %v floor", name, now, interval, delay, minDelay)
					}
					// The repeated hour on the fall-back day stretches one step
					// by an hour of absolute time; anything longer skipped a
					// cycle.
					if delay > interval+time.Hour {
						t.Fatalf("%s delay at %v, interval %v = %v, longer than a step stretched by the fall-back",
							name, now, interval, delay)
					}
				}
			}
		}
	}
}

// The basketball jobs spend against the same meter as football, so this walks
// real months at the real schedules, the way the football test does, and holds
// them to basketball's share of the plan in internal/apibudget.
//
// It counts at the shortest interval configuration accepts, not the default:
// the default staying affordable says nothing about whether the knob is safe.
// At ten minutes the worst month is ~9,000 -- 4,464 runs of each job in a
// 31-day month and 80 restart calls. The default of fifteen is ~6,000.
//
// And it holds the off-season months on their own, much lower cap, since the
// throttle is the half of this change that saves anything: without it a July
// costs what a January does, and still fits under the in-season cap.
func TestBasketballCadenceStaysWithinMonthlyCallBudget(t *testing.T) {
	const (
		// cbb-games and cbb-lines are one request each a run.
		callsPerRun = 1

		// As in the football test: a worst case, not an observed rate. Both
		// jobs carry RunOnStart only when the process starts out of season,
		// so each restart there is one extra run of each. They are counted in
		// every month anyway, which overstates a season month by 80.
		restartsPerMonth = 40

		// A month entirely out of season is a run a day of each job plus
		// restarts: ~140. The cap is loose on purpose -- the regression it
		// exists for is the throttle disappearing, which costs thousands.
		offSeasonCap = 250
	)

	eastern := mustLoad(t, "America/New_York")
	interval := config.MinCBBSyncIntervalMins * time.Minute

	countRuns := func(start, end time.Time, next func(time.Time) time.Time) (runs, inSeason int) {
		for now := next(start); now.Before(end); now = next(now) {
			runs++
			if InSeason(now, eastern) {
				inSeason++
			}
		}
		return runs, inSeason
	}

	for year := 2026; year <= 2027; year++ {
		for month := time.January; month <= time.December; month++ {
			start := time.Date(year, month, 1, 0, 0, 0, 0, eastern)
			end := start.AddDate(0, 1, 0)

			gamesRuns, gamesInSeason := countRuns(start, end, func(now time.Time) time.Time {
				return NextGamesSync(now, eastern, interval)
			})
			linesRuns, linesInSeason := countRuns(start, end, func(now time.Time) time.Time {
				return NextLinesSync(now, eastern, interval)
			})

			calls := callsPerRun * (gamesRuns + linesRuns + 2*restartsPerMonth)
			if calls > apibudget.Basketball {
				t.Errorf("%d-%02d: %d games and %d lines runs plus %d restarts = %d calls, over basketball's %d share",
					year, month, gamesRuns, linesRuns, restartsPerMonth, calls, apibudget.Basketball)
			}
			if gamesInSeason+linesInSeason == 0 && calls > offSeasonCap {
				t.Errorf("%d-%02d is out of season but costs %d calls, over the %d an off-season month should",
					year, month, calls, offSeasonCap)
			}
		}
	}
}
