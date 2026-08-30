# Go Paper Betting Tracker

A web application for tracking paper bets on college football and college
basketball with friends. No real money is involved: each member of a league gets
a purse of play money and bets against the same lines the sportsbooks are
posting. Built with Go, HTMX, and PostgreSQL.

## Features

- **User authentication** — register, login, logout with session-based auth,
  bcrypt password hashing, and per-username login rate limiting
- **Leagues** — create public or private leagues, join by browsing or by invite
  code, each member funded from the league's starting balance
- **Betting** — moneyline, spread, and over/under bets against synced lines or a
  custom line, editable until kickoff, settled automatically once a game is final
- **Purses and standings** — every stake and payout moves a league purse; the
  league page ranks members by balance
- **College football and college basketball** — games, teams, venues, and lines
  synced from collegefootballdata.com and collegebasketballdata.com
- **Admin portal** — a single protected administrator account with control over
  users, leagues, purses, bets, game results, and the background syncs, with
  every mutation written to an audit log

## Tech Stack

- **Backend**: Go 1.27, net/http (method routing), GORM
- **Frontend**: HTMX 2.0.10 (vendored, no CDN), html/template
- **Database**: PostgreSQL, golang-migrate
- **Auth**: gorilla/sessions, bcrypt
- **Money**: shopspring/decimal — never float64
- **Deployment**: Docker; templates, static assets, and migrations are all
  embedded in the binary

## Architecture

The codebase follows a **layered architecture** with clear separation of concerns:

```
┌─────────────────────────────────────────────────────────┐
│                    HTTP Layer                           │
│              (handlers, middleware)                     │
├─────────────────────────────────────────────────────────┤
│                   Service Layer                         │
│               (business logic)                          │
├─────────────────────────────────────────────────────────┤
│                 Repository Layer                        │
│                (data access)                            │
├─────────────────────────────────────────────────────────┤
│                   Models Layer                          │
│              (data structures)                          │
└─────────────────────────────────────────────────────────┘
```

### Layer Responsibilities

| Layer | Location | Responsibility |
|-------|----------|----------------|
| **Models** | `internal/models/` | Pure data structures + GORM hooks |
| **Repository** | `internal/repository/` | Database queries and mutations |
| **Service** | `internal/{feature}/service.go` | Business logic, validation, authorization |
| **Handler** | `internal/{feature}/handler.go` | HTTP request/response, form parsing |

### Code Patterns

- **Feature-based organization** - Each feature (auth, leagues, admin) is self-contained
- **Dependency injection** - Services receive repositories, handlers receive services
- **Middleware chains** - Authentication and authorization via composable middleware
- **Repository pattern** - Database operations isolated from business logic

## Project Structure

```
├── cmd/
│   ├── server/              # Main application entry point
│   ├── seed/                # One-off CFB data seed
│   ├── seedcbb/             # One-off CBB data seed
│   ├── seedtestdata/        # Test users, leagues and bets for development
│   └── synccalendar/        # Backfill the week calendar for all seasons
├── internal/
│   ├── admin/               # Admin portal: users, leagues, bets, sync, audit
│   ├── auth/                # Login, register, sessions, middleware, rate limit
│   ├── basketball/          # Basketball browsing (calendar-day scoped)
│   ├── bets/                # Placing, editing, settling and admin correction
│   ├── cbbdata/             # collegebasketballdata.com client and sync
│   ├── cfbdata/             # collegefootballdata.com client, sync and cadence
│   ├── config/              # Environment loading and startup validation
│   ├── database/            # Connection pool and boot-time migrations
│   ├── games/               # Football game browsing and the week calendar
│   ├── leagues/             # League CRUD, membership, purses
│   ├── logging/             # slog setup
│   ├── models/              # Data structures only, plus GORM hooks
│   ├── repository/          # Data access layer
│   ├── scheduler/           # Named background jobs with status and triggers
│   ├── templates/           # Renderer, template funcs, asset versioning
│   └── timeutil/            # Calendar-day helpers that respect a location
├── migrations/              # SQL migrations (golang-migrate), embedded
├── templates/               # HTML templates, embedded
│   ├── layouts/             # Base layout
│   ├── pages/               # Full page templates
│   └── partials/            # HTMX fragments
├── static/                  # CSS and vendored JS, embedded
├── embed.go                 # //go:embed of templates/ and static/
├── docker-compose.yml       # Local development setup
├── Dockerfile               # Production build
├── Dockerfile.dev           # Development build
└── Makefile                 # Common commands
```

## Getting Started

### Prerequisites

- Go 1.27+
- Docker and Docker Compose
- Make (optional, but recommended)

### Local Development

1. **Clone the repository**

   ```bash
   git clone https://github.com/brian/paper-betting-with-friends.git
   cd paper-betting-with-friends
   ```

2. **Set up environment variables**

   ```bash
   cp env.example .env
   # Edit .env with your settings (the defaults work for Docker)
   ```

   `APP_TIMEZONE` (default `America/New_York`) is the zone the app resolves
   calendar days in — which games count as "today", what the date pager steps
   through — and the zone it falls back to for readers without JavaScript.
   Individual game times are converted to each visitor's own timezone in the
   browser, so this is not a per-user setting.

3. **Start the development environment**

   ```bash
   make dev
   ```

   This will:
   - Start PostgreSQL in a Docker container
   - Build and run the app with hot reload (via Air)
   - The app will be available at http://localhost:8080

   Pending migrations are applied automatically on startup, so there is no
   separate migration step. `make migrate-up` still exists and is useful for
   running them against a database without booting the app.

4. **Log in as the administrator**

   The account named by `ADMIN_USERNAME` (default `cfb-pbwf-admin`) is created
   on first boot using `ADMIN_PASSWORD`, so there is no window where the portal
   is reachable by whoever registers first. Both are set for you in
   `docker-compose.yml` for local development.

5. **Load some data**

   With `CFB_DATA_API_KEY` set, seed a season and give yourself something to bet
   on:

   ```bash
   make seed year=2025
   make seed-test-data
   ```

### Running Without Docker

If you prefer to run without Docker:

1. Start a PostgreSQL instance and update `DATABASE_URL` in `.env`

2. Install the development tools pinned by the go.mod `tool` directive:

   ```bash
   make tools
   ```

3. Run the server:

   ```bash
   air
   # or
   make run
   ```

## Available Commands

```bash
make help              # Show all available commands
make dev               # Start development with hot reload
make build             # Build production binary
make run               # Run the server locally (requires a reachable DATABASE_URL)
make test              # Run all tests (-race -cover)
make fmt               # Format code
make fmt-check         # Verify gofmt formatting (CI)
make fix-check         # Verify go fix has no pending modernizations (CI)
make vet               # go vet (CI)
make vulncheck         # Scan dependencies for known vulnerabilities (CI)
make tools             # Install tools pinned by the go.mod tool directive
make vendor-htmx       # Re-download vendored htmx and verify its checksum
make migrate-up        # Run migrations
make migrate-down      # Rollback last migration
make migrate-create name=xxx   # Create a new migration pair
make seed year=2025    # Seed CFB data for a season
make seedcbb season=2025       # Seed CBB data for a season
make seed-test-data    # Add test users, leagues and a mix of bets
make sync-calendar     # Backfill the week calendar for all seasons
```

Run the tests for one package directly:

```bash
go test -v -race ./internal/bets/...
```

## Data Sync

The application syncs game schedules, teams, venues, and betting lines from
[collegefootballdata.com](https://collegefootballdata.com) and
[collegebasketballdata.com](https://collegebasketballdata.com).

### Setup

1. Get an API key from each provider you want data from

2. Add them to your `.env`:

   ```bash
   CFB_DATA_API_KEY=your-api-key-here
   CBB_DATA_API_KEY=your-cbb-api-key-here
   ```

Each sync registers itself only when its key is present, so running with one
key configured is a supported way to run just one sport. A missing key logs a
warning at startup and disables that sport's sync.

### Initial Seed

Run a full seed to populate all data for a season:

```bash
make seed year=2024
```

This fetches and stores:
- All venues
- All teams (with logos, colors, conferences)
- Calendar weeks (regular + postseason)
- All games and results
- Betting lines from multiple providers (DraftKings, FanDuel, ESPN, Bovada, etc.)

### Background Sync

When `CFB_DATA_API_KEY` is set, the server syncs football games and betting
lines on a cadence that follows the football week. All times are wall-clock in
`APP_TIMEZONE`:

| When | Interval |
| --- | --- |
| Thursday, Friday, Saturday, 6am–midnight | every 15 minutes |
| Any other day, 6am–midnight | every 30 minutes |
| Sunday, midnight–2am | every 15 minutes |
| Otherwise (overnight, to 6am) | hourly |

Sunday keeps the game-day pace until 2am because Saturday's late West Coast
kickoffs are still being played and settled well after midnight Eastern.

Runs sit on a grid measured from midnight — :00, :15, :30, :45 — rather than
counting from whenever the process started, so a redeploy cannot shift the whole
schedule onto an arbitrary offset.

CFBD's free tier allows 5,000 calls per month and each run spends two, so the
schedule is a budget as much as a freshness setting. The cadence above costs
3,262–3,730 calls a month; a flat 15-minute poll costs about 5,760 and exhausts
the allowance on its own. `TestSyncCadenceStaysWithinMonthlyCallBudget` walks
real calendar months and fails if a change to the intervals overruns the plan.
The shape lives in `cfbdata.NextSync`.

The footer of every page shows when each sync last succeeded and when it is next
due. "Last succeeded" is deliberately not "last ran": a job that ran two minutes
ago and errored has not refreshed anything, and saying otherwise would be a lie
about how old the data is.

Success times are persisted to the `sync_state` table — one row per job, no
history — and restored on boot, so a deploy does not reset the footer to
"awaiting first update" while the data is in fact minutes old.

Basketball is polled on a flat interval instead, `CBB_SYNC_INTERVAL_MINS`
(default 15). Its provider is not metered the same way, and a basketball
schedule does not have football's single enormous Saturday to shape a cadence
around.

## Administration

There is exactly one administrator, named by `ADMIN_USERNAME` and provisioned
from the environment on every boot:

- If the account does not exist it is created, and startup **fails** if
  `ADMIN_PASSWORD` is unset — coming up with no way in is worse than not coming
  up at all.
- If it does exist it is forced back to admin, and the password is reset only
  when it no longer matches the stored hash. So `ADMIN_PASSWORD` is a lockout
  recovery lever rather than something rehashed on every restart, and changing
  it plus a restart is how you get back in.

The account cannot be renamed, demoted, or deleted through the portal, and there
is no UI anywhere for granting admin to anyone else. Resetting a password —
including through the portal — bumps that user's session version, which
immediately invalidates every session cookie already issued to them.

The portal covers users, leagues and purses, bets (including forcing a
settlement or voiding one, with the purse corrected by the difference), the game
and result inspector, and the background syncs, where each job can be triggered
by hand and reports its last run, last success, next due time and last error.
Every mutation is written to an audit log at `/admin/audit` with the actor and a
before/after detail.

## Deployment

The production image is built from `Dockerfile`: a multi-stage build producing a
static binary on Alpine, running as a non-root user. Templates, static assets,
and migrations are all compiled into the binary, so the image is the binary
alone and there is no way for the schema, the pages, and the code to arrive out
of step with each other.

### Environment

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | yes | PostgreSQL connection string |
| `SESSION_KEY` | yes | `openssl rand -hex 32`. Startup fails in production if this is missing, short, or left at the committed default |
| `ENV` | yes | Must be exactly `production`. This is what turns on the `Secure` flag for session cookies and HSTS, and turns off SQL logging |
| `ADMIN_USERNAME` | no | Defaults to `cfb-pbwf-admin` |
| `ADMIN_PASSWORD` | first boot | Required to create the account; afterwards only used to reset a lost password |
| `CFB_DATA_API_KEY` | per sport | Football sync is disabled without it |
| `CBB_DATA_API_KEY` | per sport | Basketball sync is disabled without it |
| `APP_TIMEZONE` | no | Defaults to `America/New_York` |
| `DB_MAX_OPEN_CONNS` | no | Defaults to 20. Must leave headroom under the database's own connection limit |
| `PORT` | no | Defaults to 8080; most platforms inject this |
| `MIGRATE_ON_START` | no | Defaults to true — see below |

`Config.Validate` runs before anything else and refuses to start on the failures
that would otherwise be completely silent: a forgeable session key, or an `ENV`
value that is neither `development` nor `production` and so gets treated as
neither.

### Migrations

Pending migrations are applied during startup, before anything reads a table.
golang-migrate takes a Postgres advisory lock while it works, so concurrent
starts serialize rather than race.

A migration that fails leaves the schema marked dirty, and the server then
refuses to boot rather than serve against a half-applied schema. That is the one
case `MIGRATE_ON_START=false` exists for: it gets a server up so you can go and
look, and it is not something to leave set.

### Running it

The app needs **exactly one instance, and it must not scale to zero.** The
background syncs run in-process on a scheduler inside the server:

- More than one instance means every sync runs several times over, against APIs
  that are metered monthly.
- An instance that sleeps when idle stops syncing. Scores never arrive, so bets
  never settle, and nothing anywhere reports an error — the jobs are simply not
  running.

On a platform with an idle-sleep feature (Railway's App Sleeping, for example),
turn it off, and keep replicas at 1.

`GET /health` returns 200 with `{"status":"ok"}` for platform health checks. It
deliberately does not check the database: a liveness probe that fails on a brief
database blip converts a recoverable hiccup into a restart loop.

The image carries no `HEALTHCHECK` directive, because the port comes from `PORT`
at runtime and a baked-in URL would be wrong on any host that assigns one.

### Before going live

- Confirm database backups are enabled — no managed provider guarantees them by
  default on every plan.
- Registration at `POST /register` is **open to anyone who can reach the app.**
  If that is not what you want, gate it before the URL is public.

## License

[MIT](LICENSE)
