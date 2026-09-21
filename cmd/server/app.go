package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/admin"
	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/basketball"
	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/leagues"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"gorm.io/gorm"
)

// application is everything buildHandler constructs: the HTTP handler the
// server serves, and the pieces main still has to reach afterwards.
//
// It exists because the middle of main is the only place the real wiring lives
// -- which services a handler was given, which middleware wraps which routes,
// and in what order -- and a test that builds its own is testing its own
// opinion of that wiring rather than the one that ships. A level-4 test takes
// Handler and drives it over httptest; main takes the rest.
type application struct {
	// Handler is the fully wrapped router: every route registered, the whole
	// middleware stack applied, outermost first.
	Handler http.Handler

	// Renderer is returned because main installs the footer's globals on it
	// after the scheduler exists, which is after this function has run.
	Renderer *templates.Renderer

	// The services something outside buildHandler still reaches: main registers
	// a job on Bets and provisions an account through Admin, and SetClock needs
	// the four that read a clock. Auth and Leagues are not here because nothing
	// asks for them -- a level-3 test builds its own from the same *gorm.DB,
	// and a level-4 test is supposed to reach auth only through /register.
	Games      *games.Service
	Bets       *bets.Service
	Basketball *basketball.Service
	Admin      *admin.Service
}

// SetClock points everything in the process that reads "now" at one time source.
//
// Five things do: four services and the renderer, whose footer resolves the
// copyright year on its own. A page rendered against replayed fixtures has to
// get the same answer from all of them -- the sync page reports the
// scoreboard's state beside the current week, and the games grid and the bet
// slip disagree about whether a kickoff has passed if only one of them has been
// moved. Giving them separate instants is the failure this exists to make
// impossible to write by accident.
//
// Only a test calls this; in the server every one of these clocks is time.Now.
// It is not concurrency-safe and is not meant to be -- call it between
// buildHandler and the first request, the same contract SetBetEvaluator has.
func (a *application) SetClock(now func() time.Time) {
	a.Games.SetClock(now)
	a.Bets.SetClock(now)
	a.Basketball.SetClock(now)
	a.Admin.SetClock(now)
	a.Renderer.SetClock(now)
}

// buildHandler constructs the services, handlers, routes and middleware stack.
//
// assetFS is passed in rather than derived from cfg because the choice between
// the embedded copy and the working directory is main's: os.DirFS(".") is
// relative to the process's working directory, which is the repository root for
// the server and the package directory for a test.
//
// logger is passed in rather than taken from slog.Default because the two
// middlewares that use it are the only things in the process that log from
// outside a service, and main is where logging.Setup decided what a log line
// looks like.
//
// sched is passed in rather than created here because main owns its lifetime --
// it starts it, waits for it on shutdown, and registers the sync jobs on it once
// it knows whether there is an API key to sync with. The admin service holds it
// so the portal's "run now" drives the same job the timer does.
func buildHandler(
	cfg *config.Config,
	db *gorm.DB,
	location *time.Location,
	assetFS fs.FS,
	sched *scheduler.Scheduler,
	logger *slog.Logger,
) (*application, error) {
	renderer, err := templates.NewRenderer(assetFS, cfg.IsDevelopment(), location)
	if err != nil {
		return nil, fmt.Errorf("initialize templates: %w", err)
	}

	authService := auth.NewService(db, cfg)
	leaguesService := leagues.NewService(db)

	app := &application{
		Renderer:   renderer,
		Games:      games.NewService(db, location),
		Bets:       bets.NewService(db),
		Basketball: basketball.NewService(db, location),
	}
	app.Admin = admin.NewService(db, cfg, sched, app.Bets, app.Games)

	// Initialize handlers.
	authHandler := auth.NewHandler(authService, renderer)
	leaguesHandler := leagues.NewHandler(leaguesService, renderer)
	gamesHandler := games.NewHandler(app.Games, renderer, db)
	betsHandler := bets.NewHandler(app.Bets, renderer, db)
	// The bet slip asks the bets service which weeks already have a Holy Lock.
	gamesHandler.SetHolyLockReader(app.Bets)
	// The grid and detail page ask the bets service what the viewer has already
	// bet on each game.
	gamesHandler.SetUserBetReader(app.Bets)
	basketballHandler := basketball.NewHandler(app.Basketball, renderer)
	adminHandler := admin.NewHandler(app.Admin, renderer)

	// Set up router.
	mux := http.NewServeMux()

	// Serve static files from the same source as the templates.
	staticFS, err := fs.Sub(assetFS, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Register auth routes.
	authHandler.RegisterRoutes(mux)

	// Register admin routes (each one additionally requires the admin flag).
	adminHandler.RegisterRoutes(mux, auth.RequireAuth(authService))

	// Register leagues routes (requires authentication).
	leaguesHandler.RegisterRoutes(mux, auth.RequireAuth(authService))

	// Register games routes (requires authentication).
	gamesHandler.RegisterRoutes(mux, auth.RequireAuth(authService))

	// Register bets routes (requires authentication).
	betsHandler.RegisterRoutes(mux, auth.RequireAuth(authService))

	// Register basketball routes (requires authentication).
	basketballHandler.RegisterRoutes(mux, auth.RequireAuth(authService))

	// Home page.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		user := auth.UserFromContext(r.Context())
		data := map[string]any{
			"Title": "Home",
			"User":  user,
		}

		if err := renderer.Render(w, "home", data); err != nil {
			slog.Error("template render failed", "template", "home", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Health check endpoint.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Apply middleware stack. Order matters: the logger is outermost so it
	// still records the 500 that recoverPanics synthesises, and both sit outside
	// OptionalAuth so a panic in session handling is caught too.
	app.Handler = applyMiddleware(mux,
		requestLogger(logger),
		recoverPanics(logger),
		securityHeaders(cfg.IsProduction()),
		auth.OptionalAuth(authService),
	)

	return app, nil
}
