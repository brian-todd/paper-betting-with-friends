package timeutil

import (
	"testing"
	"time"
)

// The zero value being the real clock is what lets five services take one of
// these without a constructor change, so it is the property worth pinning.
func TestZeroClockIsTimeNow(t *testing.T) {
	var c Clock

	before := time.Now()
	got := c.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("zero Clock.Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestSetOverridesAndNilRestores(t *testing.T) {
	var c Clock
	at := time.Date(2026, 9, 5, 20, 49, 9, 0, time.UTC)

	c.Set(Fixed(at))
	if got := c.Now(); !got.Equal(at) {
		t.Errorf("Now() after Set = %v, want %v", got, at)
	}
	// Twice, because a test asserting on an interval wants the same answer
	// every time it asks rather than a clock that advances on read.
	if got := c.Now(); !got.Equal(at) {
		t.Errorf("second Now() = %v, want %v", got, at)
	}

	c.Set(nil)
	if got := c.Now(); got.Before(at) {
		t.Errorf("Now() after Set(nil) = %v, want the real clock, which is past %v", got, at)
	}
}
