package cbbdata

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/timeutil"
)

// Basketball cadence.
//
// Four jobs read four things at four rates, and all of them spend from the
// monthly allowance football also draws on:
//
//   - cbb-scoreboard is the live feed: status, score and clock. It finalizes
//     games, so it is what settlement normally follows.
//   - cbb-games is the backstop. The scoreboard only covers a day or so either
//     side of now, and it never writes postponed or cancelled, so /games still
//     runs hourly in season rather than at football's six hours.
//   - cbb-lines follows CBB_SYNC_INTERVAL_MINS, because lines are what a bettor
//     reads before tip-off and nothing else refreshes them.
//   - cbb-schedule reaches well past the three-day incremental window, once a
//     day, so the day picker has games to show and a game stuck unfinished by an
//     outage is repaired.
//
// Between seasons each runs once a day. The incremental window reaches three
// days ahead, so anything faster asks for an empty span.
const (
	// offSeasonInterval is the rate between seasons.
	offSeasonInterval = 24 * time.Hour

	// gamesInterval is the /games rate through the season.
	gamesInterval = time.Hour

	// scoreboardLiveInterval is the rate while a game is being played.
	scoreboardLiveInterval = 5 * time.Minute

	// scoreboardIdleInterval is the in-season rate while none is. It caps the
	// wait even when the next tip-off is hours away, because the games table can
	// be wrong and this schedule is derived from it.
	scoreboardIdleInterval = time.Hour

	// maxGameDuration is how long after tip-off a game is still presumed to be
	// being played. It bounds what one stuck row can cost: a game the feed never
	// calls final keeps the live rate on for this long, and no longer.
	maxGameDuration = 4 * time.Hour

	// scheduleSyncHour is the local hour cbb-schedule runs, clear of any slate.
	scheduleSyncHour = 4

	// minDelay floors the computed wait, so no clock case can become a hot
	// loop against a metered API.
	minDelay = time.Minute
)

// The season, as calendar days in the schedule's location.
//
// The committed captures run from 2025-11-03 to 2026-04-07. The season opens
// here a week before the earliest tip, so the first games are already on the
// fast rate when the three-day window reaches them and a season starting a few
// days early is still covered, and closes a week after the championship. A date
// a few days wrong costs freshness, not data: the daily off-season run still
// fetches whatever is played outside it.
const (
	seasonOpens  = 10_25 // October 25
	seasonCloses = 4_15  // April 15
)

// InSeason reports whether now falls on a day basketball is being played,
// read as a calendar day in loc. The season spans New Year, so that is any day
// on or after it opens or on or before it closes.
func InSeason(now time.Time, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	t := now.In(loc)
	day := int(t.Month())*100 + t.Day()
	return day >= seasonOpens || day <= seasonCloses
}

// ScoreboardState is what the scoreboard schedule needs to know about the games
// table. The caller resolves it, because answering it means reading the database
// and the arithmetic here stays testable without one.
type ScoreboardState struct {
	// Active reports a game that is being played, or that tipped off recently
	// enough to still be.
	Active bool

	// NextTip is the earliest future tip-off, nil when none is known.
	NextTip *time.Time
}

// linesLag is how far the lines grid sits behind the games grid.
//
// syncLines skips a line for a game it cannot find, so a lines run landing on
// the same instant as a games run could race it. The lag has to stay under the
// shortest interval configuration accepts, or it would no longer separate them.
const linesLag = 5 * time.Minute

// nextAtRate returns the next point on rate's grid during the season, and the
// next midnight outside it.
//
// The season's edges are midnights, which lie on every grid, so the step cannot
// jump over one: the last in-season run lands on the midnight that closes it,
// and the first run after the off-season midnight that opens it takes the fast
// rate. That holds because rate divides a day, which config.Validate requires of
// the one rate that is configurable.
func nextAtRate(now time.Time, loc *time.Location, rate time.Duration) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	if !InSeason(now, loc) {
		rate = offSeasonInterval
	}
	return timeutil.NextOnGrid(now.In(loc), loc, rate)
}

// NextGamesSync returns the next instant the basketball games sync should run
// after now.
func NextGamesSync(now time.Time, loc *time.Location) time.Time {
	return nextAtRate(now, loc, gamesInterval)
}

// NextLinesSync returns the next instant the basketball lines sync should run
// after now: interval's grid during the season, linesLag later.
func NextLinesSync(now time.Time, loc *time.Location, interval time.Duration) time.Time {
	return nextAtRate(now.Add(-linesLag), loc, interval).Add(linesLag)
}

// NextScoreboardSync returns the next instant the live scoreboard sync should
// run after now.
//
// There is no margin before a tip-off, because there is no score before one. The
// wake lands on it, scheduled_at <= now makes the next state Active, and the live
// rate takes over.
//
// The idle wait stays on the wall-clock grid so a restart cannot shift the
// schedule onto an arbitrary offset. Out of season it is a day rather than an
// hour, which is what keeps an empty July from costing 720 requests.
func NextScoreboardSync(now time.Time, loc *time.Location, state ScoreboardState) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	t := now.In(loc)
	if state.Active {
		return timeutil.NextOnGrid(t, loc, scoreboardLiveInterval)
	}

	next := nextAtRate(t, loc, scoreboardIdleInterval)
	if state.NextTip != nil {
		if tip := state.NextTip.In(loc); tip.After(t) && tip.Before(next) {
			return tip
		}
	}
	return next
}

// NextScheduleSync returns the next instant the wide schedule sync should run
// after now: scheduleSyncHour, every day of the year, since this is the job that
// finds next season's games before the season's rates begin.
func NextScheduleSync(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	t := now.In(loc)
	next := time.Date(t.Year(), t.Month(), t.Day(), scheduleSyncHour, 0, 0, 0, loc)
	if !next.After(t) {
		next = time.Date(t.Year(), t.Month(), t.Day()+1, scheduleSyncHour, 0, 0, 0, loc)
	}
	return next
}

// GamesDelay returns how long to wait after now before the next basketball
// games sync.
func GamesDelay(now time.Time, loc *time.Location) time.Duration {
	return atLeastMinDelay(NextGamesSync(now, loc).Sub(now))
}

// LinesDelay returns how long to wait after now before the next basketball
// lines sync.
func LinesDelay(now time.Time, loc *time.Location, interval time.Duration) time.Duration {
	return atLeastMinDelay(NextLinesSync(now, loc, interval).Sub(now))
}

// ScoreboardDelay returns how long to wait after now before the next scoreboard
// sync.
func ScoreboardDelay(now time.Time, loc *time.Location, state ScoreboardState) time.Duration {
	return atLeastMinDelay(NextScoreboardSync(now, loc, state).Sub(now))
}

// ScheduleDelay returns how long to wait after now before the next wide schedule
// sync.
func ScheduleDelay(now time.Time, loc *time.Location) time.Duration {
	return atLeastMinDelay(NextScheduleSync(now, loc).Sub(now))
}

func atLeastMinDelay(d time.Duration) time.Duration {
	return max(d, minDelay)
}
