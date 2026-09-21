#!/usr/bin/env bash
#
# Record live API responses into internal/fixtures/testdata.
#
# This is the only thing in the repository that spends metered requests outside
# the running server. CFBD allows 30,000 a month and the background jobs are
# most of it, so capture deliberately and in batches -- one run of the default
# set below is nine requests.
#
# The work is done by ./cmd/capture rather than by curl and jq here, because
# the directory a capture lands in has to be the directory the fixture server
# looks in, and a slug implemented twice drifts. This script exists for the one
# thing shell is better at: finding the key in .env without putting it in the
# process list or the shell history.
#
# Usage:
#   scripts/capture.sh                      # the default football set
#   scripts/capture.sh --cbbd               # the default basketball set
#   scripts/capture.sh /venues /teams       # specific cfbd paths
#   scripts/capture.sh -provider cbbd '/games?season=2026'
#
# Re-capturing a route REPLACES what is there. That is the default because the
# server replays a route oldest-first: a second capture of /venues appended
# beside the first would leave the seed reading the stale one forever and never
# reaching the fresh one -- a re-capture that appears to do nothing.
#
# A route holding several captures is a sequence on purpose (the scoreboard
# series is twenty-six), so replacing one is refused before any request is
# spent. Pass -append to extend it or -replace to discard it.
#
# This replaces scripts/capture-scoreboard.sh, whose question -- what the feed
# looks like between games -- was answered, and whose answer is now the
# scoreboard fixture series. Its summary.tsv columns were specific to that
# investigation and are not carried forward.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The keys live in .env, which is gitignored. Sourcing it rather than taking an
# argument keeps them out of the process list and out of shell history.
if [[ -f "$root/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    . "$root/.env"
    set +a
fi

# The default set is what `seed -fixtures` needs to write a populated database:
# SeedAll's six endpoints, in its own order -- venues, teams, calendar, games,
# rankings, lines.
#
# Two weeks rather than one. The scoreboard series already committed here
# spans the boundary: twenty-one captures of the live Saturday 2026-09-05,
# which the calendar puts in week 1, and five quiet snapshots from 2026-09-12,
# which is week 2. Capturing only week 1 would leave those last five describing
# games no fixture ever wrote.
#
# Two rules about what is captured, both learned the hard way:
#
#   Narrow by week, never by division. The spread across fbs/fcs/ii/iii is the
#   property that found the last several bugs -- a classification compared in
#   the wrong case, a scoreboard scoped to a division whose teams were missing.
#   Trimming by week keeps that spread and still bounds the file.
#
#   /venues and /teams are unfiltered on purpose. SyncGames skips any game
#   whose home or away team it cannot find, logs a warning and returns nil, so
#   a fixture set missing a division seeds fewer games and still exits 0.
#
# No seasonType, because `seed` without -seasonType sends none, and a query
# present in one and absent in the other resolves to two different directories.
# A THIRD RULE, learned from the capture that is deliberately not below.
#
#   Some captures are worth more stale than fresh, and week 6 of 2026 is one.
#   It was recorded on 2026-09-20, before it was played: nothing completed, no
#   points, real future kickoffs, and 39 games the feed had not scheduled. That
#   is the only capture in which the clock decides anything, and three level-2
#   tests rest on it. Re-recording it after 2026-10-11 returns a played week and
#   silently removes their premise, so it is not in the default set and should
#   not be added to one.
#
#   TestClientDecodesEveryCapturedEndpoint/week_6_is_still_an_unplayed_week is
#   what notices. If it fails after a capture run, the fix is `git checkout` on
#   internal/fixtures/testdata/cfbd/games/week=6*, not an edit to the test.
#
#   Recording a *new* unplayed week is fine and cheap -- one request for a week
#   that has not happened yet -- and is how to replace week 6 if it ever has to
#   go.
YEAR="${YEAR:-2026}"
default_cfbd=(
    /venues
    /teams
    "/calendar?year=$YEAR"
)
for week in 1 2; do
    default_cfbd+=(
        "/games?year=$YEAR&week=$week"
        "/rankings?year=$YEAR&week=$week"
        "/lines?year=$YEAR&week=$week"
    )
done

# Basketball's SeedAll chunks the season by month to stay under the API's 3000
# result cap, so its fixture set is venues, teams, and six games+lines pairs.
# Season 2026 means November 2025 through April 2026. The date format and the
# month boundaries here have to match cbbdata.SyncService.SeedAll exactly; the
# fixture server's 500 is what says so when they drift.
SEASON="${SEASON:-2026}"
default_cbbd=(
    /venues
    /teams
)
prev=$((SEASON - 1))
starts=("$prev-11-01" "$prev-12-01" "$SEASON-01-01" "$SEASON-02-01" "$SEASON-03-01" "$SEASON-04-01")
ends=("$prev-12-01" "$SEASON-01-01" "$SEASON-02-01" "$SEASON-03-01" "$SEASON-04-01" "$SEASON-05-01")
for i in "${!starts[@]}"; do
    window="season=$SEASON&startDateRange=${starts[$i]}T00:00:00.000Z&endDateRange=${ends[$i]}T00:00:00.000Z"
    default_cbbd+=("/games?$window" "/lines?$window")
done

cd "$root"

case "${1:-}" in
    "")
        echo "capturing the default cfbd set for $YEAR weeks 1-2 (${#default_cfbd[@]} metered requests)" >&2
        exec go run ./cmd/capture -provider cfbd "${default_cfbd[@]}"
        ;;
    --cbbd)
        echo "capturing the default cbbd set for season $SEASON (${#default_cbbd[@]} metered requests)" >&2
        exec go run ./cmd/capture -provider cbbd "${default_cbbd[@]}"
        ;;
esac

exec go run ./cmd/capture "$@"
