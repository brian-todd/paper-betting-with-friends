package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"
	// Embed the IANA timezone database so LoadLocation works regardless of
	// whether the runtime image ships tzdata.
	_ "time/tzdata"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/admin"
	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/basketball"
	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/cbbdata"
	"github.com/brian/paper-betting-with-friends/internal/cfbdata"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/database"
	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/leagues"
	"github.com/brian/paper-betting-with-friends/internal/logging"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/brian/paper-betting-with-friends/migrations"
	"gorm.io/gorm"
)

// syncRunTimeout bounds a single background sync run.
const syncRunTimeout = 5 * time.Minute

// calendarRunTimeout bounds a single calendar sync run, which covers many
// seasons and so takes longer than an incremental game sync.
const calendarRunTimeout = 10 * time.Minute

// seedRunTimeout bounds a full seed. A seed walks venues, teams, the calendar
// and then every game and line of a season, so it is far longer than any
// incremental run -- and it is triggered by hand, not on a schedule, so a
// generous bound costs nothing.
const seedRunTimeout = 30 * time.Minute

func main() {
	// Load configuration.
	cfg := config.Load()

	// Install the structured logger before anything captures slog.Default.
	logger := logging.Setup(cfg.Env)
	logger.Info("starting server", "env", cfg.Env, "port", cfg.Port)

	// Refuse to start on configuration that would be unsafe but silent. A
	// forgeable session key or an unrecognised ENV produces a server that looks
	// entirely healthy while authenticating nobody.
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// The zone the app reasons about calendar days in. Display times are
	// converted per-reader in the browser; this only fixes day boundaries and
	// the no-JavaScript fallback, so an unknown zone degrades rather than fails.
	location, err := cfg.LoadLocation()
	if err != nil {
		logger.Warn("falling back to UTC", "error", err)
	}
	logger.Info("using timezone", "timezone", location.String())

	// Connect to database.
	db, err := database.Connect(cfg)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close(db)

	// Apply pending migrations before anything reads a table. The schema and the
	// binary that expects it then ship together, so there is no window where a
	// new build is serving against an old schema.
	if cfg.MigrateOnStart {
		if err := database.Migrate(cfg.DatabaseURL, migrations.FS); err != nil {
			logger.Error("failed to apply migrations", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Warn("MIGRATE_ON_START is off; the schema is assumed to be current")
	}

	// Templates and static files are compiled into the binary, so a deployment
	// is one artefact and there is no way for the two to arrive out of step. In
	// development they are read from the working directory instead, so editing
	// either takes effect without a rebuild.
	var assetFS fs.FS = assets.FS
	if cfg.IsDevelopment() {
		assetFS = os.DirFS(".")
	}

	// Initialize template renderer.
	renderer, err := templates.NewRenderer(assetFS, cfg.IsDevelopment(), location)
	if err != nil {
		logger.Error("failed to initialize templates", "error", err)
		os.Exit(1)
	}

	// Background syncs stop when this context is canceled, which also cancels
	// any sync already in flight.
	syncCtx, stopSyncs := context.WithCancel(context.Background())
	defer stopSyncs()

	// The scheduler is built before the services because the admin service holds
	// it: the portal's "run now" goes through the same job the timer drives, so
	// a manual run gets the same timeout, status and success recording.
	sched := scheduler.New(logger)

	// Initialize services.
	authService := auth.NewService(db, cfg)
	leaguesService := leagues.NewService(db)
	gamesService := games.NewService(db, location)
	betsService := bets.NewService(db)
	basketballService := basketball.NewService(db, location)

	registerSyncJobs(sched, cfg, location, db, betsService, logger)

	adminService := admin.NewService(db, cfg, sched, betsService, gamesService)

	// The administrator is provisioned from the environment rather than through
	// a sign-up flow, so a fresh deployment has exactly one way in and no window
	// where the portal is reachable by whoever registers first.
	if err := adminService.EnsureAdminUser(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Error("failed to provision administrator account", "username", cfg.AdminUsername, "error", err)
		os.Exit(1)
	}

	// Initialize handlers.
	authHandler := auth.NewHandler(authService, renderer)
	leaguesHandler := leagues.NewHandler(leaguesService, renderer)
	gamesHandler := games.NewHandler(gamesService, renderer, db)
	betsHandler := bets.NewHandler(betsService, renderer, db)
	basketballHandler := basketball.NewHandler(basketballService, renderer)
	adminHandler := admin.NewHandler(adminService, renderer)

	// Set up router.
	mux := http.NewServeMux()

	// Serve static files from the same source as the templates.
	staticFS, err := fs.Sub(assetFS, "static")
	if err != nil {
		logger.Error("failed to open static assets", "error", err)
		os.Exit(1)
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
	handler := applyMiddleware(mux,
		requestLogger(logger),
		recoverPanics(logger),
		securityHeaders(cfg.IsProduction()),
		auth.OptionalAuth(authService),
	)

	// Create server.
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Last-success times are persisted so they survive a restart. Without this
	// every deploy would report the data as never refreshed, which is both
	// wrong and most misleading exactly when someone is checking whether the
	// new build's syncs are alive.
	syncStateRepo := repository.NewSyncStateRepository(db)
	if successes, err := syncStateRepo.FindAll(); err != nil {
		logger.Error("failed to load sync state", "error", err)
	} else {
		sched.RestoreSuccesses(successes)
	}
	sched.SetSuccessRecorder(func(job string, at time.Time) {
		if err := syncStateRepo.RecordSuccess(job, at); err != nil {
			logger.Error("failed to record sync success", "job", job, "error", err)
		}
	})

	// The footer reports when each sync last refreshed and when it is next due,
	// so the data's age is visible on every page rather than being something a
	// reader has to guess at from a stale-looking score.
	renderer.SetGlobals(func() map[string]any {
		return map[string]any{"SyncStatus": sched.PublicStatus()}
	})

	sched.Start(syncCtx)

	// Start server in goroutine.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Wait for an interrupt signal or a fatal server error.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logger.Error("server error", "error", err)
		stopSyncs()
		sched.Wait()
		os.Exit(1)
	case sig := <-quit:
		logger.Info("shutting down", "signal", sig.String())
	}

	// Stop background syncs and wait for in-flight runs to unwind.
	stopSyncs()
	sched.Wait()

	// Allow 30 seconds for graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

// registerSyncJobs adds the periodic data syncs that have an API key configured.
func registerSyncJobs(sched *scheduler.Scheduler, cfg *config.Config, location *time.Location, db *gorm.DB, evaluator *bets.Service, logger *slog.Logger) {
	if cfg.CFBDataAPIKey != "" {
		syncService := cfbdata.NewSyncService(cfbdata.NewClient(cfg.CFBDataAPIKey), db)
		syncService.SetBetEvaluator(evaluator)

		// The football feed is polled on a cadence that tracks the week rather
		// than a fixed interval, because CFBD meters us monthly and a flat poll
		// spends the whole allowance on Tuesday mornings. SYNC_INTERVAL_MINS no
		// longer applies to this job.
		sched.Add(scheduler.Job{
			Name:  "cfb-games-and-lines",
			Label: "Football",
			NextDelay: func(now time.Time) time.Duration {
				return cfbdata.SyncDelay(now, location)
			},
			Timeout: syncRunTimeout,
			Run: func(ctx context.Context) error {
				return syncService.SyncGamesAndLines(ctx, syncService.GetCurrentSeasonYear(), nil, nil)
			},
		})

		sched.Add(scheduler.Job{
			Name:     "cfb-calendar",
			Interval: 24 * time.Hour,
			Timeout:  calendarRunTimeout,
			Run:      syncService.SyncAllCalendars,
		})

		// Teams and venues are synced by nothing else. The periodic job covers
		// games and lines only, and syncGames skips any game whose teams it
		// cannot find -- so on an unseeded database every sync reports success
		// and stores nothing. This is the one action that fills that gap, and
		// it is manual because it is needed once a season, not on a timer.
		sched.Add(scheduler.Job{
			Name:       "cfb-seed",
			ManualOnly: true,
			Timeout:    seedRunTimeout,
			Run: func(ctx context.Context) error {
				return syncService.SeedAll(ctx, seedSeason(ctx, syncService.GetCurrentSeasonYear()), nil, nil)
			},
		})
	} else {
		logger.Warn("CFB_DATA_API_KEY not set, football sync disabled")
	}

	if cfg.CBBDataAPIKey != "" {
		cbbSyncService := cbbdata.NewSyncService(cbbdata.NewClient(cfg.CBBDataAPIKey), db)
		cbbSyncService.SetBetEvaluator(evaluator)

		sched.Add(scheduler.Job{
			Name:     "cbb-games-and-lines",
			Label:    "Basketball",
			Interval: time.Duration(cfg.CBBSyncIntervalMins) * time.Minute,
			Timeout:  syncRunTimeout,
			Run:      cbbSyncService.SyncGamesAndLines,
		})

		// As above, and more so: the basketball incremental sync only looks at
		// a few days either side of today, so the season only exists at all if
		// something seeded it.
		sched.Add(scheduler.Job{
			Name:       "cbb-seed",
			ManualOnly: true,
			Timeout:    seedRunTimeout,
			Run: func(ctx context.Context) error {
				return cbbSyncService.SeedAll(ctx, seedSeason(ctx, cbbdata.GetCurrentSeason()))
			},
		})
	} else {
		logger.Warn("CBB_DATA_API_KEY not set, basketball sync disabled")
	}
}

// seedSeason is the season a manual seed was asked for, or fallback when the
// trigger named none.
//
// The value has already been validated by the admin service, which is where an
// operator can be told what went wrong. Reaching here with something
// unparseable would mean a caller bypassed that, so the safe reading is the
// current season rather than a year nobody asked for.
func seedSeason(ctx context.Context, fallback int) int {
	args := scheduler.ArgsFrom(ctx)
	if len(args) == 0 {
		return fallback
	}

	year, err := strconv.Atoi(args[0])
	if err != nil {
		slog.Warn("ignoring unparseable seed season", "value", args[0], "using", fallback)
		return fallback
	}
	return year
}

// applyMiddleware applies middleware in reverse order (first middleware wraps outermost).
func applyMiddleware(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for _, m := range slices.Backward(middleware) {
		h = m(h)
	}
	return h
}
