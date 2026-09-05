# Go Paper Betting Tracker

A web application for tracking paper bets on college football and college
basketball with friends. Each member of a league gets a purse of play money and
bets against the lines sportsbooks are actually posting; no real money is
involved.

Built with Go 1.27 (net/http method routing, GORM), htmx, and PostgreSQL. Money
and odds are `shopspring/decimal` throughout, never `float64`.

## Features

- **Leagues** — public or private, joined by browsing or by invite code, each
  member funded from the league's starting balance
- **Betting** — moneyline, spread, and over/under against synced lines or a
  custom line, editable until kickoff, settled automatically once a game is final
- **Purses and standings** — every stake and payout moves a league purse
- **Two sports** — games, teams, venues, and lines synced from
  collegefootballdata.com and collegebasketballdata.com
- **Admin portal** — one protected administrator with control over users,
  leagues, purses, bets, results, and the background syncs, with every mutation
  written to an audit log

## Architecture

Layered **handler → service → repository → models**, with feature packages under
`internal/` (auth, leagues, bets, games, basketball, admin).

| Layer | Location | Responsibility |
|-------|----------|----------------|
| Models | `internal/models/` | Data structures and GORM hooks |
| Repository | `internal/repository/` | Database queries and mutations |
| Service | `internal/{feature}/service.go` | Business logic, validation, authorization |
| Handler | `internal/{feature}/handler.go` | HTTP request/response, form parsing |

Templates, static assets, and SQL migrations are all embedded in the binary, so
a deployment is a single artefact. In development they are read from disk
instead, so edits take effect without a rebuild.

`AGENTS.md` documents the conventions in detail — error handling, the timezone
rules, purse arithmetic, and the sharp edges worth knowing before changing
anything.

## Getting Started

**Prerequisites:** Go 1.27+, Docker and Docker Compose, Make.

```bash
cp env.example .env      # defaults work for Docker
make dev                 # Postgres + app with hot reload on :8080
```

Migrations are applied automatically at startup, so there is no separate
migration step.

Log in as the administrator named by `ADMIN_USERNAME` (default
`cfb-pbwf-admin`), which `docker-compose.yml` provisions for local development.
Then, with `CFB_DATA_API_KEY` set, load something to bet on:

```bash
make seed year=2025      # venues, teams, calendar, games, lines
make seed-test-data      # test users, leagues, and a mix of bets
```

`make help` lists every target. The checks CI runs are `make fmt-check`,
`make fix-check`, `make vet`, `make test`, and `make vulncheck`.

To run without Docker, point `DATABASE_URL` at your own PostgreSQL, then
`make tools` and `make run`.

## Data Sync

Each sport's sync registers itself only if its API key is present, so running
one sport is supported; a missing key logs a warning and disables that sync.

Football's games-and-lines sync is polled on a cadence that follows the
football week rather than a flat interval, in `APP_TIMEZONE` wall-clock:

| When | Interval |
| --- | --- |
| Thursday–Saturday, 6am–midnight | every 15 minutes |
| Any other day, 6am–midnight | every 30 minutes |
| Sunday, midnight–2am | every 15 minutes |
| Otherwise (overnight, to 6am) | hourly |

Sunday holds the game-day pace until 2am because Saturday's late West Coast
kickoffs are still being settled well after midnight Eastern. Runs sit on a grid
measured from midnight, so a redeploy cannot shift the schedule onto an
arbitrary offset. The shape lives in `cfbdata.NextSync`.

**The cadence is a budget, not just a freshness setting.** CFBD meters us at
30,000 calls a month, shared across every job below. Rough in-season cost of
each, at default configuration (FBS-only scoreboard, `CBB_SYNC_INTERVAL_MINS`
unset):

| Job | Cadence | Calls/run | ~Calls/month |
| --- | --- | --- | --- |
| `cfb-games-and-lines` | table above | 2 (`/games` + `/lines`) | ~3,550 |
| `cfb-scoreboard` | 5 min in-season, hourly off-season, one call per division | 1 | ~8,640 |
| `cfb-calendar` | daily, loops years until the API returns empty | ~25 | ~750 |
| `cfb-rankings` | every 6 hours | 1 | ~120 |
| `cbb-games-and-lines` | flat `CBB_SYNC_INTERVAL_MINS` (default 15), no seasonal throttle | 2 (`/games` + `/lines`) | ~5,760 |
| **Total** | | | **~18,800 / 30,000** |

That leaves roughly a third of the budget as headroom. Two things to watch if
this changes:

- `TestFootballCadenceStaysWithinMonthlyCallBudget` (`internal/cfbdata`) only
  covers the two football jobs, capped at 24,000 rather than the real 30,000,
  to leave room for calendar, rankings, and basketball. It's tested against
  `CFB_SCOREBOARD_CLASSIFICATIONS` set to two divisions (the widest an operator
  would plausibly configure), which alone would consume nearly all of that
  24,000 — so adding a second division is not free, and there is no test
  guarding the combined total across both sports.
- Unlike football, `cbb-games-and-lines` has no seasonal throttle: it polls
  flat year-round, including the CBB offseason (spring/summer), so a chunk of
  its ~5,760/month is spent returning near-empty results outside
  November–April.

Every page footer reports when each sync last *succeeded*, deliberately not when
it last *ran*: a job that ran two minutes ago and errored has refreshed nothing,
and reporting otherwise would lie about how old the data is. Success times are
persisted to `sync_state` and restored on boot, so a deploy does not reset the
footer while the data is in fact minutes old.

## Administration

There is exactly one administrator, provisioned from the environment on every
boot. If the account is missing it is created, and startup **fails** when
`ADMIN_PASSWORD` is unset — coming up with no way in is worse than not coming up
at all. If it exists, it is forced back to admin and the password is reset only
when it no longer matches the stored hash, which makes `ADMIN_PASSWORD` a
lockout-recovery lever rather than something rehashed on every restart.

The account cannot be renamed, demoted, or deleted through the portal, and
nothing anywhere grants admin to anyone else. Resetting a password bumps that
user's session version, immediately invalidating every cookie already issued to
them.

The portal covers users, leagues and purses, bets (including forcing a
settlement or voiding one, with the purse corrected by the difference), a game
and result inspector, and the background syncs — each triggerable by hand and
reporting last run, last success, next due, and last error. Every mutation is
audited at `/admin/audit`.

## Deployment

The production image is a static binary on Alpine running as a non-root user.
Since templates, assets, and migrations are embedded, the image is the binary
alone and the schema cannot arrive out of step with the code.

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | yes | PostgreSQL connection string |
| `SESSION_KEY` | yes | `openssl rand -hex 32`. Startup fails in production if missing, short, or left at the committed default |
| `ENV` | yes | Exactly `production`. Gates the `Secure` cookie flag, HSTS, and SQL logging |
| `ADMIN_USERNAME` | no | Defaults to `cfb-pbwf-admin` |
| `ADMIN_PASSWORD` | first boot | Required to create the account; later only to reset a lost password |
| `CFB_DATA_API_KEY` | per sport | Football sync disabled without it |
| `CBB_DATA_API_KEY` | per sport | Basketball sync disabled without it |
| `APP_TIMEZONE` | no | Defaults to `America/New_York` |
| `DB_MAX_OPEN_CONNS` | no | Defaults to 20; must leave headroom under the database's own limit |
| `PORT` | no | Defaults to 8080; most platforms inject this |
| `MIGRATE_ON_START` | no | Defaults to true |

`Config.Validate` runs first and refuses to start on failures that would
otherwise be silent — a forgeable session key, or an `ENV` that is neither
`development` nor `production` and so gets treated as neither.

Migrations are applied during startup, before anything reads a table.
golang-migrate takes a Postgres advisory lock, so concurrent starts serialize
rather than race. A failed migration leaves the schema dirty and the server then
refuses to boot; `MIGRATE_ON_START=false` exists to get a server up so you can
go and look, and is not something to leave set.

**The app needs exactly one instance, and it must not scale to zero.** The
syncs run in-process:

- More than one instance runs every sync several times over, against APIs
  metered monthly.
- An instance that sleeps when idle stops syncing. Scores never arrive, bets
  never settle, and nothing reports an error — the jobs simply are not running.

`GET /health` returns 200 for platform probes. It deliberately does not check
the database: a liveness probe that fails on a brief blip turns a recoverable
hiccup into a restart loop. The image carries no `HEALTHCHECK`, because `PORT`
is assigned at runtime and a baked-in URL would be wrong.

Before going live, confirm database backups are enabled, and note that
registration at `POST /register` is **open to anyone who can reach the app** —
gate it first if that is not what you want.

## License

[MIT](LICENSE)
