package cfbdata

import "time"

// Sync cadence.
//
// CFBD's free tier allows 5,000 calls a month and each sync run spends two of
// them (/games and /lines), so the schedule is a budget, not just a freshness
// knob. A flat 15-minute poll costs ~5,760 calls a month on its own — over the
// whole allowance before the calendar job takes its share.
//
// The cadence follows the football week instead: fast on the days games are
// played, slower midweek, and hourly overnight when nothing is kicking off and
// no book is moving a number.
const (
	// gameDayInterval applies Thursday through Saturday, when kickoffs are
	// dense enough that a stale score is noticeable on the page.
	gameDayInterval = 15 * time.Minute

	// offDayInterval applies the rest of the week, where the only thing moving
	// is the occasional line.
	offDayInterval = 30 * time.Minute

	// overnightInterval keeps a slow pulse through the small hours: enough to
	// pick up a finished West Coast game or an early line, cheap enough to
	// ignore.
	overnightInterval = time.Hour

	// dayStartHour is when the overnight pace gives way to the daytime one.
	dayStartHour = 6

	// sundayLateNightHour is how far into Sunday morning the Saturday game-day
	// pace carries before dropping to overnight.
	sundayLateNightHour = 2

	// minDelay floors the computed wait. Nothing should reach it; it is here so
	// an unforeseen clock case cannot become a hot loop against a metered API.
	minDelay = time.Minute
)

// NextSync returns the next instant the football sync should run after now.
//
// Runs sit on a wall-clock grid measured from midnight rather than from
// whenever the process last started, so "the :15 sync" means the same thing
// across restarts and deploys, and a redeploy cannot shift the whole schedule
// onto an arbitrary offset.
//
// The grid is read in loc, not UTC: "Saturday" and "overnight" are calendar
// facts about where the games are, and the server runs in UTC, so deciding this
// against time.Now() alone would shift the schedule by the UTC offset — slowing
// the sync through Saturday evening kickoffs on the US East Coast, which is
// exactly when it matters most.
func NextSync(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	t := now.In(loc)
	step := int(intervalAt(t) / time.Minute)

	// Advance to the next multiple of step since midnight. Every point where the
	// cadence changes — midnight, 2am, 6am — is a whole hour, and so is already
	// on the grid of every interval below, which is what keeps this single step
	// from jumping over a transition.
	minutes := t.Hour()*60 + t.Minute()
	next := (minutes/step + 1) * step

	// time.Date normalises the minute overflow past midnight into the next day,
	// and resolves the result against loc's offset for that date — which is what
	// keeps the grid on the wall clock across a DST change rather than drifting
	// by an hour.
	//
	// The walk forward is for the other kind of transition. When the clocks go
	// back, an hour of wall-clock readings happens twice, and Go resolves the
	// ambiguous ones to the first pass; during the second pass the next grid
	// point by wall clock is therefore still in the past. Left alone that hands
	// the scheduler a negative delay and polls flat out until the hour clears.
	for {
		candidate := time.Date(t.Year(), t.Month(), t.Day(), 0, next, 0, 0, loc)
		if candidate.After(t) {
			return candidate
		}
		next += step
	}
}

// SyncDelay returns how long to wait after now before the next football sync.
func SyncDelay(now time.Time, loc *time.Location) time.Duration {
	if delay := NextSync(now, loc).Sub(now); delay > minDelay {
		return delay
	}
	return minDelay
}

// intervalAt is the polling interval in force at t.
func intervalAt(t time.Time) time.Duration {
	day, hour := t.Weekday(), t.Hour()

	if hour < dayStartHour {
		// Saturday football runs long: late West Coast kickoffs are still being
		// played, and settled, well past midnight Eastern. Sunday therefore
		// holds the game-day pace until 2am before dropping to overnight.
		if day == time.Sunday && hour < sundayLateNightHour {
			return gameDayInterval
		}
		return overnightInterval
	}

	switch day {
	case time.Thursday, time.Friday, time.Saturday:
		return gameDayInterval
	default:
		return offDayInterval
	}
}
