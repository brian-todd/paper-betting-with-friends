// Package syncerr summarizes the writes a sync run could not complete.
//
// A sync walks hundreds of rows from an upstream feed, and a single row that
// will not save is no reason to abandon the rest -- so the loops log the
// failure and carry on. Left there, a run that dropped every row on the floor
// still returns nil and the scheduler records a success: the admin page shows a
// healthy job and the only trace is a few hundred lines in the log.
package syncerr

import (
	"errors"
	"fmt"
)

// ErrIncomplete marks a run that finished its pass but could not write
// everything it read.
//
// It exists so a caller sequencing several syncs can tell a partial run apart
// from an outright failure. There is nothing to be gained by continuing past an
// unreachable API, but the next month of games is still worth fetching after a
// handful of rows would not save.
var ErrIncomplete = errors.New("incomplete sync")

// Tally counts failed writes and keeps the first error as the representative
// one. The zero value is ready to use.
type Tally struct {
	count int
	first error
}

// Add records a failed write. A nil error is not a failure and is ignored, so a
// caller can hand it the result of every write unconditionally.
func (t *Tally) Add(err error) {
	if err == nil {
		return
	}
	t.count++
	if t.first == nil {
		t.first = err
	}
}

// Count is the number of failed writes recorded.
func (t *Tally) Count() int {
	return t.count
}

// Err summarizes the failures as one error wrapping ErrIncomplete, or nil if
// there were none. The noun names what failed to write, e.g. "odds".
//
// The count leads because it is what distinguishes the incidents: one row a
// feed sent malformed and every row in the run failing on the same broken
// statement read identically otherwise, and Status.LastError is a single line
// on the admin page.
func (t *Tally) Err(noun string) error {
	if t.first == nil {
		return nil
	}

	writes := "writes"
	if t.count == 1 {
		writes = "write"
	}
	return fmt.Errorf("%w: %d %s %s failed, first: %w", ErrIncomplete, t.count, noun, writes, t.first)
}
