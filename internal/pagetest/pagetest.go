// Package pagetest assembles what a page test needs and nothing else.
//
// Levels 3 and 4 both render a page against rows the feed really sent, and both
// need the same four things before they can render anything: a database holding
// a captured week, a renderer over the embedded templates, a user with a league
// and a purse, and one instant that every clock in the process agrees on. None
// of that is interesting, all of it is fiddly, and getting it subtly different
// in two packages is how two tests end up disagreeing about what "now" is.
//
// The two levels differ in what they do next. A level-3 test constructs the
// handler it is interested in and calls it, putting the user on the request
// with auth.ContextWithUser. A level-4 test takes cmd/server's whole router and
// mints a session by registering through the real handler. This package stops
// at the line they share.
//
// # Why the instant is a parameter
//
// A fixture replayed against the real clock is only half a recording: the
// bodies are what CFBD sent, but every status inferred from a kickoff, and
// every timestamp stamped on a score, come from whenever the test happened to
// run. Week 1 of 2026 was captured after it was played, so against a real clock
// it looks the same on any day -- but a test that asserts on a betting cutoff,
// a live badge or the current week is asserting on today's date, and it will
// keep passing until the day it does not.
//
// So Open takes the instant, pins the seed to it with fixtureseed.At, and hands
// back Now for the caller to give to every service it builds. Choose one of the
// capture instants; they are the fixture file names.
package pagetest

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	// Page tests resolve a named zone, and the suite should not depend on the
	// runner image shipping tzdata -- cmd/server takes the same precaution.
	_ "time/tzdata"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/fixtureseed"
	"github.com/brian/paper-betting-with-friends/internal/leagues"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
)

// SessionKey is a committed placeholder, long enough to satisfy the production
// length check that these tests never reach. Nothing signed with it leaves the
// process.
const SessionKey = "pagetest-session-key-not-a-secret-at-all"

// TimeZone is the zone the app reasons about calendar days in by default, and
// the one a page test should use: a grid that groups games by day groups them
// differently in UTC, and the difference is only visible at the edges.
const TimeZone = "America/New_York"

// Env is a database with a season in it, and everything needed to render a page
// against that season.
type Env struct {
	T        *testing.T
	DB       *gorm.DB
	Config   *config.Config
	Location *time.Location
	Renderer *templates.Renderer

	// At is the instant every clock in this test reads, and the instant the
	// fixtures were replayed at.
	At time.Time
	// Now is At as a clock, for SetClock.
	Now func() time.Time
}

// Assets is the embedded templates and static files, for a level-4 test that
// builds the real router. Never os.DirFS: that is relative to the process's
// working directory, which is the package directory under `go test`.
var Assets fs.FS = assets.FS

// Open gives a page test an empty transaction and a renderer, with every clock
// pinned to at. It writes no rows -- call Seed and Register for those.
func Open(t *testing.T, at time.Time) *Env {
	t.Helper()

	db := testdb.Open(t)

	location, err := time.LoadLocation(TimeZone)
	if err != nil {
		t.Fatalf("loading %s: %v", TimeZone, err)
	}

	// Built here rather than loaded, because config.Load reads .env: a page
	// test that picked up a developer's DATABASE_URL or API key would be
	// reaching outside the transaction it promised to stay inside.
	//
	// Development rather than production for one reason that matters. In
	// production auth.Service marks the session cookie Secure, and Go's cookie
	// jar honours that -- so a level-4 test over httptest's plain HTTP would
	// register successfully, receive a cookie it then refuses to send, and read
	// as "logged in user sees the login page".
	cfg := &config.Config{
		Env:            config.EnvDevelopment,
		SessionKey:     SessionKey,
		Port:           "0",
		TimeZone:       TimeZone,
		AdminUsername:  "pagetest-admin",
		MigrateOnStart: false,
	}

	// devMode false: the templates come from the embedded copy, which cannot
	// change mid-run, so re-globbing and re-parsing them on every render would
	// buy nothing and cost the whole page.
	renderer, err := templates.NewRenderer(assets.FS, false, location)
	if err != nil {
		t.Fatalf("loading templates: %v", err)
	}
	// The footer's copyright year is the one thing a template resolves from
	// "now" by itself, and a page describing a replayed Saturday should date
	// itself from that Saturday.
	renderer.SetClock(timeutil.Fixed(at))

	return &Env{
		T:        t,
		DB:       db,
		Config:   cfg,
		Location: location,
		Renderer: renderer,
		At:       at,
		Now:      timeutil.Fixed(at),
	}
}

// SeedFootball replays a captured football week into the transaction, as though
// the seed had run at Env.At.
func (e *Env) SeedFootball(year, week int) {
	e.T.Helper()

	counts, err := fixtureseed.Football(context.Background(), e.DB, year, week, fixtureseed.At(e.At))
	if err != nil {
		e.T.Fatalf("seeding football %d week %d: %v", year, week, err)
	}
	for _, count := range counts {
		if count.Rows == 0 {
			e.T.Fatalf("seeding football %d week %d wrote no %s", year, week, count.Table)
		}
	}
}

// Register creates a user through the real auth service, so the stored password
// hash is one Login will actually accept.
func (e *Env) Register(username, password string) *models.User {
	e.T.Helper()

	user, err := auth.NewService(e.DB, e.Config).Register(username, password)
	if err != nil {
		e.T.Fatalf("registering %q: %v", username, err)
	}
	return user
}

// League creates a league owned by user, which is also what opens their purse.
func (e *Env) League(name string, owner *models.User, balance string) *models.League {
	e.T.Helper()

	league, err := leagues.NewService(e.DB).CreateLeague(
		name, owner.ID, false, decimal.RequireFromString(balance))
	if err != nil {
		e.T.Fatalf("creating league %q: %v", name, err)
	}
	return league
}

// GET builds a request carrying user, the way auth.OptionalAuth would have, and
// a recorder to render into.
//
// This is the level-3 seam: the middleware stack is not in the picture, so the
// user goes straight onto the context. A level-4 test wants a real cookie
// instead and should not use this.
func (e *Env) GET(target string, user *models.User) (*httptest.ResponseRecorder, *http.Request) {
	e.T.Helper()
	return e.request(httptest.NewRequest(http.MethodGet, target, nil), user)
}

// POST is GET's counterpart for a form submission.
func (e *Env) POST(target string, form url.Values, user *models.User) (*httptest.ResponseRecorder, *http.Request) {
	e.T.Helper()

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.request(req, user)
}

func (e *Env) request(req *http.Request, user *models.User) (*httptest.ResponseRecorder, *http.Request) {
	if user != nil {
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	}
	return httptest.NewRecorder(), req
}
