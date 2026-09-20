// Package fixtureserver replays captured API responses over HTTP.
//
// It stands in for api.collegefootballdata.com and its basketball twin, so a
// sync can be run end to end with no API key, no network and no spend against
// a metered budget.
//
// It is not test-only: `seed -fixtures` runs the same handler in-process, so a
// fresh clone can populate a database. That is why it lives here rather than
// in a _test.go file.
//
// # It fails loudly
//
// A request with no fixture is a 500 naming the path and the directory that
// was searched. The tempting alternative -- answer "[]", since every endpoint
// here returns a JSON array -- is the single worst thing this could do. A sync
// against an empty array writes nothing, logs success, and is indistinguishable
// from a sync that had nothing to write. That is the exact bug the fixtures
// exist to catch, reproduced inside the tool built to catch it.
//
// # It replays a sequence
//
// The nth request to a route gets the nth capture. Past the end it repeats the
// last one, which is what a real feed does when nothing has changed. A test
// that wants exhaustion to be an error asks for it with ExhaustIsError.
package fixtureserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/fixtures"
)

// Server replays one provider's captures.
type Server struct {
	provider string
	set      fixtures.Set

	// exhaustIsError turns running off the end of a sequence into a 500
	// instead of repeating the last capture.
	exhaustIsError bool

	mu sync.Mutex
	// served counts requests per route, which is what makes the reply depend
	// on how many times it has been asked.
	served map[string]int
	// requests records every path seen, so a test can assert on what the sync
	// actually fetched -- including what it fetched and should not have.
	requests []string
}

// Option configures a Server.
type Option func(*Server)

// ExhaustIsError makes a request past the end of a sequence fail rather than
// repeat the final capture. For a test whose subject is the number of calls.
func ExhaustIsError() Option {
	return func(s *Server) { s.exhaustIsError = true }
}

// WithFixtures replaces the embedded tree, for a test that wants to write its
// own captures to a temp directory.
func WithFixtures(set fixtures.Set) Option {
	return func(s *Server) { s.set = set }
}

// New returns a handler replaying the given provider's captures.
func New(provider string, opts ...Option) *Server {
	s := &Server{
		provider: provider,
		set:      fixtures.Embedded(),
		served:   make(map[string]int),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Listen starts the handler on a loopback port and returns its base URL, ready
// to hand to cfbdata.NewClientAt. The caller calls stop.
//
// A plain net.Listener rather than httptest, because httptest imports testing
// and flag: linking it into `seed` would register the whole -test.* flag set
// on a user-facing command.
func Listen(provider string, opts ...Option) (baseURL string, fake *Server, stop func(), err error) {
	fake = New(provider, opts...)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, fmt.Errorf("listening for the fixture server: %w", err)
	}

	srv := &http.Server{Handler: fake, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("fixture server stopped", "error", err)
		}
	}()

	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String(), fake, stop, nil
}

// Requests returns the paths served so far, in order, query strings included.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Path
	if r.URL.RawQuery != "" {
		route += "?" + r.URL.RawQuery
	}

	s.mu.Lock()
	s.requests = append(s.requests, route)
	s.mu.Unlock()

	captures, err := s.set.Sequence(s.provider, r.URL.Path, r.URL.RawQuery)
	if err != nil {
		s.refuse(w, route, err)
		return
	}

	// The index is keyed on the resolved sequence, not on the raw route, so
	// two callers spelling the same query differently share one position --
	// which is the behaviour a real feed has.
	key := captures[0].Path
	s.mu.Lock()
	n := s.served[key]
	s.served[key]++
	s.mu.Unlock()

	if n >= len(captures) {
		if s.exhaustIsError {
			s.refuse(w, route, fmt.Errorf("sequence of %d exhausted on request %d", len(captures), n+1))
			return
		}
		n = len(captures) - 1
	}

	capture := captures[n]
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(capture.Status)
	if _, err := w.Write(capture.Body); err != nil {
		slog.Debug("fixture server write failed", "route", route, "error", err)
	}
}

// refuse answers a request no fixture covers. It is a 500 with a sentence
// naming the path, and deliberately not an empty array -- see the package
// comment.
func (s *Server) refuse(w http.ResponseWriter, route string, cause error) {
	msg := fmt.Sprintf("no fixture for %s %s: %v\n\n"+
		"Capture it with: scripts/capture.sh %s '%s'\n",
		s.provider, route, cause, s.provider, route)
	http.Error(w, msg, http.StatusInternalServerError)
}
