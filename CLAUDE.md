# CLAUDE.md

## Commands

- `make dev` — Docker Compose with Air hot reload
- `make test` — run all tests (`go test -v -race -cover ./...`)
- `go test -v -race ./internal/bets/...` — run tests for one package
- `make fmt` — format code
- `make fmt-check` / `make vet` / `make vulncheck` — the checks CI runs
- `make tools` — download tools pinned by the go.mod `tool` directive
- `make migrate-up` / `make migrate-down` — run/rollback migrations
- `make migrate-create name=<name>` — create new migration pair
- `make seed year=YYYY week=N seasonType=regular` — seed CFB data
- `make seedcbb season=YYYY` — seed CBB data
- `make vendor-htmx` — re-download the vendored htmx build and verify its checksum

## Architecture

Layered: **Handler → Service → Repository → Models**

- Feature packages live under `internal/` (auth, bets, leagues, games, basketball, admin)
- Shared data access in `internal/repository/`
- External API sync in `internal/cfbdata/` and `internal/cbbdata/`
- Entry points in `cmd/` (server, seed, seedcbb, seedtestdata, synccalendar)
- Periodic background syncs run via `internal/scheduler`
- Logging setup in `internal/logging`; HTTP middleware in `cmd/server/middleware.go`
- Templates in `templates/` (layouts, pages, partials); static assets in `static/`
- Templates, static assets and migrations are all `//go:embed`ed into the binary
  (`embed.go` at the module root, `migrations/embed.go`), so a deployment is one
  artefact. In development they are read from disk instead — see Templates.

## Code Conventions

### Constructors

Services take `*gorm.DB` and create their own repositories internally. Handlers take a service and renderer.

```go
func NewService(db *gorm.DB) *Service {
    return &Service{
        gameRepo: repository.NewGameRepository(db),
    }
}

func NewHandler(service *Service, renderer *templates.Renderer) *Handler {
    return &Handler{service: service, templates: renderer}
}
```

### Route Registration

Each handler has `RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler)`. Uses Go 1.22+ method routing syntax. Extract path params with `r.PathValue("id")`.

```go
mux.Handle("GET /leagues", authMiddleware(http.HandlerFunc(h.Index)))
mux.Handle("POST /leagues/{id}/join", authMiddleware(http.HandlerFunc(h.Join)))
```

### Error Handling

- Define package-level sentinel errors at the top of service.go: `var ErrXxx = errors.New("...")`
- Check with `errors.Is(err, ErrXxx)` — never type assertions
- Translate `gorm.ErrRecordNotFound` into domain errors in the service layer
- Handlers convert domain errors to user-facing messages via `errors.Is()` switches

### Models

- UUID primary keys: `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
- Every model needs a `BeforeCreate` hook that sets UUID if zero-value
- Use `decimal.Decimal` (shopspring) for all money and odds values — never float64
- Typed string constants for enums (e.g., `type BetStatus string`)

### HTMX

Handlers check `r.Header.Get("HX-Request") == "true"` to decide between returning an HTML fragment or doing a standard redirect. Use `renderer.Render()` for full pages, `renderer.RenderPartial()` for HTMX fragments.

htmx is pinned at 2.0.10 and vendored to `static/js/htmx.min.js` rather than
loaded from a CDN, so the app has no third-party runtime dependency. `HTMX_VERSION`
and `HTMX_SHA256` in the Makefile are the source of truth — bump both together and
run `make vendor-htmx` to upgrade.

htmx discards the body of a 4xx by default, so a handler that renders a
validation message with `http.StatusBadRequest` will silently show the user
nothing. The `htmx-config` meta tag in `templates/layouts/base.html` opts 400
and 422 into being swapped; keep returning real status codes rather than
downgrading errors to 200. It also sets `reportValidityOfForms: true`, without
which htmx blocks a request that fails client-side constraint validation but
never surfaces the browser's message.

When a handler answers an HTMX request with an error, render a *fragment*
(`RenderPartial`) targeted at a dedicated slot — `renderAuthError` in
`internal/auth/handler.go` is the reference. `Render` emits the full base
layout, which nests the whole page inside the swap target.

### HTTP Middleware

The stack is assembled in `main` with `applyMiddleware`, where the **first
entry wraps outermost**. Order is load-bearing:

```go
handler := applyMiddleware(mux,
    requestLogger(logger),
    recoverPanics(logger),
    securityHeaders(cfg.IsProduction()),
    auth.OptionalAuth(authService),
)
```

`requestLogger` stays outside `recoverPanics` so its log line still records the
500 that a recovered panic produces.

`recoverPanics` exists because `net/http`'s own recovery is close to the worst
available outcome: it drops the connection with no response and writes the trace
through the standard logger, so the one incident worth alerting on never reaches
the structured logs. It re-panics on `http.ErrAbortHandler` (an intentional
abort, not an error) and leaves an already-started response alone, since a
template that panics mid-render has already sent a 200 and a partial body.

`securityHeaders` sends HSTS only in production — pinning `https://` for a year
against a localhost served over plain HTTP is a per-browser chore to undo. The
CSP carries `'unsafe-inline'` for scripts and styles because the templates
genuinely use inline `<script>` blocks, `onclick`/`onchange` attributes and
`style=` attributes; it is therefore not an XSS defence, and the directives that
do earn their place are `frame-ancestors`, `form-action`, `base-uri` and
`object-src`. `img-src` must allow arbitrary https origins because team logo
URLs come from the data provider.

The policy does **not** grant `'unsafe-eval'`, which rules out the htmx
attributes that htmx compiles with `new Function`: `hx-on`, `hx-vals="js:..."`,
`hx-headers="js:..."`, and `hx-trigger` event filters like `click[ctrlKey]`.
Bind an ordinary listener in a `<script>` block instead — `game_detail.html`
attaches `htmx:afterRequest` that way. This breaks silently in the browser with
nothing to see server-side, so `TestTemplatesAvoidHtmxFeaturesNeedingUnsafeEval`
fails the build if one reappears.

### Templates

Pass `map[string]any` with `"User"` key (from auth context). Pages are in `templates/pages/*.html` using the layout at `templates/layouts/base.html`.

`NewRenderer` takes an `fs.FS`, not a directory path. `main` passes `assets.FS`
(embedded) in production and `os.DirFS(".")` in development, so editing a
template or the CSS still takes effect without a rebuild.

The `asset` function versions a URL by **hashing the file's contents**, never
its mtime: every file in an `embed.FS` reports the zero time, so an
mtime-derived version would collapse every asset onto one value that never
changes and pin stale CSS in browser caches forever.

### Time and Timezones

Timestamps are stored as `TIMESTAMPTZ` and handled as absolute instants. The
server process runs in UTC (no `TZ` is set in any image), so **never** render a
`time.Time` with `.Format` in a template — it will print UTC and read as the
wrong day for anyone east or west of it.

Use the `localTime` template function instead:

```html
{{localTime .Game.ScheduledAt "datetime"}}
```

It emits `<time datetime="<RFC3339 UTC>" data-format="...">`, and
`static/js/localtime.js` rewrites the text in the reader's own timezone. The
server-rendered text inside the element is the same instant in `APP_TIMEZONE`,
so a browser without JavaScript still sees something sensible. Format names live
in `timeLayouts` (`internal/templates/templates.go`) and must have a matching
entry in `FORMATS` in the JS; `TestPageTemplatesOnlyUseKnownTimeFormats` fails
if a template uses a name that does not exist.

The exception is a value that is already a *calendar day* rather than an instant
— the selected date in `basketball_games.html`, for example. Those are resolved
server-side in `APP_TIMEZONE` and must not be shifted per reader.

Calendar-day decisions ("today's games", the date pager) need an explicit
`*time.Location`, threaded from `cfg.LoadLocation()` in `main`. They belong in
the service, not the handler — see `basketball.Service.Today` / `ParseDate`.

- Use `timeutil.StartOfDay(t, loc)`, never `t.Truncate(24 * time.Hour)`, which
  works on absolute duration since the zero time and so always snaps to UTC
  midnight whatever location `t` carries
- Use `time.ParseInLocation` for `YYYY-MM-DD` input, never `time.Parse`
- Advance days with `AddDate(0, 0, 1)`, not `Add(24 * time.Hour)` — a DST day is
  23 or 25 hours long
- Comparing two instants (`game.ScheduledAt.Before(time.Now())`) is
  timezone-independent and needs no location

`cmd/server` imports `_ "time/tzdata"` so `LoadLocation` works even on an image
without the tzdata package.

### Purse Operations

Stake deduction is atomic — `DeductStake` uses a WHERE clause checking `balance >= amount`. On bet creation failure after deduction, always refund: `_ = s.purseRepo.CreditWinnings(...)`.

Editing a bet moves only the *difference* in stake, via `adjustStake`. Raising a
$10 bet to $15 needs $5 free, not the $15 a refund-and-recharge would briefly
require. Roll it back the same way if the bet then fails to save.

### Bets

A bet stores both the odds row it came from and a snapshot of the numbers at
that moment, so a later line move never changes what was agreed. Placing and
editing resolve that pair through the same `resolve*Odds` helpers in
`internal/bets/lines.go` — don't reimplement the custom-odds branch in a new
caller.

`editable()` mirrors `authorizeEdit()`. They must agree, or the page offers an
edit the service then refuses. Both gate on `game.ScheduledAt`, not
`game.Status`, because status only advances when the sync runs.

The bet repositories' `Update` omits associations:

```go
return r.db.Omit(clause.Associations).Save(bet).Error
```

`FindByID` preloads the odds row, and a plain `Save` writes that preloaded row's
ID back over the foreign key — so moving a bet to a different line silently kept
it pointing at the old one while the snapshot changed.

### Game Results

A `GameResult` row is written whenever the provider reports a score, which need
not wait for the game to end, so the row's presence does **not** mean the game
is over.

`FinalizedAt` is the finality flag: nil while the score is provisional, set when
the provider calls the game complete. `EvaluateBetsForGame` refuses to settle
against a result where `IsFinal()` is false — otherwise a halftime lead pays out.
Anything new that reads a score has to decide which of the two it wants.

**No live scores actually arrive today.** CFBD's `/games` returns null points
until `completed` flips true, so for football the nil-`FinalizedAt` path never
runs. Live football scores are on `/scoreboard` (with `period`, `clock`,
`possession`), which is FBS-only and deliberately not synced. The basketball feed
reports a real status and filters on `status=in_progress`, so it may well carry
live points — unverified, as it was checked out of season. Templates mark a
non-final score with a `live` class; that styling is currently unreachable for
football.

Football `Game.Status` is *inferred* (`now > startDate + 5min`), not reported, so
a card reading "Live" may be a finished game the feed has not updated. Basketball
takes its status from the API.

### Testing

- Table-driven tests with `t.Run()` subtests
- Use `testing.T`, `httptest.NewRequest`, `httptest.NewRecorder` — no mocking framework
- Decimal test values: `decimal.RequireFromString("150")`

### Logging

Use `log/slog`, never the bare `log` package. `internal/logging.Setup` installs
the default logger (JSON in production, text in development) and must be called
before anything captures `slog.Default`.

- Message is a lowercase, static string; variables go in key/value attrs
- Errors use `slog.Error("failed to do x", "error", err)`
- Long-lived services take a scoped child logger, e.g.
  `slog.Default().With("component", "cfb-sync")`
- `cmd/` binaries do work in a `run() error` so deferred cleanup runs; `main`
  logs the error and calls `os.Exit(1)` (no `log.Fatal`, which skips defers)

### Background Jobs

Periodic work is registered as a `scheduler.Job` (name, interval, per-run
timeout, `Run func(context.Context) error`) rather than a bare `time.Ticker`
goroutine. Jobs stop on context cancellation, which also cancels an in-flight
run. Test them with `testing/synctest` and `synctest.Sleep` — no real sleeps.

The scheduler runs every job through `safeRun`, which recovers a panic and
returns it wrapped in `ErrJobPanic`. Jobs run on goroutines this package owns,
and an unrecovered panic on one of those takes down the whole process — so a
malformed field in a single upstream response would stop the server serving
pages. The recovered error lands in `Status.LastError` for the admin page and
the stack goes to the log, where it is actually readable.

A job can also be run on demand: `Trigger(name)` sends on a capacity-1 channel,
and that buffer *is* the debounce — a second trigger while one is pending
returns `ErrRunPending` rather than queuing a second call against a metered API.

### The Week Calendar

Anything that resolves "what season or week is it now" must first drop weeks
whose span cannot be real — `models.Week.Plausible()`, bounded by
`models.MaxWeekSpan` (90 days). A single row with an end date a year past its
start contains every instant in between, so it wins any containing-week check
and hides the whole calendar behind it.

There are two consumers, and they must both apply the rule:

- `games.Service.GetCurrentWeek` filters in Go via `plausibleWeeks`
- `repository.WeekRepository.FindSeasonContainingDate` filters in SQL, and
  feeds `cfbdata.GetCurrentSeasonYear` — which decides *which season the
  background sync fetches*

Missing it in the second one is worse than in the first: the UI merely points
at the wrong week, but the sync silently pulls a year that contains none of the
games being watched, so scores never arrive and bets never settle while every
job still logs success.

Compare the span in SQL with `end_date <= start_date + make_interval(secs => ?)`
and pass `MaxWeekSpan.Seconds()`. A bare `time.Duration` binds as an int64 and
Postgres rejects `interval <= bigint`.

### Migrations

- Zero-padded sequential: `NNNNNN_description.up.sql` / `.down.sql`
- Always provide both up and down migrations, and neither file may be empty —
  an empty down migration applies cleanly and does nothing, which is the worst
  way for a rollback to fail
- PostgreSQL SQL dialect
- Use `make migrate-create name=<description>` to create new pairs

The SQL is embedded via `migrations/embed.go` and applied by
`database.Migrate` during startup, before anything reads a table, so the binary
and the schema it expects ship together. golang-migrate takes a Postgres
advisory lock, so concurrent starts serialize rather than race.

`Migrate` opens its **own** short-lived connection from `DATABASE_URL` rather
than reusing gorm's pool, because golang-migrate's `Close()` closes the
`*sql.DB` it is given — handing it the app's pool would tear down the
connection the server is about to run on.

A failed migration leaves the schema dirty and the server then refuses to boot.
`MIGRATE_ON_START=false` is the escape hatch for exactly that case; it is not
something to leave set. The files stay on disk so `make migrate-up` and the
embedded copy read the same directory.

## Things to Avoid

- float64 for money or odds — always `decimal.Decimal`
- Business logic in handlers — put it in the service layer
- Calling repositories directly from handlers
- GORM AutoMigrate — use SQL migrations only
- Repository interfaces — concrete types are used throughout
- The bare `log` package — use `log/slog`
- `log.Fatal` in `cmd/` — it skips deferred cleanup
- Raw `time.Ticker` goroutines for recurring work — use `internal/scheduler`
- Loading frontend libraries from a CDN — vendor them under `static/` with a pinned checksum
- `hx-on` and other htmx attributes evaluated with `new Function` — the CSP has no `'unsafe-eval'`, so they fail silently in the browser
- `.Format` on a `time.Time` in a template — use `localTime`, or the server's UTC leaks into the UI
- `Truncate(24 * time.Hour)` or `Add(24 * time.Hour)` for calendar days — use `timeutil.StartOfDay` and `AddDate`
- Bare `db.Save(bet)` in a bet repository — a preloaded association overwrites the foreign key; `Omit(clause.Associations)`
- Treating a `GameResult` as final — check `IsFinal()`, or bets settle on a live score
- Trusting stored week dates unchecked — filter on `models.Week.Plausible()` in *every* path that asks "which season/week is it now"
- Calling `j.Run` directly, or starting any goroutine whose panic nothing recovers — one takes down the whole process
- `.Format`-style mtime cache busting for assets — every embedded file reports the zero mtime; hash the contents
- Unbounded database pools — `database.Connect` sets the limits, and `DB_MAX_OPEN_CONNS` has to stay under the server's own cap
- Assuming a route is admin-only because it lives in `internal/admin` — it is only guarded if it was registered through `guard` in `RegisterRoutes`
