# AGENTS.md

## Commands

- `make dev` — Docker Compose with Air hot reload
- `make test` — run all tests (`go test -v -race -cover ./...`)
- `make test-db` — the same, against a real PostgreSQL, which it drops and
  recreates first so a local run starts where CI starts. The repository tests
  skip without it; see Testing
- `go test -v -race ./internal/bets/...` — run tests for one package
- `make fmt` — format code
- `make fmt-check` / `make fix-check` / `make vet` / `make vulncheck` — the
  checks CI runs. `fix-check` is the one that is easy to forget: it fails on any
  pending `go fix` modernization, and CI runs `go mod tidy` and diffs go.mod too
- `make tools` — download tools pinned by the go.mod `tool` directive
- `make migrate-up` / `make migrate-down` — run/rollback migrations
- `make migrate-create name=<name>` — create new migration pair
- `make seed year=YYYY week=N seasonType=regular` — seed CFB data
- `make seedcbb season=YYYY` — seed CBB data
- `make seed-fixtures` / `make seedcbb-fixtures` — seed from the captured API
  responses in `internal/fixtures/testdata`. No API key, no network, no metered
  requests; this is what a fresh clone runs. See Fixtures
- `make capture` — re-record those fixtures. Spends metered requests, so
  nothing else in the repository calls it

**`config.Load` calls `godotenv.Load`, so `.env` wins over the process
environment.** `env -u CFB_DATA_API_KEY ./server` does *not* give you a keyless
server: the file is read after, the key comes back, every `RunOnStart` job fires,
and booting for ten seconds to check the process comes up costs a dozen or more
metered requests against a database with no teams to resolve them to. Move `.env`
aside, or point `DATABASE_URL` at a scratch database and accept the spend
knowingly. The fixture paths are keyless for a different and stronger reason —
`fixtureseed` constructs `NewClientAt(base, "")` and points it at a local socket,
so no environment can reach an upstream from there.
- `make vendor-htmx` — re-download the vendored htmx build and verify its checksum

## Architecture

Layered: **Handler → Service → Repository → Models**

- Feature packages live under `internal/` (auth, bets, leagues, games, basketball, admin)
- Shared data access in `internal/repository/`
- External API sync in `internal/cfbdata/` and `internal/cbbdata/`
- Entry points in `cmd/` (server, seed, seedcbb, seedtestdata, synccalendar,
  capture). `cmd/server` is split in two: `buildHandler` in `app.go` builds every
  service, route and middleware and returns the `http.Handler` the server serves;
  `main.go` keeps config, the database, migrations, the scheduler, the jobs and
  the signal loop. A test that wants the application without the process takes
  the first half — see Testing
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
- Unique indexes only on things that are keys. `teams.abbreviation` carried one
  until migration 000023 and it cost rows: the basketball feed's abbreviations
  are truncated to ten characters and 79 of them then collide, `TeamRepository.Upsert`
  arbitrates `ON CONFLICT (external_id, sport)` so it could not absorb a
  violation on the other index, and `syncTeams` logs a failed upsert and
  continues — 107 teams never written, and every game involving one skipped
  afterwards as "team not found". Nothing read a team by abbreviation

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
template or the CSS still takes effect without a rebuild. It is `main` that
chooses, not `buildHandler`, because `os.DirFS(".")` is relative to the
process's working directory — the repository root for the server and the package
directory under `go test`.

`currentYear` is the only template function that resolves "now" on its own, and
it reads `Renderer.SetClock` rather than the wall clock so a page rendered
against a replayed recording dates itself from that recording. Anything else a
template needs from the clock should arrive in its data.

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
require.

**A bet's status and the money it moves change in one transaction, or not at
all.** Settling, cancelling, editing and the admin correction all run through
`bets.Service.inTx`, and each changes the bet only through a conditional write:
`SettleIfPending`, `CancelIfPending`, `UpdateIfPending`, `TransitionStatus`.
The condition decides which of two concurrent callers moves the money; the
transaction makes a failed credit undo the move. Before both, a double-clicked
cancel refunded twice, a payout that failed after the bet was marked won was
never retried (the sweep reads only pending bets), and an edit saved the status
it had read over a cancel that landed in between.

`CreditWinnings` returns `ErrPurseNotFound` rather than updating nothing, which
is what turns a missing purse into a rollback instead of a bet paid to nobody.

Leaving a league removes the membership and keeps the purse, so a returning
member comes back to the balance they had — otherwise leaving would reset a
losing season. Every path that opens a purse goes through
`PurseRepository.CreateIfAbsent`; a plain `Create` on rejoin collides with the
old purse's primary key, which is how rejoining used to fail outright.

### Bets

A bet stores both the odds row it came from and a snapshot of the numbers at
that moment, so a later line move never changes what was agreed. Placing and
editing resolve that pair through the same `resolve*Odds` helpers in
`internal/bets/lines.go` — don't reimplement the custom-odds branch in a new
caller.

`editable()` mirrors `authorizeEdit()`. They must agree, or the page offers an
edit the service then refuses. Both gate on `game.ScheduledAt`, not
`game.Status`, because status only advances when the sync runs.

Bets are never saved whole. An edit goes through `UpdateIfPending`, which
writes an explicit column list: that keeps the status out of the write, and it
keeps out the odds row `FindByID` preloads — a whole-struct `Save` wrote that
row's ID back over the foreign key, so moving a bet to a different line silently
kept it pointing at the old one while the snapshot changed.

### Line History

The odds tables hold one row per game and book, overwritten by every lines
sync, so on their own they only know the current price. `odds_movements` is the
history behind them: a row each time a series — game, market, source — takes a
value it did not hold at the previous sync, and nothing when a sync finds the
line unchanged.

A row carries the **whole value**, not a delta. The row count is the same, but a
delta log has to be summed from its first entry to read any point in it and one
lost row corrupts everything after; here the line at an instant is the latest
row at or before it. Read the series as a step, not a slope — a line moves in
ticks, and a value between two rows is one no book offered. The move itself
happened somewhere between the previous sync and `recorded_at`, so the lines
cadence is the resolution. There is no `away_spread` column: a point spread is
one number seen from both sides, and both syncs store the away side as the
negation.

`OddsMovementRepository.Record` decides in one statement, and the sync offers it
every line it writes after a successful upsert:

- It compares against the **latest movement**, not the odds row. So the odds
  upsert keeps bumping `updated_at`, which the game page shows as when the line
  was last checked; an empty series counts as a change, which records every
  first value with no backfill; and a movement that fails to write is written by
  the next sync instead, because the history still disagrees with the feed — the
  two writes need no transaction. A failed history write leaves the run
  incomplete in its own tally ("odds history writes failed"), not the odds one:
  the lines themselves were stored.
- "Latest" is the latest **at or before the incoming instant**. A replay at an
  earlier time than a stored sync is compared with what came before it, but
  nothing re-checks the row after it, which may now repeat its value.
- Values are cast to the column types before comparing, so -110 and -110.00, or
  a basketball number through `NewFromFloat`, are one line.
- **A book's quotes are folded to one per run before anything is written**
  (`quotesBySource` in each sync). "ESPN" and "ESPN Bet" map to one source; written
  as two quotes, the later one won the odds row and the history's row for that
  instant, and the next run's first quote then read as a move away and back — a
  phantom row per series on every sync. The fold is market by market, later quote
  winning, which is what the odds rows held before. The unique index on
  (series, instant) and its `ON CONFLICT` are only a backstop.
- Two lines syncs running at once — a seed job overlapping the scheduled one —
  can both record the same move. Nothing serialises them, because the cost is a
  row that repeats the value before it: the line read at any instant is still
  right, and a lock per series would cost more than the row.

`recorded_at` is the sync service's clock, read once per run, never the
database's `now()`. A seed of a past week therefore records one row per series
dated when the seed ran — true as "first seen by us", not a real history.

What it cannot see: a book pulling a line. The sync never deletes an odds row,
so a withdrawn line reads as unchanged. CFBD's `spreadOpen`/`overUnderOpen` are
not recorded either — they carry no instant, and inventing one would be worse
than leaving them out.

### Game Results

A `GameResult` row is written whenever the provider reports a score, which need
not wait for the game to end, so the row's presence does **not** mean the game
is over.

`FinalizedAt` is the finality flag: nil while the score is provisional, set when
the provider calls the game complete. `EvaluateBetsForGame` refuses to settle
against a result where `IsFinal()` is false — otherwise a halftime lead pays out.
Anything new that reads a score has to decide which of the two it wants.

Live football scores come from `/scoreboard`, not `/games`. CFBD's `/games`
returns null points until `completed` flips true, so the nil-`FinalizedAt` path
never runs from there; `internal/cfbdata/scoreboard.go` is what fills it. The
basketball feed reports a real status and filters on `status=in_progress`, so it
may well carry live points — unverified, as it was checked out of season.

**Two feeds write a football game, and they disagree for minutes at a time.**
The scoreboard calls a game over within five minutes; `/games` keeps inferring
"in progress" until its own sync sees `completed`. Two rules keep that from
flapping, and both live in SQL so neither writer has to read before writing:

- `GameRepository.Upsert` never regresses a `final` status, and `completed` is
  OR'd rather than assigned
- `GameRepository.UpdateReportedStatus` is stricter still: `advancesFrom` lists
  what each status may replace, so a game only ever moves forward. `cancellable`
  reads the status as well as the kickoff — not instead of it, since PR 1 — so a
  status that could fall back to `scheduled` would reopen the refund window on a
  game whose stored kickoff is also wrong, which is exactly the delayed-game
  case
- `GameResultRepository.Upsert` keeps the first `finalized_at`, COALESCEs the
  line scores and excitement index so the feed that does not know a value cannot
  erase it, and refuses to let a *provisional* write overwrite the score of an
  already-finalized result — `EvaluateBetsForGame` re-reads that row, so
  guarding `finalized_at` without guarding what it certifies is half a rule

`scheduled_at` is the deliberate exception, and it is worth knowing why before
"fixing" it. `GameRepository.UpdateScheduledAt` lets the scoreboard correct a
moved kickoff, guarded on `status <> 'final'` — *not* `= 'scheduled'`, because
`/games` infers a status from the start time it last saw, so a game delayed an
hour is already stored as `in_progress` before the correction arrives and the
obvious guard would refuse exactly the case it exists for. Only a finished game
has no kickoff left to move. The `scheduled_at <> ?` inequality is what keeps it
from writing every game on every run.

Two absences are skipped rather than written, and they are different things.
`startTimeTBD` carries a *placeholder* instant — midnight of the expected day —
so the flag is the only way to tell it from a real kickoff. A missing or null
`startDate` is the zero time with that flag left false, so the flag does not
catch it; year 1 reads as a kickoff long past, which closes betting and leaves
every bet already placed neither editable nor cancellable until `/games`
rewrites the row.

**Neither may be inferred from, either.** `syncGames` derives a status from
`now > startDate + 5min`, and for every division outside
`CFB_SCOREBOARD_CLASSIFICATIONS` and every week outside the current one that
inference is the only status there is. Run against a `startTimeTBD` placeholder
it puts a game nobody has scheduled into `in_progress` for the rest of the day —
39 of week 6's 275 games in the committed capture — and `advancesFrom` has no
edge back, so the scoreboard cannot correct it. The zero `startDate` is the same
shape with a worse instant. Both now fall through to `scheduled`, which is what
the feed is actually saying.

What is *not* fixed is the kickoff itself: `scheduled_at` is NOT NULL and the
first insert has nothing better than the placeholder, so the betting cutoff —
which reads the kickoff, not the status — still closes on a TBD game at midnight
of the day it is played. Closing that needs a column recording that the instant
is a placeholder, and a decision about whether such a game takes bets at all.
`TestGamesStatusIsInferredFromTheClock` asserts the placeholder is still stored,
so closing it has to come through there.

`Upsert` still assigns `scheduled_at` unconditionally, so the two writers are
asymmetric: a `/games` run holding a kickoff CFBD has not corrected yet writes
the stale time back, and the scoreboard re-corrects it within five minutes. The
value flaps for one interval and converges on the fresher feed. Guarding
`scheduled_at` in `Upsert` to tidy that up would freeze the kickoffs of every
division the scoreboard does not poll, where `/games` is the only writer there
is.

This half-fixes the wart on `advancesFrom`: the delayed game's kickoff is
corrected, so the betting cutoff and the scoreboard's own cadence start
behaving, but the *status* stays wrong until the game really is under way —
there is no edge back from `in_progress` to `scheduled`, and adding one is the
thing `advancesFrom` exists to prevent.

**Finalizing a game and paying out on it are separate acts.** Any feed may
finalize: the scoreboard has always written `finalized_at` when it sees a game
complete, and since `GameResultRepository.Upsert` keeps the *first* one, at five
minutes against `/games`'s fifteen it is usually the feed that does. What it
could not do was act on it — `EvaluateBetsForGame` was reached only from the
middle of `syncGames`, so a fully finalized game sat with its bets pending until
that endpoint was next polled and happened to agree.

The `bet-settlement` job closes that gap. It sweeps every five minutes for games
with a finalized result and a bet still pending (`SettlementRepository`), and
calls `EvaluateBetsForGame` on each. It reads only the database, so it is
registered in `main` outside `registerSyncJobs` — a pending payout is owed
whether or not an API key is configured. It also makes settlement *retriable*,
which it was not: a game whose payout failed halfway used to depend on another
sync run passing the same way.

Two things follow. `minTimeToPlay` (90 minutes past kickoff) is a floor on how
soon a "final" is believed, because acting on the scoreboard within five minutes
means acting on a mislabelled one within five minutes too, and no game is played
out that fast. And settlement now has more than one caller, so moving a bet off
pending goes through `SettleIfPending` — a conditional update whose
`RowsAffected` decides who credits the purse. Two callers reading the same bet as
pending would otherwise both pay it.

The scoreboard's odds are still ignored: they name no sportsbook, and the odds
tables are keyed by one.

`Game.Status` is reported for any football game the scoreboard covers, and
*inferred* (`now > startDate + 5min`) for the rest — which is every division
outside `CFB_SCOREBOARD_CLASSIFICATIONS` and every game outside the current
week. Basketball takes its status from the API.

The line score is the exception: it stays on `GameResult`, where the score
lives, and `PeriodScores(sport)` zips the two sides into columns for the game
page. It takes the sport because that page serves basketball too, and basketball
plays two halves — given football's regulation it files real half scores under
`Q1` and `Q2` and pads two empty quarters after them.
The column labels come from the same `periodLabel` the live badge uses, so an
"OT" column and an "OT" badge on the same page cannot drift apart. The four
quarters are always present, carrying nil — not zero — for a period the feed
has said nothing about, so the table has the shape it will end with from the
first snap rather than growing a column each quarter. Overtime extends it one
column at a time.

Everything the scoreboard reports beyond the score — period, clock, situation,
possession, last play, TV, weather, win probability — lands in
`models.GameLiveState`, one row per game, preloaded as `Game.LiveState`.
`possession` is normalised to `home`, `away` or nil on the way in: those are the
only values the feed sends and the only ones anything reads, and the column is
bounded where the upstream string is not — one long enough to overflow it fails
the whole row's upsert and costs the clock and the situation with it. Its
display helpers are all nil-safe receivers, because the templates call them on
games the scoreboard has never covered. A row's presence means "the scoreboard
has seen this game", never "this game is live"; the clock is left in place once
a game ends, so the live strip gates on `Game.Status`, not on the row.

### Fixtures

`internal/fixtures/testdata` holds recorded CFBD and CBBD responses, and
`internal/fixtureserver` replays them over HTTP. `cfbdata.NewClientAt` and
`cbbdata.NewClientAt` are the seam — a constructor rather than a
`CFB_DATA_BASE_URL`, because an environment variable would put production one
typo away from syncing a season out of a fixture directory while logging
success.

The layout is path → directories, query → leaf directory, capture instant →
file name: `cfbd/games/week=1&year=2026/20260920T124751Z.json`. The query is
sorted before it is slugged, and `_` names an empty query. Failure suffixes:
`.502.json` serves that status, `.bad` serves non-JSON under a JSON content
type.

- **`//go:embed all:testdata`, never `//go:embed testdata`.** Without `all:`,
  embed skips every path beginning with `_` — which is the empty-query
  directory, so `/venues` and `/teams` vanish from the binary while sitting on
  disk
- **The fake upstream answers an unrouted path with a 500, never `[]`.** Every
  endpoint here returns a JSON array, so the empty one is the tempting answer
  and the worst available: a sync over it writes nothing and reports success
- **A route serves a sequence.** The nth request gets the nth capture, and past
  the end it repeats the last
- **Never filter a capture by division, conference or team.** Narrow by week.
  The spread across `fbs`/`fcs`/`ii`/`iii` is what several past bugs needed to
  be visible, and `/venues` and `/teams` are unfiltered because `syncGames`
  skips a game whose team is missing, logs a warning and returns nil
- **A fixture seed checks two things**, because neither covers the other: that
  the fake upstream refused nothing (`Server.Refusals()`, which holds whatever
  was already in the database), and that the tables are not empty (which
  catches a capture that is present but hollow, and can only fail against a
  fresh database)
- **Some captures are worth more stale than fresh.** Week 6 of 2026 was
  recorded before it was played, and that is its entire value — it is the only
  capture in which the clock decides anything, and three level-2 tests rest on
  it. Re-recording it returns a played week and silently removes their premise,
  so it is not in `scripts/capture.sh`'s default set. `week 6 is still an
  unplayed week` in the level-1 test is what notices; if it fails after a
  capture run, `git checkout` that directory rather than editing the test.
  Recording a *new* unplayed week is the way to replace it
- **`make capture` replaces a route's capture; it refuses to replace a
  sequence.** The server replays oldest-first, so appending a second capture of
  a single-shot endpoint leaves the seed reading the stale one forever. A route
  with several captures is a series on purpose — `-append` extends it,
  `-replace` discards it, and the refusal happens before any metered request

`internal/fixtureseed` is the shared path. `cmd/seed -fixtures` runs it against
a connection; a test runs it against a `testdb` transaction to get rows the
feed really sent — see Testing. `fixtureseed.At(instant)` replays a seed as
though it were running then, which a test asserting on a status or a
`finalized_at` needs and `seed -fixtures` does not.

`fixtureseed.Football` runs the whole `SeedAll`, so it only works for a week with
the whole set captured — weeks 1 and 2. `fixtureseed.FootballGames` is one week of
games alone, on top of reference data a prior `Football` call wrote, which is what
week 6 needs. It refuses a zero for the same reason the others do.

**Which week a fixture can answer for depends on when it was captured.** Weeks 1
and 2 were recorded on 2026-09-20, after they had been played: `/games` reports
every game `completed` with a real score, so they infer `final` whatever the
clock says and can say nothing about an unplayed game. Week 6 was recorded the
same day and had not been played — 275 games, none completed, no points, real
future kickoffs, 39 of them `startTimeTBD`. It is the only capture in which the
clock decides anything, and `/games` is the only endpoint captured for it.

### Testing

- Table-driven tests with `t.Run()` subtests
- Use `testing.T`, `httptest.NewRequest`, `httptest.NewRecorder` — no mocking framework
- Decimal test values: `decimal.RequireFromString("150")`

Repository tests run against a real PostgreSQL, via `internal/testdb`. The
queries *are* the thing under test there, and their failure mode is quiet — a
join that drops rows or a predicate that matches nothing returns an empty result
and no error, which is indistinguishable from having nothing to match. A mock
cannot reach that and an in-memory engine answers a different dialect.

`testdb.Open(t)` hands back a `*gorm.DB` inside a transaction that is rolled
back when the test ends, so fixtures cannot outlive a run — which matters
because a developer's database holds real seeded seasons. `InsertTeam`,
`InsertVenue` and `InsertGame` fill in everything a test did not set.

**What that harness cannot see is concurrency.** One transaction is one
connection, so two requests can never overlap in it, and a nested
`db.Transaction` is a savepoint on that same connection. Two consequences: a
race between two callers can only be tested at the conditional write that
decides it (`SettleIfPending` refusing the second caller), never end to end;
and a repository built from the service's `*gorm.DB` instead of the `tx` inside
`inTx` passes every test here while committing outside the transaction in
production, where that `*gorm.DB` is a pool. Proving either needs committed rows
and two connections, which this package does not provide.

There are two ways to fill that transaction, and the choice is by subject.
A test about **logic** — a predicate refusing an edge, a stake moving a
difference — uses the `Insert*` helpers, where the inputs are visible and
minimal. A test about **the shape of real data** — a query over a mixed
division slate, a parser meeting a field it has only been described — calls
`fixtureseed.Football(ctx, db, year, week)` and gets rows CFBD actually sent.
The `Insert*` defaults are somebody's belief about the feed; `InsertTeam`
hard-codes `"fbs"` in lower case and only a comment said that was right.

Seeded tests stay few and shared: a week is thousands of rows written and
rolled back, about 1.3s — nearer 8s under `-race`. `make test` is unaffected at
3–4s because these skip without a database.

**Do not benchmark this suite on a laptop.** Wall clock for `make test-db`
measured between 62s and 120s across runs of *identical* code, with the database
recreated before each one — and the sign of a main-versus-branch difference
flipped between adjacent pairs. Per-package times are worse: `cmd/server`
measured 7s, 19s, 34s, 55s and 72s across five runs of one commit.

**The variance is thermal.** Sampled during a run, package temperature reaches
100°C — Tj,max — four times over seventy seconds, and peak clock sags from
4278 MHz to 3032 MHz, recovering to 4700 MHz within seconds of the test binaries
exiting. Sustained load heat-soaks the chassis, so each run in a series starts
hotter and throttles sooner than the last, which produces a clean ascending
sequence that reads exactly like a regression somebody just introduced. A pause
resets it.

Two different things, worth keeping apart. *Contention* sets the level: load
average is only ~2.5 on twenty cores, because the suite is bound on the one
PostgreSQL container that twenty test binaries hammer in parallel rather than on
cores. *Throttling* sets the trend between runs. Neither is a property of the
code.

To find out whether a change costs time, read CI — its database is a fresh
service container and its machine is doing nothing else. If you must compare
locally, let the machine idle between runs and still expect ±30%.

**There is no leak, and this was checked rather than assumed.** The suite's cost
is additive in the number of seeded tests, not quadratic — one seeded test does
not make the next one slower. Two measurements say so:

- Seeding the same week ten times in one process: 1.35, 0.84, 1.03, 0.91, 0.91,
  1.03, 0.85, 1.07, 1.20, 1.25s. Flat.
- The scan-heavy level-3 grid test run five times while dead tuples climbed from
  58k to 66k: 7.07, 7.45, 7.83, 7.32, 7.35s. Also flat.

Inserts do not care about dead tuples, and the seeded tables are small enough
(412 games, 674 teams) that even 66k dead rows is a few MB living in shared
buffers. Autovacuum also keeps up *during* a run, not only after it.

So the way this suite becomes an hour long is by someone adding an hour of
seeds, one deliberate test at a time — not by drift. The numbers to plan with,
under `-race`: a football week seed ~8s, a basketball season seed ~100s, a page
render ~0.1-0.2s.

**Cost the change against CI's core count, not a laptop's.** `go test` defaults
`-p` to GOMAXPROCS, so a 20-core machine runs twenty test binaries at once and
wall clock is roughly the slowest package — which makes a new seeded test look
free. GitHub's free-tier `ubuntu-latest` has **2 vCPUs**, so it runs two, and
wall clock is roughly the *sum* halved. A seeded test that costs nothing locally
costs half its own duration on every CI run, forever.

The metric that survives the difference is **summed package time**, which is
parallelism-independent: `go test -race ./... | awk '/^ok/{...}'`. It is 245s on
this tree against 196s before levels 3 and 4. Reproduce CI's shape locally with
`go test -p 2`, which turns a 78s run into a 127s one.

Of that 245s, the basketball season seed is ~100s — a single test, 40% of the
suite. If CI time ever becomes the binding constraint, that is the first lever
and a separate job is the way to pull it, since the test is also the only thing
exercising `seedcbb -fixtures` end to end.

`synchronous_commit = off` on the test database was tried and does nothing here,
which is worth knowing before someone tries it again: `testdb` wraps a whole
test in one transaction and rolls it back, so there is almost nothing to commit.

What is structurally true and needs no measuring: the basketball season seed is
the longest pole by a wide margin and everything else finishes inside its
shadow. So the next seeded test is close to free, and the one that finally
displaces basketball is not — at which point the lever is to run the seeded
tests as their own CI job, not to make them prove less. That test is also the
only thing exercising `seedcbb -fixtures` end to end.

One thing that is *not* the explanation, having been chased: dead tuples. Every
test rolls its transaction back, but a rolled-back transaction has already
written its heap tuples — rollback only marks them invisible — so immediately
after a run every seeded table is 100% garbage (`games` at 0 live rows, 4,784
dead, 5.9 MB). Autovacuum reclaims it within minutes, so it does not survive
between runs and it is not what makes one run slower than the last.

Seeded tests also couple to the fixture set, so a recapture moves a test that
turns on "the third FCS game of week 2".

Run them with `make test-db`, which starts the compose database and **drops and
recreates** `betting_tracker_test` beside the development one. Recreating rather
than creating-if-absent is so a local run starts where CI starts: CI gets a
fresh service container every job, and a laptop got whatever the last twenty
runs left behind. The target refuses a `TEST_DB_NAME` that does not end in
`_test`, because the development database is on the same server one word away. Without `TEST_DATABASE_URL`
they **skip** locally so a plain `make test` still passes with nothing running,
and **fail** when `CI` is set, so renaming the variable out from under them
cannot turn the suite green by accident.

The database must be **empty**, and `Open` fails with a sentence saying so if
it is not. These queries are aggregates over whole tables, and a transaction
still reads the committed rows around it, so pointing them at a development
database would have them answering from real seeded seasons — passing or
failing on rows no test wrote. That is checked rather than conventional on
purpose: the alternative, dating every fixture past the real data, holds only
until someone writes a test without knowing about it.

Compare timestamps with `Equal`, not `==`: the column keeps microseconds and
the driver returns `Local`, so neither the monotonic reading nor the location
survives the round trip.

#### The clock

`cfbdata.SyncService`, `cbbdata.SyncService`, `bets.Service`, `games.Service` and
`basketball.Service` each take an overridable time source:

```go
svc.SetClock(timeutil.Fixed(time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)))
```

The zero `timeutil.Clock` is `time.Now`, so nothing that does not care has to
say so. A test replaying a recording does care: `syncGames` infers a status from
`now > startDate + 5min` and the whole scoreboard cadence is a function of now,
so a fixture captured on a live Saturday replayed against a real clock has every
game kicking off in the past, every status inferring the same way, and the
two-feed disagreement the write rules exist for never happening. The instants to
choose from are the fixture file names.

Not `testing/synctest`, which `internal/scheduler` uses and which does not
generalise here: a bubble's clock starts at 2000-01-01, decades before any
captured instant, and a bubble waits for `net/http.Transport`'s read loop, which
never ends. `internal/timeutil/clock.go` says this at length.

`templates.Renderer` has one for the footer's copyright year, which is the one
thing a template resolves from "now" by itself. `cmd/server`'s
`application.SetClock` fans one instant out to all five — four services and the
renderer — and a page test should use it rather than setting them one at a time,
because the failure it prevents is a page answering one question from the
fixture and the next from today.

`admin.Service` has one too, for the two facts the sync page reports about "now":
the scoreboard state and the upper bound on a manual seed's season. It is not
about production drift — the scheduler passes its own instant into `NextDelay`, so
both sides of "the same function, not a second opinion" read the wall clock and
agree. It is that `Health()` was the one part of that page nothing could pin,
while the current week beside it already came through `games.Service`, so a page
rendered at a fixture instant answered one question from the fixture and the other
from today. Give it the **same instant** as the sync services in a test.

Two things keep their own `time.Now`: the repositories (it sets `updated_at`,
which nothing asserts on) and `cbbdata.GetCurrentSeason` (a `cmd/` flag default
resolved before any service exists — `SeasonFor(now)` is its testable half).

A display helper that needs "now" takes it as an argument instead.
`games.ZoneAbbreviation(loc, at)` is the one there is, and it takes the instant
because a zone's abbreviation is a function of the date: `America/New_York` is
EDT in September and EST in January. Reading the wall clock there produced the
worst failure mode available to a test — a page test written in October passing
until March.

A service method that writes a timestamp reads its own clock rather than taking
one. `bets.FinalizeGameResult` took an `at` argument and its single caller always
passed `time.Now()`, so the parameter was not flexibility — it was a second clock
to forget to set.

#### The level ladder

| Level | Boundary | Where |
| --- | --- | --- |
| 1 | Fixture → client | `internal/{cfbdata,cbbdata}/client_fixtures_test.go` |
| 2 | Fixture → database | `internal/cfbdata/{games_status,convergence,classification,sync_lines}_fixtures_test.go`, `internal/cbbdata/sync_fixtures_test.go` |
| 3 | Database → page | `internal/games/handler_fixtures_test.go`, `internal/bets/edit_fixtures_test.go` |
| 4 | Fixture → page | `cmd/server/app_fixtures_test.go` |

`cmd/server/app_test.go` is level 4 without a fixture: sessions and the admin
portal through the real router, where the wiring is the thing under test and no
feed data is needed.

A test file is named for the source file it exercises, never for its level:
`<file>_test.go`, `<file>_<topic>_test.go` for a slice of a large one, and
`<file>_fixtures_test.go` when it runs against the captured feed. Helpers
shared across a package's tests go in `<subject>_harness_test.go`. The level is
a property of what a test crosses, which the file's opening comment says; the
name is what someone looking at `edit.go` searches for.

Level 2 is where the write rules above get tested, and they need the recorded
disagreement: `/games` week 1 and the first live scoreboard snapshot contradict
each other about 57 of the 99 games they share, which is not a disagreement
anyone builds by hand, because whoever builds it already knows which feed is
right.

A level-2 test lives in the **external** test package (`package cfbdata_test`):
`fixtureseed` imports `cfbdata`, so an internal test file importing it is an
import cycle.

Two traps in these tests specifically. Subtests share one transaction, so the
first failed statement poisons every later one and a single bad query reads as
several failures — look at the first.

And a rule that passes is not a rule that is tested. Every guard these tests
claim to cover was checked by **mutating the guard and watching the test fail**:
widening `advancesFrom`, replacing the score and `finalized_at` guards with plain
assignment, reverting the `startTimeTBD` case, and un-folding
`normalizeClassifications` all fail. One does not — replacing
`games.completed OR excluded.completed` with a plain assignment passes, because
only `/games` calls `Upsert` and the captured week reports every game
`completed`, so there is nothing for the OR to protect. That check is kept and
labelled in the test rather than deleted. Do this to any new write-rule test
before believing it.

#### Levels 3 and 4

`internal/pagetest` is the shared half: a `testdb` transaction, a renderer over
the embedded templates, a user with a league and a purse, and one instant every
clock in the process agrees on. `pagetest.Open(t, at)` and then `SeedFootball`
or `SeedFootballGames`. It stops where the two levels diverge.

**Level 3 calls a handler directly.** `Env.GET`/`Env.POST` put the user on the
request context the way `auth.OptionalAuth` would have, so no session is
involved. A route's path parameters are not parsed by anything at this level —
`req.SetPathValue("week", ...)` is the caller's job, and forgetting one reads as
a 400 rather than as a missing step.

**Level 4 goes through `cmd/server`'s own router.** `buildHandler` returns the
handler `main` serves, so a level-4 test drives the real stack rather than a mux
it assembled itself — which matters because the wiring is where the interesting
mistakes are, and *a route is only guarded if it was registered through the
guard*. The session is minted by posting the real `/register` form and keeping
the cookie; there is no other way in, and `auth` marks the cookie `Secure` in
production, which Go's cookie jar then refuses to send over `httptest`'s plain
HTTP. `pagetest` builds a development config for exactly that reason.

Both levels want `pagetest.Assets`, never `os.DirFS(".")`: the working directory
under `go test` is the package directory, so the on-disk template tree is not
where the server would find it.

Two things to know before adding one.

**Subtests share the transaction, so a subtest that seeds changes what the later
ones count.** Scope a count to the week under test rather than to the table, or
adding a second week moves an unrelated expectation.

**Read a set once, not a row at a time.** A page holds a hundred cards and a
week holds four hundred; checking each one with its own query is correct and
takes twenty seconds. `gamesInTier` and `gamesWithALine` are the shape to copy.

**Verify with single-table reads, not joins, when the set is large.** The
planner's statistics are whatever the last rolled-back run left: once autovacuum
has cleaned up after one, every table reads as "no tuples in N pages", the
planner estimates one row for each, and a join picks a nested loop that rescans
the inner table per outer row. Measured on the basketball season, a LATERAL
check of spreads against their history took 24 ms on a freshly created database
and 10.4 s after a previous run — the same query, the same rows. `make test-db`
recreates the database, so a full run starts fast and a package run straight
after another does not. `testdb.OddsHistory` reads each table on its own and
pairs the rows in Go, which has no join order to get wrong.

A check that compares two things built by the same function is not a check. The
line-history invariant maps odds rows to movements column by column in `testdb`
rather than through `models.SpreadMovement` and its siblings, because those are
what the sync records with — mapping both sides through them let a constructor
reading the wrong column agree with itself.

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

Job cadence is a spending plan, not a freshness knob. CFBD meters us at 30,000
requests a month and the football jobs are most of it:

- `cfb-scoreboard` polls every 5 minutes while a game is being played and
  hourly the rest of the time, one request per division per run
  (`cfbdata.ScoreboardDelay`). What decides which is
  `cfbdata.ResolveScoreboardState`, which reads the games table — is anything
  live, and when is the next kickoff — rather than asking whether the calendar
  says it is a season. Its worst failure is silent: anything that stops the
  state resolving reads as live, which is permanent 5-minute polling on a job
  that still looks healthy. So it logs the resolved state at debug, and the
  admin sync page shows the same two facts through the same function — not a
  second opinion that can drift from what the scheduler acted on. It is scoped to
  the divisions in `CFB_SCOREBOARD_CLASSIFICATIONS`, not to football as a whole:
  `/games` and `/teams` are fetched unfiltered, so the table holds every
  division CFBD returns, and their status is inferred from the clock, which
  reads `in_progress` for hours at a time. Each extra division is another
  ~2,700 requests a month in the heart of the season, ~720 out of it
- `cfb-lines` follows the football week — 15 minutes Thu–Sat, 30 midweek,
  hourly overnight (`cfbdata.SyncDelay`) — at one request a run, ~1,870 a month
- `cfb-games` runs four times a day on the wall clock (`cfbdata.GamesDelay`),
  ~120 a month. It shared the lines cadence until the two were split, which
  paid a 15-minute game-day rate for a feed that changes weekly. What made the
  split safe is that nothing time-critical comes off `/games` for the divisions
  the scoreboard covers. Outside them it is the only feed there is, and two
  things get worse by the same six hours: a bet can sit unsettled that long
  after its game ends, and `scheduled_at` can be that stale where it used to be
  at most an hour — which is a betting cutoff and a refund window, not just a
  badge, since a kickoff moved earlier leaves a gap in which a bet can be placed
  on or voided off a game already under way. `UpdateScheduledAt` cannot close it
  there; only the scoreboard calls it. The remedy is
  `CFB_SCOREBOARD_CLASSIFICATIONS` rather than a faster rate. It carries
  `RunOnStart` but no catch-up for a slot missed while the process was down —
  the obvious catch-up keys on the last *success*, which never advances while a
  run keeps failing, so a `/games` endpoint returning 502s would be retried
  every minute forever. `RunOnStart` is the cheap half of the same idea, and it
  covers the case the six-hour grid cannot: `NextDelay` is recomputed on every
  start, so a process restarting faster than its interval never reaches the
  timer, and this job would never run at all
- `cbb-games` and `cbb-lines` share one cadence (`cbbdata.NextGamesSync`):
  `CBB_SYNC_INTERVAL_MINS` from October 25 to April 15, once a day outside it,
  ~5,800 a month between them in season and ~60 out. They are two jobs so a
  failing `/games` no longer skips `/lines` and a line snapshot with it — not
  to slow `/games` the way football's was. Basketball has no scoreboard, so
  `/games` is its only source of scores, statuses and settlement. The lines
  run `linesLag` (five minutes) behind the games: `syncLines` skips a game it
  cannot find, so racing the games run in one slot drops a new game's lines
  until the next — a day, out of season. `RunOnStart` is set only when the
  process starts out of season, where the slot is a day long; in season it
  would be two metered requests on every Air reload for nothing. The interval
  is a grid step, so when a basketball key is set `Config.Validate` refuses
  one that does not divide a day or lies outside 10–1440: zero divides by it,
  100 runs at irregular spacing across midnight, and under ten the pair
  outspend their share

The allowance is split in `internal/apibudget` — a share for each sport and a
reserve for the jobs no test counts — and each sport's cadence test holds its
worst month to its own share. `apibudget`'s own test checks the shares still fit
the allowance, so neither sport can pass by raising its number.
`TestBasketballCadenceStaysWithinMonthlyCallBudget` counts at the ten-minute
floor rather than the default, and holds an off-season month to a few hundred —
which is what notices the throttle going missing, since a July at the in-season
rate still fits under the share.

`TestFootballCadenceStaysWithinMonthlyCallBudget` walks real months at all
three schedules and fails if a change to any of them overruns the plan, capped
at 10,000 against a measured worst month of ~7,770. It drives the scoreboard
from a synthetic slate, because a cadence derived from the games table cannot be
costed against a feed assumed live around the clock. That slate is calibrated
against the real schedule — ~34 live hours a week against ~36 measured — and a
slate that is too thin is the one way this test passes while production
overspends.

What that cap does *not* catch is `/games` going back onto the lines cadence:
~1,740 calls a month hides under a cap sized for the scoreboard, which dominates
the sum. `TestNextGamesSyncRunsFourTimesADay` is what pins that rate.

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

One down migration is *designed* to fail. 000023 dropped a unique index that the
data had been silently conforming to by losing rows, so restoring it errors on any
database that has since stored a colliding pair — which is every database worth
having run it on. The file says so and gives the `force 23` recovery. Rolling back
past it means first deciding which of each colliding pair of teams to delete, and
a migration is the wrong place to decide that.

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
- Saving a whole bet row — a preloaded association overwrites the foreign key and a stale status overwrites a cancel or a settlement; write named columns, conditionally, as `UpdateIfPending` does
- Moving a bet's status and a purse outside `inTx` — a credit that fails after the status commits leaves the bet settled and unpaid, and nothing retries it
- Treating a `GameResult` as final — check `IsFinal()`, or bets settle on a live score
- Crediting a purse for a settled bet without first winning `SettleIfPending` — the settlement sweep is not the only caller, and a bet read as pending twice is paid twice. A cancel's refund is gated the same way, on `CancelIfPending`
- Assigning `status` or `finalized_at` unconditionally in a football upsert — two feeds write those rows and a plain assignment lets the slower one un-finish a settled game
- Guarding `scheduled_at` in `GameRepository.Upsert` the way `status` is guarded — `/games` is the only feed for every division the scoreboard does not poll, and the guard would freeze their kickoffs permanently
- Guarding a kickoff correction on `status = 'scheduled'` — `/games` infers `in_progress` from the old start time, so that is precisely the row that needs correcting
- Writing a feed's start time without checking it is non-zero — an omitted or null `startDate` unmarshals to year 1, which reads as a kickoff long past and freezes every bet on the game
- Treating a `GameLiveState` row as "this game is live" — it exists from before kickoff and keeps the last clock after the whistle; gate on `Game.Status`
- Trusting stored week dates unchecked — filter on `models.Week.Plausible()` in *every* path that asks "which season/week is it now"
- Calling `j.Run` directly, or starting any goroutine whose panic nothing recovers — one takes down the whole process
- Keying a job's missed-slot catch-up on its last *success* — a run that keeps failing never advances it, so an endpoint returning 502s is retried every minute forever; key it on the last attempt, if at all
- `.Format`-style mtime cache busting for assets — every embedded file reports the zero mtime; hash the contents
- Unbounded database pools — `database.Connect` sets the limits, and `DB_MAX_OPEN_CONNS` has to stay under the server's own cap
- Registering an admin route on the parent mux — `admin.RegisterRoutes` builds its own mux and mounts it on `GET`/`POST` `/admin` and `/admin/` behind `RequireAuth` and `RequireAdmin`, so a route is guarded by being on that mux. Mount points need their methods: a method-less `/admin/` conflicts with `GET /` and `ServeMux` panics
- `//go:embed testdata` without the `all:` prefix for fixtures — it silently drops the `_` directory, which is where the unfiltered endpoints live
- Answering an unrouted fixture path with `[]` — it is indistinguishable from a successful sync with nothing to write
- Appending a fresh capture beside an old one on a single-shot endpoint — the server replays oldest-first, so the new one is never reached
- Filtering a capture by division — the spread is the property that makes the fixture worth having
- A base-URL environment variable — `NewClientAt` is the seam, and it is reachable only from code that means to call it
- Re-capturing an *unplayed* week — its value is that the games had not happened, and a refresh destroys it silently
- Inferring a status from a `startTimeTBD` placeholder or a zero `startDate` — both are instants the feed does not mean, and `advancesFrom` has no edge back from the `in_progress` they produce
- A unique index on a display string — the one on `teams.abbreviation` cost 107 basketball teams and the 49 games that needed them, because `Upsert` arbitrates a different index and the violation was logged and continued past
- Slowing basketball's `/games` to football's schedule rate — there is no basketball scoreboard behind it, so it is the score feed and the settlement trigger
- Raising a share in `internal/apibudget` to make a cadence test pass — the shares are the plan; overrunning one is the finding
- Deltas in `odds_movements` — store the whole line at each change; a delta log turns one lost row into every later value being wrong
- Writing a book's quotes one by one in a lines sync — fold them with `quotesBySource` first, or a book under two spellings records a phantom move every run
- Deciding whether a line moved by comparing against the odds row — the upsert has already overwritten it, and comparing against the latest movement is what lets a failed history write heal on the next sync
- Dating a movement with the database's `now()` — use the sync's clock, or a replayed fixture records today instead of its capture instant
- Replaying a fixture against the real clock — use `SetClock` or `fixtureseed.At`, or every recorded game reads as long finished
- A level-2 test in the internal test package — `fixtureseed` imports the sync packages, so it has to be `package cfbdata_test`
- Trusting a green write-rule test — mutate the rule and confirm it fails, or it is asserting on data that never contested anything
- Building a router in a test instead of calling `buildHandler` — the wiring is the thing a level-4 test exists to check, and a hand-assembled mux agrees with whatever the test already believes
- Reading the wall clock in a display helper — `ZoneAbbreviation` takes the instant, because a page test written in October passes until March otherwise
- `os.DirFS(".")` in a test — the working directory under `go test` is the package directory; use `pagetest.Assets`
- A production config in a page test — `auth` marks the session cookie `Secure`, and Go's cookie jar then refuses to send it over `httptest`'s plain HTTP, so registration succeeds and every later request reads as logged out
- Checking a page of a hundred cards one query per card — read the set once; the row-at-a-time version is correct and twenty seconds slower
- Counting a whole table in a test whose subtests seed — they share the transaction, so a later subtest's week silently moves an earlier subtest's expectation
