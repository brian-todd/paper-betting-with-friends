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

// Scoreboard cadence.
//
// The scoreboard is the live feed -- the clock, the period, the score as it
// moves -- so while a game is being played it is polled far harder than games
// and lines, which exist to pick up a schedule change and a line move.
//
// The rest of the time it drops to a pulse. A five-minute poll around the clock
// is ~8,600 requests a month per division against an allowance of 30,000, and
// almost all of it buys nothing: a Tuesday in October has no football being
// played, and neither does 9am on a Saturday. Polling only while there is
// something on the feed costs ~2,700 in October and ~720 in June instead. What decides the rate is
// therefore whether a game is on, not whether the calendar says it is a season
// -- see ScoreboardState.
const (
	// scoreboardLiveInterval is the rate while a game is being played.
	scoreboardLiveInterval = 5 * time.Minute

	// scoreboardIdleInterval is the rate while none is.
	//
	// It caps the wait even when the next kickoff is days away, because the
	// games table can be wrong -- a game added, or moved -- and this schedule is
	// derived from it. An hourly floor costs ~720 calls a month and bounds how
	// long a wrong scheduled_at can hide a live game.
	scoreboardIdleInterval = time.Hour

	// maxGameDuration is how long after kickoff a game is still presumed to be
	// being played. The longest live game observed over a week of samples was
	// ~5h15m.
	maxGameDuration = 6 * time.Hour
)

// ScoreboardState is what the schedule needs to know about the games table.
//
// The scoreboard covers the current CFB week only, so at a week rollover it
// holds no future kickoff at all and cannot schedule itself from its own
// contents. These facts come from the database, which spans the whole season.
type ScoreboardState struct {
	// Active reports a game that is being played, or that kicked off recently
	// enough to still be.
	Active bool

	// NextKickoff is the earliest future kickoff, nil when none is known.
	NextKickoff *time.Time
}

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
	return nextOnGrid(t, loc, intervalAt(t))
}

// NextScoreboardSync returns the next instant the live scoreboard sync should
// run after now.
//
// state is what the games table says is being played and what is next to kick
// off. The caller resolves it, because answering it means reading the database
// and the schedule arithmetic here stays testable without one.
//
// There is deliberately no margin before a kickoff. Waking early buys nothing
// -- there is no score before kickoff -- and the wake lands *on* the kickoff,
// at which point scheduled_at <= now makes the next state Active and the live
// rate takes over.
func NextScoreboardSync(now time.Time, loc *time.Location, state ScoreboardState) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	t := now.In(loc)
	if state.Active {
		return nextOnGrid(t, loc, scoreboardLiveInterval)
	}

	// The idle wait stays on the wall-clock grid rather than being computed as
	// now.Add(interval), for the reason given on NextSync: a restart must not
	// shift the whole schedule onto an arbitrary offset. A kickoff sooner than
	// the next grid point brings the run forward to it.
	next := nextOnGrid(t, loc, scoreboardIdleInterval)
	if state.NextKickoff != nil {
		if kickoff := state.NextKickoff.In(loc); kickoff.After(t) && kickoff.Before(next) {
			return kickoff
		}
	}
	return next
}

// ScoreboardDelay returns how long to wait after now before the next scoreboard
// sync.
func ScoreboardDelay(now time.Time, loc *time.Location, state ScoreboardState) time.Duration {
	if delay := NextScoreboardSync(now, loc, state).Sub(now); delay > minDelay {
		return delay
	}
	return minDelay
}

// nextOnGrid advances t, which is already expressed in loc, to the next
// multiple of interval since midnight.
//
// Every point where a cadence changes — midnight, 2am, 6am — is a whole hour,
// and so is already on the grid of every interval used here, which is what
// keeps this single step from jumping over a transition.
func nextOnGrid(t time.Time, loc *time.Location, interval time.Duration) time.Time {
	step := int(interval / time.Minute)

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

// Daily pre-game context cadence.
//
// Everything on these two jobs refreshes once a day, which is the ceiling this
// page's data was specified against. SP+, FPI, CORE, records, ATS and season
// efficiency all move once a week, after Saturday's games, so a daily refresh is
// already more than the data justifies. The forecast moves faster than that and
// is the one place the ceiling costs something -- a Saturday-evening kickoff is
// read off a forecast taken that morning.
//
// Eight requests a day between them is ~240 a month against an allowance of
// 30,000.
const (
	// teamStatsHour is the local hour the team-season refresh targets. Early
	// enough that it is never competing with a slate, and a fixed wall-clock
	// hour rather than a flat 24-hour interval so a restart does not permanently
	// move the run to whatever time the process happened to come up.
	teamStatsHour = 4

	// gameContextHour staggers the per-game refresh an hour after the
	// team-season one. Nothing breaks if they overlap -- they write different
	// tables -- but two jobs waking together against a metered API for no
	// reason is a habit worth not forming.
	gameContextHour = 5
)

// nextDailyAt returns the next instant at the given local hour after now.
func nextDailyAt(now time.Time, loc *time.Location, hour int) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	t := now.In(loc)
	next := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, loc)
	if !next.After(t) {
		// AddDate, not Add(24*time.Hour): a DST day is 23 or 25 hours long, and
		// the target is a wall-clock hour.
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// NextTeamStatsSync returns the next instant the daily team stats sync should
// run after now.
//
// Nothing depends on the exact hour -- unlike the scoreboard, there is no event
// this has to land before. A run that drifts costs a rating a day staler than it
// had to be, on a number that changes weekly.
func NextTeamStatsSync(now time.Time, loc *time.Location) time.Time {
	return nextDailyAt(now, loc, teamStatsHour)
}

// TeamStatsDelay returns how long to wait after now before the next team stats
// sync.
func TeamStatsDelay(now time.Time, loc *time.Location) time.Duration {
	if delay := NextTeamStatsSync(now, loc).Sub(now); delay > minDelay {
		return delay
	}
	return minDelay
}

// NextGameContextSync returns the next instant the daily per-game context sync
// should run after now.
func NextGameContextSync(now time.Time, loc *time.Location) time.Time {
	return nextDailyAt(now, loc, gameContextHour)
}

// GameContextDelay returns how long to wait after now before the next per-game
// context sync.
func GameContextDelay(now time.Time, loc *time.Location) time.Duration {
	if delay := NextGameContextSync(now, loc).Sub(now); delay > minDelay {
		return delay
	}
	return minDelay
}
