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
