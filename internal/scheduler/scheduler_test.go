package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// discardLogger keeps job logging out of test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSchedulerRunsOnInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:     "tick",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		// The first run lands one interval in, not immediately.
		synctest.Wait()
		if got := runs.Load(); got != 0 {
			t.Fatalf("before first tick: runs = %d, want 0", got)
		}

		synctest.Sleep(time.Minute)
		if got := runs.Load(); got != 1 {
			t.Fatalf("after 1 minute: runs = %d, want 1", got)
		}

		synctest.Sleep(3 * time.Minute)
		if got := runs.Load(); got != 4 {
			t.Fatalf("after 4 minutes: runs = %d, want 4", got)
		}

		cancel()
		s.Wait()
	})
}

func TestSchedulerContinuesAfterJobError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:     "flaky",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return errors.New("upstream unavailable")
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		// A job that fails every time must keep being scheduled.
		synctest.Sleep(3 * time.Minute)
		if got := runs.Load(); got != 3 {
			t.Fatalf("runs = %d, want 3 (loop should survive errors)", got)
		}

		cancel()
		s.Wait()
	})
}

func TestSchedulerStopsOnContextCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:     "tick",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(2 * time.Minute)
		before := runs.Load()

		cancel()
		s.Wait() // must return promptly rather than hang

		// No further runs once the context is canceled.
		synctest.Sleep(5 * time.Minute)
		if got := runs.Load(); got != before {
			t.Fatalf("runs after cancel = %d, want %d", got, before)
		}
	})
}

// A shutdown must interrupt a sync that is already in flight, which the old
// ticker.Stop() approach did not do.
func TestSchedulerCancelsInFlightRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		var runErr error

		s := New(discardLogger())
		s.Add(Job{
			Name:     "slow",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				runErr = ctx.Err()
				return ctx.Err()
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(time.Minute)
		<-started

		cancel()
		s.Wait()

		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("in-flight run error = %v, want context.Canceled", runErr)
		}
	})
}

func TestSchedulerAppliesPerRunTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runErr error
		done := make(chan struct{})

		s := New(discardLogger())
		s.Add(Job{
			Name:     "hangs",
			Interval: time.Minute,
			Timeout:  30 * time.Second,
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				runErr = ctx.Err()
				close(done)
				return ctx.Err()
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		// Tick at 1m, then the run's own 30s timeout fires at 1m30s.
		synctest.Sleep(time.Minute + 30*time.Second)
		<-done

		if !errors.Is(runErr, context.DeadlineExceeded) {
			t.Fatalf("run error = %v, want context.DeadlineExceeded", runErr)
		}

		cancel()
		s.Wait()
	})
}

func TestSchedulerRunsMultipleJobsIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var fast, slow atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:     "fast",
			Interval: time.Minute,
			Run:      func(ctx context.Context) error { fast.Add(1); return nil },
		})
		s.Add(Job{
			Name:     "slow",
			Interval: 5 * time.Minute,
			Run:      func(ctx context.Context) error { slow.Add(1); return nil },
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(10 * time.Minute)
		if got := fast.Load(); got != 10 {
			t.Errorf("fast runs = %d, want 10", got)
		}
		if got := slow.Load(); got != 2 {
			t.Errorf("slow runs = %d, want 2", got)
		}

		cancel()
		s.Wait()
	})
}

func TestSchedulerIgnoresInvalidJobs(t *testing.T) {
	s := New(discardLogger())
	s.Add(Job{Name: "no-interval", Run: func(ctx context.Context) error { return nil }})
	s.Add(Job{Name: "no-func", Interval: time.Minute})
	s.Add(Job{Name: "negative", Interval: -time.Minute, Run: func(ctx context.Context) error { return nil }})

	if len(s.jobs) != 0 {
		t.Fatalf("registered %d jobs, want 0", len(s.jobs))
	}
}

// A NextDelay job is re-asked after every run, so its cadence can change as the
// week does rather than being fixed at registration.
func TestSchedulerRecomputesNextDelayEachRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64
		delays := []time.Duration{time.Minute, 5 * time.Minute, 2 * time.Minute}

		s := New(discardLogger())
		s.Add(Job{
			Name: "adaptive",
			NextDelay: func(now time.Time) time.Duration {
				return delays[min(int(runs.Load()), len(delays)-1)]
			},
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		// Waits of 1m, then 5m, then 2m: each comes from the schedule as it
		// stood after the previous run, not from the first value returned.
		steps := []struct {
			advance time.Duration
			want    int64
		}{
			{time.Minute, 1},
			{5 * time.Minute, 2},
			{2 * time.Minute, 3},
			{2 * time.Minute, 4},
		}

		elapsed := time.Duration(0)
		for _, step := range steps {
			synctest.Sleep(step.advance)
			elapsed += step.advance
			if got := runs.Load(); got != step.want {
				t.Fatalf("after %v: runs = %d, want %d", elapsed, got, step.want)
			}
		}

		cancel()
		s.Wait()
	})
}

// A schedule that returns nothing usable must not turn into a hot loop against
// a metered API.
func TestSchedulerFloorsNonPositiveNextDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:      "broken-schedule",
			NextDelay: func(now time.Time) time.Duration { return 0 },
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(3 * time.Minute)
		if got := runs.Load(); got != 3 {
			t.Fatalf("runs = %d, want 3 (a zero delay should floor at one minute)", got)
		}

		cancel()
		s.Wait()
	})
}

// NextDelay is a schedule in its own right, so a job carrying one needs no
// Interval.
func TestSchedulerAcceptsNextDelayWithoutInterval(t *testing.T) {
	s := New(discardLogger())
	s.Add(Job{
		Name:      "no-interval-but-scheduled",
		NextDelay: func(now time.Time) time.Duration { return time.Minute },
		Run:       func(ctx context.Context) error { return nil },
	})

	if len(s.jobs) != 1 {
		t.Fatalf("registered %d jobs, want 1", len(s.jobs))
	}
}

// The footer reads these, so a job has to report itself before it has ever run
// rather than appearing out of nowhere on its first tick.
func TestSchedulerStatusIsRegisteredBeforeFirstRun(t *testing.T) {
	s := New(discardLogger())
	s.Add(Job{
		Name:     "cfb",
		Label:    "Football",
		Interval: time.Minute,
		Run:      func(ctx context.Context) error { return nil },
	})

	status := s.Status()
	if len(status) != 1 {
		t.Fatalf("got %d statuses, want 1", len(status))
	}
	if status[0].Name != "cfb" || status[0].Label != "Football" {
		t.Errorf("status = %+v, want name cfb / label Football", status[0])
	}
	if !status[0].LastRun.IsZero() || !status[0].LastSuccess.IsZero() {
		t.Errorf("a job that has not run should report zero times, got %+v", status[0])
	}
}

// A failing job still ran, but it did not refresh anything. The two timestamps
// have to part company or the footer reports fresh data that never arrived.
func TestSchedulerStatusSeparatesRunsFromSuccesses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var fail atomic.Bool

		s := New(discardLogger())
		s.Add(Job{
			Name:     "flaky",
			Label:    "Flaky",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				if fail.Load() {
					return errors.New("upstream said no")
				}
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(time.Minute)
		good := s.Status()[0]
		if good.LastSuccess.IsZero() || !good.LastSuccess.Equal(good.LastRun) {
			t.Fatalf("after a good run: LastRun = %v, LastSuccess = %v, want equal and non-zero",
				good.LastRun, good.LastSuccess)
		}

		fail.Store(true)
		synctest.Sleep(time.Minute)

		bad := s.Status()[0]
		if !bad.LastRun.After(good.LastRun) {
			t.Errorf("LastRun = %v, want it to advance past %v on a failed run", bad.LastRun, good.LastRun)
		}
		if !bad.LastSuccess.Equal(good.LastSuccess) {
			t.Errorf("LastSuccess = %v, want it held at %v — a failed run refreshed nothing",
				bad.LastSuccess, good.LastSuccess)
		}

		cancel()
		s.Wait()
	})
}

func TestSchedulerStatusReportsNextRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(discardLogger())
		s.Add(Job{
			Name:     "cfb",
			Label:    "Football",
			Interval: 5 * time.Minute,
			Run:      func(ctx context.Context) error { return nil },
		})

		ctx, cancel := context.WithCancel(t.Context())
		start := time.Now()
		s.Start(ctx)
		synctest.Wait()

		if got, want := s.Status()[0].NextRun, start.Add(5*time.Minute); !got.Equal(want) {
			t.Errorf("NextRun before the first run = %v, want %v", got, want)
		}

		synctest.Sleep(5 * time.Minute)
		if got, want := s.Status()[0].NextRun, start.Add(10*time.Minute); !got.Equal(want) {
			t.Errorf("NextRun after the first run = %v, want %v", got, want)
		}

		cancel()
		s.Wait()
	})
}

// Only labelled jobs are meant for the UI; the calendar sync is plumbing.
func TestSchedulerPublicStatusHidesUnlabelledJobs(t *testing.T) {
	s := New(discardLogger())
	s.Add(Job{Name: "cfb", Label: "Football", Interval: time.Minute, Run: func(ctx context.Context) error { return nil }})
	s.Add(Job{Name: "cfb-calendar", Interval: time.Hour, Run: func(ctx context.Context) error { return nil }})

	if got := len(s.Status()); got != 2 {
		t.Errorf("Status() returned %d jobs, want 2", got)
	}

	public := s.PublicStatus()
	if len(public) != 1 {
		t.Fatalf("PublicStatus() returned %d jobs, want 1", len(public))
	}
	if public[0].Name != "cfb" {
		t.Errorf("PublicStatus() surfaced %q, want cfb", public[0].Name)
	}
}

// A successful run has to be handed to the recorder so it can outlive the
// process; a failed one must not be, or a restart would report data that never
// arrived.
func TestSchedulerRecordsOnlySuccessfulRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var fail atomic.Bool
		var recorded []string

		s := New(discardLogger())
		s.SetSuccessRecorder(func(job string, at time.Time) {
			recorded = append(recorded, job)
		})
		s.Add(Job{
			Name:     "flaky",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				if fail.Load() {
					return errors.New("upstream said no")
				}
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(time.Minute)
		if len(recorded) != 1 {
			t.Fatalf("after a good run: recorded %v, want one entry", recorded)
		}

		fail.Store(true)
		synctest.Sleep(2 * time.Minute)
		if len(recorded) != 1 {
			t.Errorf("after two failed runs: recorded %v, want it unchanged", recorded)
		}

		cancel()
		s.Wait()
	})
}

// A restart should not report every sync as having never run.
func TestSchedulerRestoreSuccesses(t *testing.T) {
	earlier := time.Now().Add(-time.Hour)

	s := New(discardLogger())
	s.Add(Job{Name: "cfb", Label: "Football", Interval: time.Minute, Run: func(ctx context.Context) error { return nil }})

	s.RestoreSuccesses(map[string]time.Time{
		"cfb":     earlier,
		"unknown": time.Now(),
	})

	status := s.Status()
	if len(status) != 1 {
		t.Fatalf("got %d statuses, want 1 (an unknown job should not create one)", len(status))
	}
	if !status[0].LastSuccess.Equal(earlier) {
		t.Errorf("LastSuccess = %v, want the restored %v", status[0].LastSuccess, earlier)
	}
	if !status[0].LastRun.IsZero() {
		t.Errorf("LastRun = %v, want zero — this process has not run it yet", status[0].LastRun)
	}
}

func TestTriggerRunsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:     "tick",
			Interval: time.Hour,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		// An hour away from its next tick, the job runs on request.
		if err := s.Trigger("tick"); err != nil {
			t.Fatalf("Trigger() error = %v", err)
		}
		synctest.Wait()

		if got := runs.Load(); got != 1 {
			t.Fatalf("after Trigger: runs = %d, want 1", got)
		}

		cancel()
		s.Wait()
	})
}

func TestTriggerUnknownJob(t *testing.T) {
	s := New(discardLogger())
	s.Add(Job{Name: "tick", Interval: time.Minute, Run: func(ctx context.Context) error { return nil }})

	if err := s.Trigger("nope"); !errors.Is(err, ErrUnknownJob) {
		t.Errorf("Trigger(unknown) error = %v, want ErrUnknownJob", err)
	}
}

// A second request while one is already queued is refused rather than queued
// behind it. Against a metered upstream, an impatient double click on the admin
// page would otherwise buy two runs.
func TestTriggerDebouncesWhileRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64
		release := make(chan struct{})

		s := New(discardLogger())
		s.Add(Job{
			Name:     "slow",
			Interval: time.Hour,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				<-release
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		if err := s.Trigger("slow"); err != nil {
			t.Fatalf("first Trigger() error = %v", err)
		}
		synctest.Wait()

		// The first request has been taken off the channel and is in flight, so
		// one more may be queued behind it...
		if err := s.Trigger("slow"); err != nil {
			t.Fatalf("second Trigger() error = %v", err)
		}
		// ...but not two.
		if err := s.Trigger("slow"); !errors.Is(err, ErrRunPending) {
			t.Errorf("third Trigger() error = %v, want ErrRunPending", err)
		}

		close(release)
		synctest.Wait()

		if got := runs.Load(); got != 2 {
			t.Errorf("runs = %d, want 2", got)
		}

		cancel()
		s.Wait()
	})
}

// A manual run must leave the timer armed for a fresh interval. Resetting a
// timer that is still armed without draining it fires an immediate second run.
func TestTriggerReschedulesWithoutDoubleFiring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:     "tick",
			Interval: time.Hour,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		// Half an interval in, ask for a run.
		time.Sleep(30 * time.Minute)
		if err := s.Trigger("tick"); err != nil {
			t.Fatalf("Trigger() error = %v", err)
		}
		synctest.Wait()

		if got := runs.Load(); got != 1 {
			t.Fatalf("after Trigger: runs = %d, want 1", got)
		}

		// The originally scheduled tick would have landed here. It must not,
		// because the manual run reset the clock.
		time.Sleep(31 * time.Minute)
		synctest.Wait()
		if got := runs.Load(); got != 1 {
			t.Errorf("at the old scheduled time: runs = %d, want 1", got)
		}

		// A full interval after the manual run, the schedule resumes.
		time.Sleep(30 * time.Minute)
		synctest.Wait()
		if got := runs.Load(); got != 2 {
			t.Errorf("one interval after the manual run: runs = %d, want 2", got)
		}

		cancel()
		s.Wait()
	})
}

func TestStatusReportsLastError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var fail atomic.Bool
		fail.Store(true)

		s := New(discardLogger())
		s.Add(Job{
			Name:     "flaky",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				if fail.Load() {
					return errors.New("upstream is down")
				}
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		time.Sleep(time.Minute)
		synctest.Wait()

		if got := s.Status()[0].LastError; got != "upstream is down" {
			t.Errorf("after a failure: LastError = %q, want %q", got, "upstream is down")
		}

		// A success clears it, so the page does not keep reporting an outage
		// that has already recovered.
		fail.Store(false)
		time.Sleep(time.Minute)
		synctest.Wait()

		if got := s.Status()[0].LastError; got != "" {
			t.Errorf("after a success: LastError = %q, want empty", got)
		}

		cancel()
		s.Wait()
	})
}

// A job runs on a goroutine this package owns, so an unrecovered panic there
// would take the whole server down -- not just the sync. It has to be reported
// like any other failed run and the schedule has to carry on.
func TestSchedulerSurvivesPanickingJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64
		var boom atomic.Bool
		boom.Store(true)

		s := New(discardLogger())
		s.Add(Job{
			Name:     "explodes",
			Label:    "Explodes",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				if boom.Load() {
					panic("nil map write")
				}
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(time.Minute)

		panicked := s.Status()[0]
		if panicked.LastRun.IsZero() {
			t.Fatal("LastRun is zero, want the panicking run recorded as a run")
		}
		if !panicked.LastSuccess.IsZero() {
			t.Errorf("LastSuccess = %v, want zero — a panic is not a success", panicked.LastSuccess)
		}
		if !strings.Contains(panicked.LastError, "nil map write") {
			t.Errorf("LastError = %q, want it to carry the panic value", panicked.LastError)
		}
		if panicked.Running {
			t.Error("Running is true after a panic, want the run marked finished")
		}

		// The schedule survives: the job is retried, and a later success clears
		// the error exactly as it would after an ordinary failure.
		boom.Store(false)
		synctest.Sleep(time.Minute)

		recovered := s.Status()[0]
		if got := runs.Load(); got != 2 {
			t.Fatalf("runs = %d, want 2 (the loop must keep scheduling after a panic)", got)
		}
		if recovered.LastSuccess.IsZero() {
			t.Error("LastSuccess is zero after a good run following a panic")
		}
		if recovered.LastError != "" {
			t.Errorf("LastError = %q, want it cleared by the successful run", recovered.LastError)
		}

		cancel()
		s.Wait()
	})
}

// The recovered value is wrapped so a caller can tell a panic from an upstream
// error without matching on message text.
func TestSchedulerPanicErrorIsIdentifiable(t *testing.T) {
	s := New(discardLogger())
	job := Job{
		Name: "explodes",
		Run:  func(ctx context.Context) error { panic("boom") },
	}

	err := s.safeRun(t.Context(), job)
	if !errors.Is(err, ErrJobPanic) {
		t.Fatalf("err = %v, want it to wrap ErrJobPanic", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want it to name the panic value", err)
	}
}

// A manual-only job is registered, reports itself, and waits -- it must never
// fire on its own, however long the process runs.
func TestSchedulerManualOnlyJobNeverRunsOnItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:       "seed",
			ManualOnly: true,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)

		synctest.Sleep(72 * time.Hour)
		if got := runs.Load(); got != 0 {
			t.Fatalf("runs = %d after three days, want 0", got)
		}

		status := s.Status()[0]
		if !status.Manual {
			t.Error("Status.Manual = false, want true so the page can say why there is no next run")
		}
		if !status.NextRun.IsZero() {
			t.Errorf("NextRun = %v, want zero for a job that is never scheduled", status.NextRun)
		}

		cancel()
		s.Wait()
	})
}

// Triggering it runs it once, through the same path as any other job: the run
// is recorded, and a second trigger while one is pending is still refused.
func TestSchedulerManualOnlyJobRunsWhenTriggered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int64

		s := New(discardLogger())
		s.Add(Job{
			Name:       "seed",
			ManualOnly: true,
			Run: func(ctx context.Context) error {
				runs.Add(1)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		if err := s.Trigger("seed"); err != nil {
			t.Fatalf("Trigger() = %v, want nil", err)
		}
		synctest.Wait()

		if got := runs.Load(); got != 1 {
			t.Fatalf("runs = %d, want 1", got)
		}
		if status := s.Status()[0]; status.LastSuccess.IsZero() {
			t.Error("LastSuccess is zero, want a manual run recorded like any other")
		}

		// And it stays manual: no timer was armed by the run just completed.
		synctest.Sleep(72 * time.Hour)
		if got := runs.Load(); got != 1 {
			t.Errorf("runs = %d after a manual run plus three days, want 1", got)
		}

		cancel()
		s.Wait()
	})
}

func TestSchedulerAcceptsManualOnlyJobWithoutSchedule(t *testing.T) {
	s := New(discardLogger())
	s.Add(Job{Name: "seed", ManualOnly: true, Run: func(ctx context.Context) error { return nil }})

	if len(s.jobs) != 1 {
		t.Fatalf("registered %d jobs, want 1 (ManualOnly is a schedule in its own right)", len(s.jobs))
	}
}

// A manual trigger can hand the run a value. The plumbing matters because the
// alternative -- a field somewhere that the trigger sets and the run reads --
// lets a second, rejected trigger change what an already-queued run will do.
func TestSchedulerTriggerPassesArgsToTheRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		got := make(chan []string, 1)

		s := New(discardLogger())
		s.Add(Job{
			Name:       "seed",
			ManualOnly: true,
			// A Timeout is essential to this test, not incidental: it makes
			// invoke derive a second context, which is where the args were
			// once dropped.
			Timeout: time.Hour,
			Run: func(ctx context.Context) error {
				got <- ArgsFrom(ctx)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		if err := s.Trigger("seed", "2019"); err != nil {
			t.Fatalf("Trigger() = %v, want nil", err)
		}
		synctest.Wait()

		args := <-got
		if len(args) != 1 || args[0] != "2019" {
			t.Fatalf("ArgsFrom() = %v, want [2019]", args)
		}

		cancel()
		s.Wait()
	})
}

// A scheduled run was triggered by nobody, so it must not inherit the arguments
// of whatever manual run happened to come before it.
func TestSchedulerScheduledRunHasNoArgs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		got := make(chan []string, 2)

		s := New(discardLogger())
		s.Add(Job{
			Name:     "sync",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				got <- ArgsFrom(ctx)
				return nil
			},
		})

		ctx, cancel := context.WithCancel(t.Context())
		s.Start(ctx)
		synctest.Wait()

		if err := s.Trigger("sync", "2019"); err != nil {
			t.Fatalf("Trigger() = %v, want nil", err)
		}
		synctest.Wait()
		if args := <-got; len(args) != 1 {
			t.Fatalf("manual run args = %v, want [2019]", args)
		}

		// The next run comes from the timer, not from anyone asking.
		synctest.Sleep(time.Minute)
		if args := <-got; args != nil {
			t.Errorf("scheduled run args = %v, want nil", args)
		}

		cancel()
		s.Wait()
	})
}

// ArgsFrom on a context that never carried any must not panic on the type
// assertion -- scheduled runs take exactly this path.
func TestArgsFromPlainContext(t *testing.T) {
	if args := ArgsFrom(t.Context()); args != nil {
		t.Errorf("ArgsFrom() = %v, want nil", args)
	}
}
