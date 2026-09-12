#!/usr/bin/env bash
#
# Capture a /scoreboard response for later study.
#
# The live-sync cadence is currently driven by "is today inside a week row",
# which is true for every hour of the season whether or not a ball is in the
# air. Replacing it with something that tracks actual games needs an answer to
# what the feed looks like *between* games -- how far ahead it lists kickoffs,
# whether a delayed game reads scheduled or in_progress, and whether an
# abandoned game ever clears out of in_progress. One Saturday afternoon sample
# cannot answer any of those, so this collects them over a few days.
#
# Writes the raw body to samples/scoreboard/<UTC timestamp>-<classification>.json
# and appends one summary row to samples/scoreboard/summary.tsv. Both are
# gitignored. Every run spends one metered CFBD request per classification.
#
# Usage:  scripts/capture-scoreboard.sh [classification ...]   (default: fbs)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/samples/scoreboard"
summary="$out/summary.tsv"
mkdir -p "$out"

# The key lives in .env, which is gitignored. Sourcing it rather than taking an
# argument keeps it out of the process list and out of shell history.
if [[ -z "${CFB_DATA_API_KEY:-}" && -f "$root/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    . "$root/.env"
    set +a
fi

if [[ -z "${CFB_DATA_API_KEY:-}" ]]; then
    echo "CFB_DATA_API_KEY is not set and .env does not define it" >&2
    exit 1
fi

if [[ ! -s "$summary" ]]; then
    printf 'captured_at\tclassification\thttp\tseconds\tbytes\tscheduled\tin_progress\tcompleted\tother\tnext_kickoff\tearliest_start\tlatest_start\toldest_in_progress_start\n' > "$summary"
fi

now="$(date -u +%Y%m%dT%H%M%SZ)"

for classification in "${@:-fbs}"; do
    body="$out/$now-$classification.json"

    # A failed capture is data too -- a 502 at 3am is exactly the kind of thing
    # this is meant to notice -- so the row is written either way and the
    # non-zero exit is deferred to the end.
    read -r http seconds < <(
        curl -sS -o "$body" -w '%{http_code} %{time_total}\n' \
            -H "Authorization: Bearer $CFB_DATA_API_KEY" \
            -H 'Accept: application/json' \
            "https://api.collegefootballdata.com/scoreboard?classification=$classification" \
            || echo "000 0"
    )

    bytes=$(wc -c < "$body" | tr -d ' ')

    if [[ "$http" == "200" ]] && jq -e . "$body" >/dev/null 2>&1; then
        # Counts by status, the next kickoff still to come, the span of start
        # dates the response covers, and the start time of the longest-running
        # in_progress game -- which is what says whether a stuck game ever
        # clears, and so how long "something is being played" may be trusted.
        jq -r --arg at "$now" --arg cls "$classification" \
              --arg http "$http" --arg secs "$seconds" --arg bytes "$bytes" '
            def count($s): [.[] | select(.status == $s)] | length;
            [ $at, $cls, $http, $secs, $bytes,
              (count("scheduled") | tostring),
              (count("in_progress") | tostring),
              (count("completed") | tostring),
              ([.[] | select(.status | IN("scheduled","in_progress","completed") | not)] | length | tostring),
              ([.[] | select(.status == "scheduled") | .startDate] | sort | (.[0] // "-")),
              ([.[].startDate] | sort | (.[0] // "-")),
              ([.[].startDate] | sort | (.[-1] // "-")),
              ([.[] | select(.status == "in_progress") | .startDate] | sort | (.[0] // "-"))
            ] | @tsv' "$body" >> "$summary"
    else
        printf '%s\t%s\t%s\t%s\t%s\t-\t-\t-\t-\t-\t-\t-\t-\n' \
            "$now" "$classification" "$http" "$seconds" "$bytes" >> "$summary"
        failed=1
    fi
done

exit "${failed:-0}"
