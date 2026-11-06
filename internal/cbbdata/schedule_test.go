package cbbdata

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/apibudget"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/fixtures"
	"github.com/brian/paper-betting-with-friends/internal/fixtureserver"
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

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"in season, the next hour",
			time.Date(2026, time.December, 5, 19, 7, 0, 0, eastern),
			time.Date(2026, time.December, 5, 20, 0, 0, 0, eastern)},
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
			time.Date(2026, time.October, 25, 1, 0, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextGamesSync(tt.now, eastern); !got.Equal(tt.want) {
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
	if got := NextGamesSync(now, eastern); !got.Equal(want) {
		t.Errorf("NextGamesSync(%v, Eastern) = %v, want Eastern midnight %v", now, got, want)
	}

	want = time.Date(2026, time.October, 25, 3, 0, 0, 0, time.UTC)
	if got := NextGamesSync(now, nil); !got.Equal(want) {
		t.Errorf("NextGamesSync(%v, nil) = %v, want UTC's own grid at %v", now, got, want)
	}
}

// /games dropping back onto the lines rate would cost ~2,200 a month and still
// fit under the budget cap, so the rate is pinned on its own.
func TestGamesRunHourlyThroughTheSeason(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")
	day := time.Date(2026, time.December, 5, 0, 0, 0, 0, eastern)

	runs := 0
	for now := NextGamesSync(day, eastern); now.Before(day.AddDate(0, 0, 1)); now = NextGamesSync(now, eastern) {
		runs++
	}
	if runs != 23 {
		t.Errorf("%d games runs after midnight in a day, want 23 more on the hour", runs)
	}
}

// The lines grid sits behind every other grid, at every interval configuration
// accepts, so a lines run never lands on the instant a games run does -- which
// is the race the lag exists to prevent.
func TestLinesNeverLandOnAGamesRun(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	for _, interval := range validIntervals(t) {
		for _, start := range []time.Time{
			time.Date(2026, time.October, 23, 0, 0, 0, 0, eastern),
			time.Date(2027, time.April, 14, 0, 0, 0, 0, eastern),
		} {
			end := start.AddDate(0, 0, 4)
			for lines := NextLinesSync(start, eastern, interval); lines.Before(end); lines = NextLinesSync(lines, eastern, interval) {
				if lines.Equal(NextGamesSync(lines.Add(-time.Nanosecond), eastern)) {
					t.Fatalf("interval %v: lines and games both run at %v", interval, lines)
				}
			}
		}
	}
}

func TestNextScoreboardSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")
	evening := time.Date(2026, time.December, 5, 19, 7, 0, 0, eastern)
	july := time.Date(2026, time.July, 4, 19, 7, 0, 0, eastern)

	tests := []struct {
		name  string
		now   time.Time
		state ScoreboardState
		want  time.Time
	}{
		{"a live game, the next five minutes", evening, ScoreboardState{Active: true},
			time.Date(2026, time.December, 5, 19, 10, 0, 0, eastern)},
		{"nothing live and nothing known, the next hour", evening, ScoreboardState{},
			time.Date(2026, time.December, 5, 20, 0, 0, 0, eastern)},
		{"a tip-off before the next hour brings the run forward", evening,
			ScoreboardState{NextTip: new(time.Date(2026, time.December, 5, 19, 30, 0, 0, eastern))},
			time.Date(2026, time.December, 5, 19, 30, 0, 0, eastern)},
		{"a tip-off after the next hour does not", evening,
			ScoreboardState{NextTip: new(time.Date(2026, time.December, 5, 21, 0, 0, 0, eastern))},
			time.Date(2026, time.December, 5, 20, 0, 0, 0, eastern)},
		{"out of season, the next midnight", july, ScoreboardState{},
			time.Date(2026, time.July, 5, 0, 0, 0, 0, eastern)},
		{"but a game out of season still gets the live rate", july, ScoreboardState{Active: true},
			time.Date(2026, time.July, 4, 19, 10, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextScoreboardSync(tt.now, eastern, tt.state); !got.Equal(tt.want) {
				t.Errorf("NextScoreboardSync() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextRankingsSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"a Wednesday waits for Monday evening", time.Date(2026, time.November, 4, 12, 0, 0, 0, eastern),
			time.Date(2026, time.November, 9, 18, 0, 0, 0, eastern)},
		{"Monday evening's run is followed by Tuesday morning's", time.Date(2026, time.November, 9, 18, 0, 0, 0, eastern),
			time.Date(2026, time.November, 10, 6, 0, 0, 0, eastern)},
		{"Tuesday morning's run waits a week", time.Date(2026, time.November, 10, 6, 0, 0, 0, eastern),
			time.Date(2026, time.November, 16, 18, 0, 0, 0, eastern)},
		{"out of season it waits for the first slot in the season", time.Date(2027, time.July, 4, 12, 0, 0, 0, eastern),
			time.Date(2027, time.October, 25, 18, 0, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextRankingsSync(tt.now, eastern); !got.Equal(tt.want) {
				t.Errorf("NextRankingsSync(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestNextRatingsSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before the slot runs that morning", time.Date(2026, time.November, 4, 4, 0, 0, 0, eastern),
			time.Date(2026, time.November, 4, 5, 0, 0, 0, eastern)},
		{"on the slot waits a day", time.Date(2026, time.November, 4, 5, 0, 0, 0, eastern),
			time.Date(2026, time.November, 5, 5, 0, 0, 0, eastern)},
		{"the season spans New Year", time.Date(2026, time.December, 31, 12, 0, 0, 0, eastern),
			time.Date(2027, time.January, 1, 5, 0, 0, 0, eastern)},
		{"the last day of the season is followed by the next season", time.Date(2027, time.April, 15, 5, 0, 0, 0, eastern),
			time.Date(2027, time.October, 25, 5, 0, 0, 0, eastern)},
		{"out of season it waits for the first slot in the season", time.Date(2027, time.July, 4, 12, 0, 0, 0, eastern),
			time.Date(2027, time.October, 25, 5, 0, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextRatingsSync(tt.now, eastern); !got.Equal(tt.want) {
				t.Errorf("NextRatingsSync(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestNextTeamStatsSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"mid-slot goes to the next one", time.Date(2026, time.November, 4, 7, 30, 0, 0, eastern),
			time.Date(2026, time.November, 4, 12, 0, 0, 0, eastern)},
		{"on a slot goes to the next one", time.Date(2026, time.November, 4, 12, 0, 0, 0, eastern),
			time.Date(2026, time.November, 4, 18, 0, 0, 0, eastern)},
		{"out of season it waits for the first slot in the season", time.Date(2027, time.July, 4, 12, 0, 0, 0, eastern),
			time.Date(2027, time.October, 25, 0, 0, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextTeamStatsSync(tt.now, eastern); !got.Equal(tt.want) {
				t.Errorf("NextTeamStatsSync(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestNextBoxScoresSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before the offset, this hour's", time.Date(2026, time.December, 5, 19, 7, 0, 0, eastern),
			time.Date(2026, time.December, 5, 19, 40, 0, 0, eastern)},
		{"on the offset, the next hour's", time.Date(2026, time.December, 5, 19, 40, 0, 0, eastern),
			time.Date(2026, time.December, 5, 20, 40, 0, 0, eastern)},
		{"out of season, the next day's", time.Date(2026, time.July, 4, 19, 7, 0, 0, eastern),
			time.Date(2026, time.July, 5, 0, 40, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextBoxScoresSync(tt.now, eastern); !got.Equal(tt.want) {
				t.Errorf("NextBoxScoresSync(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestNextScheduleSync(t *testing.T) {
	eastern := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before the hour, today", time.Date(2026, time.December, 5, 3, 0, 0, 0, eastern),
			time.Date(2026, time.December, 5, 4, 0, 0, 0, eastern)},
		{"on the hour, tomorrow", time.Date(2026, time.December, 5, 4, 0, 0, 0, eastern),
			time.Date(2026, time.December, 6, 4, 0, 0, 0, eastern)},
		{"out of season it still runs, to find next season's games", time.Date(2026, time.July, 4, 12, 0, 0, 0, eastern),
			time.Date(2026, time.July, 5, 4, 0, 0, 0, eastern)},
		{"across a month end", time.Date(2026, time.December, 31, 5, 0, 0, 0, eastern),
			time.Date(2027, time.January, 1, 4, 0, 0, 0, eastern)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextScheduleSync(tt.now, eastern); !got.Equal(tt.want) {
				t.Errorf("NextScheduleSync(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
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
			for name, delay := range map[string]time.Duration{
				"games":      GamesDelay(now, eastern),
				"scoreboard": ScoreboardDelay(now, eastern, ScoreboardState{}),
				"live":       ScoreboardDelay(now, eastern, ScoreboardState{Active: true}),
				"schedule":   ScheduleDelay(now, eastern),
				"box scores": BoxScoresDelay(now, eastern),
			} {
				if delay < minDelay {
					t.Fatalf("%s delay at %v = %v, under the %v floor", name, now, delay, minDelay)
				}
			}
			for _, interval := range validIntervals(t) {
				delay := LinesDelay(now, eastern, interval)
				if delay < minDelay {
					t.Fatalf("lines delay at %v, interval %v = %v, under the %v floor", now, interval, delay, minDelay)
				}
				// The repeated hour on the fall-back day stretches one step
				// by an hour of absolute time; anything longer skipped a
				// cycle.
				if delay > interval+time.Hour {
					t.Fatalf("lines delay at %v, interval %v = %v, longer than a step stretched by the fall-back",
						now, interval, delay)
				}
			}
		}
	}
}

// The basketball jobs spend against the same meter as football, so this walks
// real months at the real schedules and holds them to basketball's share of the
// plan in internal/apibudget.
//
// It counts lines at the shortest interval configuration accepts, not the
// default: the default staying affordable says nothing about whether the knob is
// safe.
//
// The scoreboard's cost is decided by the games table, so it is driven by the
// real 2025-26 schedule rather than a synthetic slate. Each game is held live for
// assumedGameLength, which is longer than a real game on purpose, so the measured
// ~10,000 for November is a ceiling.
//
// And it holds the off-season months on their own, much lower cap, since a July
// at the in-season rate would still fit under the share.
func TestBasketballCadenceStaysWithinMonthlyCallBudget(t *testing.T) {
	const (
		assumedGameLength = 3 * time.Hour

		// As in the football test: a worst case, not an observed rate. Counted
		// for each of the four jobs, though only cbb-schedule runs on start in
		// season, which overstates a season month by 120.
		restartsPerMonth = 40
		jobs             = 4

		// cbb-ratings (two requests) and cbb-team-stats (one) can run on start only
		// in season, so a restart costs an off-season month nothing for them.
		inSeasonRestartRequests = 3

		// A month entirely out of season is a run a day of each job plus
		// restarts: ~315. The regression this exists for is a throttle
		// disappearing, which costs thousands.
		offSeasonCap = 400
	)

	eastern := mustLoad(t, "America/New_York")
	interval := config.MinCBBSyncIntervalMins * time.Minute
	tips := capturedTips(t)

	stateAt := func(now time.Time) ScoreboardState {
		// The first tip after now, and whether any game within the assumed length
		// before it is still going. tips is sorted.
		next := sort.Search(len(tips), func(i int) bool { return tips[i].After(now) })
		state := ScoreboardState{Active: next > 0 && now.Sub(tips[next-1]) < assumedGameLength}
		if next < len(tips) {
			state.NextTip = &tips[next]
		}
		return state
	}

	count := func(start, end time.Time, next func(time.Time) time.Time) int {
		runs := 0
		for now := next(start); now.Before(end); now = next(now) {
			runs++
		}
		return runs
	}

	// The captured season, then the year after it for the off-season.
	for month := time.Date(2025, time.November, 1, 0, 0, 0, 0, eastern); month.Year() < 2027; month = month.AddDate(0, 1, 0) {
		end := month.AddDate(0, 1, 0)

		scoreboard := count(month, end, func(now time.Time) time.Time { return NextScoreboardSync(now, eastern, stateAt(now)) })
		games := count(month, end, func(now time.Time) time.Time { return NextGamesSync(now, eastern) })
		lines := count(month, end, func(now time.Time) time.Time { return NextLinesSync(now, eastern, interval) })
		schedule := count(month, end, func(now time.Time) time.Time { return NextScheduleSync(now, eastern) })
		rankings := count(month, end, func(now time.Time) time.Time { return NextRankingsSync(now, eastern) })
		// Every check counts as a request, because one game the feed never covers spends one
		// on every check for three days.
		boxScores := count(month, end, func(now time.Time) time.Time { return NextBoxScoresSync(now, eastern) })

		// Two requests a run, one per rating table.
		ratings := 2 * count(month, end, func(now time.Time) time.Time { return NextRatingsSync(now, eastern) })
		teamStats := count(month, end, func(now time.Time) time.Time { return NextTeamStatsSync(now, eastern) })

		calls := scoreboard + games + lines + schedule + rankings + boxScores + ratings + teamStats + jobs*restartsPerMonth
		if InSeason(month, eastern) || InSeason(end.Add(-time.Minute), eastern) {
			calls += inSeasonRestartRequests * restartsPerMonth
		}
		t.Logf("%s: scoreboard %d, games %d, lines %d, schedule %d, rankings %d, box scores %d, ratings %d, team stats %d = %d",
			month.Format("2006-01"), scoreboard, games, lines, schedule, rankings, boxScores, ratings, teamStats, calls)

		if calls > apibudget.Basketball {
			t.Errorf("%s: %d calls is over basketball's %d share", month.Format("2006-01"), calls, apibudget.Basketball)
		}
		if !InSeason(month, eastern) && !InSeason(end.Add(-time.Minute), eastern) && calls > offSeasonCap {
			t.Errorf("%s is out of season but costs %d calls, over the %d an off-season month should",
				month.Format("2006-01"), calls, offSeasonCap)
		}
	}
}

// capturedTips is every tip-off of the captured 2025-26 season that was played,
// sorted. It reads the month windows SeedAll fetches, through the real client.
func capturedTips(t *testing.T) []time.Time {
	t.Helper()

	base, _, stop, err := fixtureserver.Listen(fixtures.CBBD)
	if err != nil {
		t.Fatalf("starting the fixture server: %v", err)
	}
	defer stop()
	client := NewClientAt(base, "")

	season := 2026
	bounds := []string{
		"2025-11-01T00:00:00.000Z", "2025-12-01T00:00:00.000Z", "2026-01-01T00:00:00.000Z",
		"2026-02-01T00:00:00.000Z", "2026-03-01T00:00:00.000Z", "2026-04-01T00:00:00.000Z",
		"2026-05-01T00:00:00.000Z",
	}

	var tips []time.Time
	for i := range len(bounds) - 1 {
		games, err := client.GetGames(context.Background(), GameQueryOpts{
			Season: &season, StartDateRange: &bounds[i], EndDateRange: &bounds[i+1],
		})
		if err != nil {
			t.Fatalf("reading captured games: %v", err)
		}
		for _, g := range games {
			if g.Status == "final" {
				tips = append(tips, g.StartDate)
			}
		}
	}
	if len(tips) == 0 {
		t.Fatal("the capture holds no played games, so the scoreboard would cost nothing")
	}
	slices.SortFunc(tips, time.Time.Compare)
	return tips
}
