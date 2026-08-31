// Package scheduler runs named background jobs on a fixed interval until the
// supplied context is canceled.
//
// It exists so the periodic data syncs are testable: the loops used to live as
// anonymous goroutines in cmd/server, where they could only be exercised with
// real wall-clock sleeps. Keeping them here lets testing/synctest drive them
// with a fake clock.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Job is a unit of recurring work.
type Job struct {
	// Name identifies the job in log output.
	Name string

	// Label names the job for display to users. A job without one is internal
	// plumbing and stays out of the UI; see PublicStatus.
	Label string

	// Interval is how often Run is invoked. Must be > 0 unless NextDelay is set.
	Interval time.Duration

	// NextDelay, when set, replaces Interval: the scheduler asks it before each
	// run how long to wait. It exists so a job can follow the shape of the thing
	// it syncs — polling hard while games are on and not at all overnight —
	// rather than paying a flat rate around the clock, which against a metered
	// upstream is most of the monthly bill.
	//
	// A non-positive result is floored at one minute, so a schedule that
	// miscomputes cannot spin the loop.
	NextDelay func(now time.Time) time.Duration

	// ManualOnly marks a job with no schedule at all: it runs only when Trigger
	// asks for it. It exists for work that is genuinely occasional -- seeding a
	// season's teams and venues, say -- which still wants everything the
	// scheduler provides (a run timeout, panic recovery, recorded status, and
	// the guarantee of no overlapping runs) and none of what a timer provides.
	//
	// Interval and NextDelay are ignored when it is set.
	ManualOnly bool

	// Timeout bounds a single invocation of Run. Zero means no timeout beyond
	// cancellation of the scheduler's context.
	Timeout time.Duration

	// Run performs the work. It is never called concurrently with itself: the
	// wait for the next run begins when the previous one returns, so a run that
	// overruns its interval delays the next rather than overlapping.
	Run func(ctx context.Context) error
}

// Status is a snapshot of one job's schedule and its last outcome.
type Status struct {
	Name  string
	Label string

	// LastRun is when a run last finished, successfully or not; LastSuccess is
	// when one last finished without error. They diverge as soon as an upstream
	// starts failing, which is precisely when the difference is worth showing:
	// a job that ran two minutes ago and errored has not refreshed anything.
	LastRun     time.Time
	LastSuccess time.Time

	// NextRun is when the next run is currently due. Zero before Start.
	NextRun time.Time

	// LastError is the message from the most recent failed run, cleared by the
	// next success. LastRun alone says a job ran; this says what went wrong,
	// which is the difference between an admin page that is useful during an
	// outage and one that only tells you to go read the logs.
	LastError string

	// Running is true while a run is in flight.
	Running bool

	// Manual reports a job that only ever runs when asked. It lets the admin
	// page distinguish "no next run because it is never scheduled" from "no
	// next run recorded yet", which look identical through NextRun alone.
	Manual bool
}

// Errors returned by Trigger.
var (
	ErrUnknownJob = errors.New("unknown job")
	ErrRunPending = errors.New("a run is already pending for this job")
)

// ErrJobPanic wraps a value recovered from a panicking job.
var ErrJobPanic = errors.New("job panicked")

// argsKey carries a manual run's arguments on the run context.
type argsKey struct{}

// ArgsFrom returns the arguments the Trigger call that started this run
// supplied, and nil for a run the schedule started.
//
// They travel on the context rather than in Run's signature so that the jobs
// which take no arguments -- which is most of them -- stay declarable as a
// plain method value. A job that wants one opts in by reading this.
func ArgsFrom(ctx context.Context) []string {
	args, _ := ctx.Value(argsKey{}).([]string)
	return args
}

// Scheduler runs a set of Jobs concurrently, one goroutine per job.
type Scheduler struct {
	logger *slog.Logger
	jobs   []Job
	wg     sync.WaitGroup

	// statusMu guards status, triggers and recordSuccess. Status is read from
	// request handlers, so it is touched far more often than it is written.
	statusMu      sync.RWMutex
	status        map[string]*Status
	triggers      map[string]chan []string
	recordSuccess func(job string, at time.Time)
}

// SetSuccessRecorder registers a function called after every successful run, so
// the time can outlive the process.
//
// It is a function rather than a store interface to keep this package free of
// any dependency on the database: the scheduler knows when a job succeeded and
// nothing else. Errors are the recorder's to log — a failure to persist must not
// take down the schedule.
func (s *Scheduler) SetSuccessRecorder(fn func(job string, at time.Time)) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.recordSuccess = fn
}

// RestoreSuccesses seeds the last-success times of already-registered jobs from
// somewhere durable, so a restart does not report every sync as having never
// run. Names with no matching job are ignored.
//
// Only LastSuccess is seeded. LastRun stays zero because this process has not
// in fact run anything yet, and conflating the two is what the split exists to
// avoid.
func (s *Scheduler) RestoreSuccesses(successes map[string]time.Time) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	for job, at := range successes {
		if st, ok := s.status[job]; ok {
			st.LastSuccess = at
		}
	}
}

// New creates a Scheduler. If logger is nil, slog.Default is used.
func New(logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		logger:   logger,
		status:   make(map[string]*Status),
		triggers: make(map[string]chan []string),
	}
}

// Status returns a snapshot of every registered job, in registration order.
func (s *Scheduler) Status() []Status {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	out := make([]Status, 0, len(s.jobs))
	for _, j := range s.jobs {
		if st, ok := s.status[j.Name]; ok {
			out = append(out, *st)
		}
	}
	return out
}

// PublicStatus returns the subset of Status for jobs carrying a Label, which
// are the ones meant to be shown to users.
func (s *Scheduler) PublicStatus() []Status {
	all := s.Status()
	out := make([]Status, 0, len(all))
	for _, st := range all {
		if st.Label != "" {
			out = append(out, st)
		}
	}
	return out
}

// setRunning marks whether a run of the job is currently in flight.
func (s *Scheduler) setRunning(name string, running bool) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if st, ok := s.status[name]; ok {
		st.Running = running
	}
}

// recordNext notes when the job is next due.
func (s *Scheduler) recordNext(name string, at time.Time) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if st, ok := s.status[name]; ok {
		st.NextRun = at
	}
}

// recordRun notes that a run finished, and whether it achieved anything.
func (s *Scheduler) recordRun(name string, at time.Time, err error) {
	s.statusMu.Lock()
	if st, ok := s.status[name]; ok {
		st.LastRun = at
		st.Running = false
		if err == nil {
			st.LastSuccess = at
			st.LastError = ""
		} else {
			st.LastError = err.Error()
		}
	}
	recorder := s.recordSuccess
	s.statusMu.Unlock()

	// Outside the lock: persisting is a database round trip, and request
	// handlers are reading Status through this same mutex.
	if err == nil && recorder != nil {
		recorder(name, at)
	}
}

// Add registers a job. Jobs with a nil Run, or with no schedule at all (a
// non-positive Interval and no NextDelay), are ignored so callers can add jobs
// conditionally without extra branching.
func (s *Scheduler) Add(j Job) {
	if j.Run == nil {
		s.logger.Warn("skipping scheduler job with no work to do", "job", j.Name)
		return
	}
	if !j.ManualOnly && j.NextDelay == nil && j.Interval <= 0 {
		s.logger.Warn("skipping invalid scheduler job", "job", j.Name, "interval", j.Interval)
		return
	}
	s.jobs = append(s.jobs, j)

	// Register up front so a job that has not run yet still reports itself,
	// rather than vanishing from the UI until its first tick.
	s.statusMu.Lock()
	s.status[j.Name] = &Status{Name: j.Name, Label: j.Label, Manual: j.ManualOnly}
	// Capacity one, because the buffer is the debounce: a second Trigger while
	// a run is already queued is rejected rather than queued behind it. That
	// matters against a metered upstream, where an impatient double click on
	// the admin page would otherwise buy two runs.
	s.triggers[j.Name] = make(chan []string, 1)
	s.statusMu.Unlock()
}

// Trigger asks a job to run as soon as its goroutine is free, without waiting
// for its next scheduled time. The run happens on that same goroutine, so it
// cannot overlap a run already in progress, and it is recorded in Status and
// through the success recorder exactly like a scheduled one.
//
// Any args are handed to the run and readable with ArgsFrom. They travel in the
// trigger channel rather than in a field somewhere, so the value a run uses is
// the one its own trigger supplied -- a second, rejected trigger cannot change
// what an already-queued run is about to do.
//
// It returns ErrUnknownJob if no job of that name is registered, and
// ErrRunPending if one has already been requested and not yet started.
func (s *Scheduler) Trigger(name string, args ...string) error {
	s.statusMu.RLock()
	ch, ok := s.triggers[name]
	s.statusMu.RUnlock()

	if !ok {
		return ErrUnknownJob
	}

	select {
	case ch <- args:
		s.logger.Info("background job triggered manually", "job", name, "args", args)
		return nil
	default:
		return ErrRunPending
	}
}

// Start launches every registered job. It returns immediately; use Wait to
// block until all jobs have stopped. Jobs stop when ctx is canceled.
//
// The first run of a job happens one interval after Start rather than
// immediately, matching the behaviour of a bare time.Ticker.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		s.wg.Go(func() {
			s.runJob(ctx, j)
		})
	}
}

// Wait blocks until all jobs started by Start have returned.
func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) runJob(ctx context.Context, j Job) {
	s.statusMu.RLock()
	trigger := s.triggers[j.Name]
	s.statusMu.RUnlock()

	// A manual-only job has no timer. timerC stays nil, and a receive from a
	// nil channel blocks forever, so the select below simply never has a
	// scheduled case to choose.
	var (
		timer  *time.Timer
		timerC <-chan time.Time
	)

	if j.ManualOnly {
		s.logger.Info("background job started", "job", j.Name, "schedule", "manual")
	} else {
		delay := j.delay()
		s.recordNext(j.Name, time.Now().Add(delay))

		// A timer rather than a ticker, because the wait is recomputed after
		// every run: a NextDelay job changes its own cadence as the week moves.
		timer = time.NewTimer(delay)
		defer timer.Stop()
		timerC = timer.C

		s.logger.Info("background job started", "job", j.Name, "delay", delay)
	}

	defer s.logger.Info("background job stopped", "job", j.Name)

	for {
		select {
		case <-ctx.Done():
			return
		case <-timerC:
			s.invoke(ctx, j, nil)
			s.reschedule(j, timer, false)
		case args := <-trigger:
			s.invoke(ctx, j, args)
			// A manual run resets the clock rather than leaving the scheduled
			// one to fire moments later on data that was just refreshed. There
			// is no clock to reset on a manual-only job.
			if timer != nil {
				s.reschedule(j, timer, true)
			}
		}
	}
}

// reschedule arms timer for j's next run. When the run that just finished was
// not the timer firing, the timer is still armed and must be stopped and
// drained first, or the stale value left on its channel fires an immediate
// second run.
func (s *Scheduler) reschedule(j Job, timer *time.Timer, drain bool) {
	if drain && !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	delay := j.delay()
	s.recordNext(j.Name, time.Now().Add(delay))
	s.logger.Debug("background job rescheduled", "job", j.Name, "delay", delay)
	timer.Reset(delay)
}

// delay is how long to wait before the next run of j.
func (j Job) delay() time.Duration {
	if j.NextDelay == nil {
		return j.Interval
	}
	if d := j.NextDelay(time.Now()); d > 0 {
		return d
	}
	// Never busy-loop on a schedule that returns nothing useful.
	return time.Minute
}

// invoke runs the job once, bounded by j.Timeout. A failing run is logged and
// the loop continues; a transient upstream error must not kill the schedule.
func (s *Scheduler) invoke(ctx context.Context, j Job, args []string) {
	runCtx := ctx
	if len(args) > 0 {
		runCtx = context.WithValue(runCtx, argsKey{}, args)
	}
	if j.Timeout > 0 {
		var cancel context.CancelFunc
		// Derived from runCtx, not ctx: deriving from ctx would drop the args
		// attached just above, silently, for every job that sets a timeout.
		runCtx, cancel = context.WithTimeout(runCtx, j.Timeout)
		defer cancel()
	}

	s.setRunning(j.Name, true)

	start := time.Now()
	err := s.safeRun(runCtx, j)
	finished := time.Now()
	elapsed := finished.Sub(start)

	s.recordRun(j.Name, finished, err)

	switch {
	case err == nil:
		s.logger.Info("background job succeeded", "job", j.Name, "duration", elapsed)
	case errors.Is(err, context.Canceled) && ctx.Err() != nil:
		// Shutdown cancelled an in-flight run; that is expected, not a failure.
		s.logger.Info("background job canceled during shutdown", "job", j.Name, "duration", elapsed)
	default:
		s.logger.Error("background job failed", "job", j.Name, "duration", elapsed, "error", err)
	}
}

// safeRun invokes j.Run, converting a panic into an ordinary error.
//
// Jobs run on goroutines owned by this package, and a panic on a goroutine
// nobody recovers takes down the entire process -- so a malformed field in one
// upstream response would stop the server serving pages. Recovering keeps the
// blast radius at a single run, which the loop already knows how to survive.
//
// The returned error deliberately carries only the panic value: it surfaces in
// Status.LastError on the admin page, where a stack trace would be unreadable.
// The trace goes to the log instead, which is where anyone diagnosing it looks.
func (s *Scheduler) safeRun(ctx context.Context, j Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("background job panicked",
				"job", j.Name,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("%w: %v", ErrJobPanic, r)
		}
	}()

	return j.Run(ctx)
}
