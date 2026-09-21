package timeutil

import "time"

// A Clock is an overridable source of the current instant, for a service whose
// behaviour turns on what time it is.
//
// # Why a field and a setter rather than a constructor parameter
//
// Every service that needs one already has a constructor taking a *gorm.DB and
// nothing optional, called from main and from a dozen tests. Threading a clock
// through those signatures would touch every call site to say "the real one" at
// all but a handful of them. The zero Clock *is* the real one, so a service
// embedding this field needs no constructor change and no existing caller
// moves -- the same shape SetBetEvaluator already uses for the other
// collaborator only some callers supply.
//
// # Why not testing/synctest
//
// internal/scheduler controls time with a synctest bubble, and that does not
// generalise to anything replaying a recorded response. A bubble's clock starts
// at 2000-01-01T00:00:00Z, and every instant in every fixture we will ever
// capture is decades in its future: every game reads as scheduled, nothing is
// ever final, and the paths worth testing never run. A bubble also waits for
// every goroutine inside it, and net/http.Transport leaves its read and write
// loops running, so a test doing real HTTP inside one hangs until the ten
// minute panic rather than failing.
//
// # Concurrency
//
// Set is not safe against a concurrent Now. It is called once during
// construction, before the scheduler starts anything, which is the same
// contract SetBetEvaluator has. Nothing calls it on a running service.
type Clock struct {
	now func() time.Time
}

// Set overrides the time source. A nil argument restores time.Now.
func (c *Clock) Set(now func() time.Time) {
	c.now = now
}

// Now is the current instant, from time.Now unless Set said otherwise.
func (c *Clock) Now() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

// Fixed returns a time source that always answers at, for a test replaying a
// fixture captured at a known instant.
func Fixed(at time.Time) func() time.Time {
	return func() time.Time { return at }
}
