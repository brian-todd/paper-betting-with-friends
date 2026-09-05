package syncerr

import (
	"errors"
	"strings"
	"testing"
)

func TestTallyErr(t *testing.T) {
	boom := errors.New("boom")
	later := errors.New("later")

	tests := []struct {
		name     string
		add      []error
		count    int
		wantNil  bool
		contains []string
	}{
		{
			name:    "a run with no failures reports success",
			add:     []error{nil, nil},
			count:   0,
			wantNil: true,
		},
		{
			name:     "a single failure is counted in the singular",
			add:      []error{boom},
			count:    1,
			contains: []string{"1 odds write failed", "boom"},
		},
		{
			name:     "a run that lost everything leads with the count",
			add:      []error{boom, later, later},
			count:    3,
			contains: []string{"3 odds writes failed", "boom"},
		},
		{
			name:     "successful writes in between do not count",
			add:      []error{nil, boom, nil, later},
			count:    2,
			contains: []string{"2 odds writes failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tally Tally
			for _, err := range tt.add {
				tally.Add(err)
			}

			if got := tally.Count(); got != tt.count {
				t.Errorf("Count() = %d, want %d", got, tt.count)
			}

			err := tally.Err("odds")
			if tt.wantNil {
				if err != nil {
					t.Fatalf("Err() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Err() = nil, want an error")
			}

			// The scheduler stores err.Error() and the admin page shows that
			// line and nothing else, so everything worth knowing is in it.
			for _, want := range tt.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Err() = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

// A caller sequencing several syncs decides whether to carry on by asking which
// kind of failure this was, so the summary has to answer both questions: it is
// a partial run, and this is the error that made it one.
func TestTallyErrWrapsBothTheSentinelAndTheFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	later := errors.New("later")

	var tally Tally
	tally.Add(boom)
	tally.Add(later)
	err := tally.Err("odds")

	if !errors.Is(err, ErrIncomplete) {
		t.Errorf("errors.Is(err, ErrIncomplete) = false for %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("errors.Is(err, boom) = false for %v", err)
	}
	if errors.Is(err, later) {
		t.Error("the summary carries a later failure too; only the first is kept")
	}
}

// An outright failure -- an unreachable API, a database that is gone -- must
// not read as a partial run, or a caller will carry on through it.
func TestAnUnrelatedErrorIsNotIncomplete(t *testing.T) {
	if errors.Is(errors.New("dial tcp: connection refused"), ErrIncomplete) {
		t.Error("a plain error matched ErrIncomplete")
	}
}
