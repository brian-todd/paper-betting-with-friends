package cbbdata

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/timeutil"
)

// Basketball cadence.
//
// The games and lines jobs share one schedule but run as two jobs. They were
// one, and a /games failure returned before /lines was asked, so every games
// error was also a missed line snapshot -- and the footer could not say which
// half was stale.
//
// Football's split went further and slowed /games to four runs a day, which was
// safe because the live scoreboard carries the score and the status. Basketball
// has no scoreboard: /games is where the score, the status and therefore the
// settlement come from, so it keeps the configured rate through the season.
//
// What both jobs lose is the rest of the year. The sync window reaches three
// days ahead, so between seasons every run asked for an empty span -- ~5,760
// requests a month of CBBD's allowance spent on nothing, on the same monthly
// meter as football through the whole of the football season. Out of season
// each job runs once a day instead, which still picks up anything played then
// and settles it within a day.
const (
	// offSeasonInterval is the rate between seasons.
	offSeasonInterval = 24 * time.Hour

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

// linesLag is how far behind each games run the lines run sits.
//
// syncLines skips a line for a game it cannot find, so a lines run racing the
// games run in the same slot drops a newly listed game's lines until the next
// slot -- a day, out of season. The old combined job ran games first and never
// did; lagging the lines keeps that order without making either job wait on
// the other. A games run is seconds against a window of days, so a few minutes
// covers it, and the lag has to stay under the shortest interval configuration
// accepts or the lines run would land after the next games run instead.
const linesLag = 5 * time.Minute

// NextGamesSync returns the next instant the basketball games sync should run
// after now: the next point on interval's grid during the season, the next
// midnight outside it.
//
// The season's edges are midnights, which lie on every grid, so the step
// cannot jump over one: the last in-season run lands on the midnight that
// closes it, and the first run after the off-season midnight that opens it
// takes the fast rate. That holds because interval divides a day, which
// config.Validate requires.
func NextGamesSync(now time.Time, loc *time.Location, interval time.Duration) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	if !InSeason(now, loc) {
		interval = offSeasonInterval
	}
	return timeutil.NextOnGrid(now.In(loc), loc, interval)
}

// NextLinesSync returns the next instant the basketball lines sync should run
// after now: the games schedule, linesLag later.
func NextLinesSync(now time.Time, loc *time.Location, interval time.Duration) time.Time {
	return NextGamesSync(now.Add(-linesLag), loc, interval).Add(linesLag)
}

// GamesDelay returns how long to wait after now before the next basketball
// games sync.
func GamesDelay(now time.Time, loc *time.Location, interval time.Duration) time.Duration {
	return atLeastMinDelay(NextGamesSync(now, loc, interval).Sub(now))
}

// LinesDelay returns how long to wait after now before the next basketball
// lines sync.
func LinesDelay(now time.Time, loc *time.Location, interval time.Duration) time.Duration {
	return atLeastMinDelay(NextLinesSync(now, loc, interval).Sub(now))
}

func atLeastMinDelay(d time.Duration) time.Duration {
	return max(d, minDelay)
}
