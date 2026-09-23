// Package timeutil holds calendar-day arithmetic that has to agree on a
// timezone. Instants stored in the database are absolute, but "today's games"
// is a wall-clock question, so anything answering it needs an explicit
// *time.Location rather than whatever zone the process happens to run in.
package timeutil

import "time"

// StartOfDay returns midnight at the beginning of t's calendar day in loc.
//
// This is deliberately not t.Truncate(24*time.Hour): Truncate works on the
// absolute duration since the zero time, so it always snaps to UTC midnight no
// matter which location t carries. Building the day with time.Date instead also
// keeps AddDate correct across a DST transition, where a day is not 24 hours.
func StartOfDay(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	t = t.In(loc)
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// NextOnGrid advances t, which is already expressed in loc, to the next
// multiple of interval since midnight.
//
// A job's runs sit on this grid rather than on intervals measured from whenever
// the process last started, so "the :15 sync" means the same thing across
// restarts and deploys, and a redeploy cannot shift a whole schedule onto an
// arbitrary offset.
//
// A schedule that changes its interval has to change it on a point every
// interval it uses already lands on -- a whole hour, or midnight -- or this
// single step can jump over the transition. An interval of a day or more steps
// to the next midnight a whole number of days on.
func NextOnGrid(t time.Time, loc *time.Location, interval time.Duration) time.Time {
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
