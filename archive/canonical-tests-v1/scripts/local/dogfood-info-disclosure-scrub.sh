#!/usr/bin/env bash
# Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T4.2 — aggregate dogfood
# wrapper retroactively completing Sprint 7 #35 triplet. The Sprint 7 scrub
# extension (api/internal/routes/scrub.go scrubInternalTerms) shipped with
# LIVE-only; per the new INQUEBRÁVEL feedback every issue closure now
# requires hermetic syntax + dogfood wrapper triplet.
#
# Suites:
#   1) T4.1 hermetic syntax check (CI-safe)
#   2) Go unit tests api/internal/routes/... — scrub denylist regex
#   3) T1.2 LIVE bash (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#
# Flags: --hermetic-only, --live-only, --json
set -euo pipefail

PLAN_SLUG="qa-sprint8-cleanup-2026-06-25"
HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0
RESULTS=()

for arg in "$@"; do
    case "$arg" in
        --hermetic-only) HERMETIC_ONLY=1 ;;
        --live-only) LIVE_ONLY=1 ;;
        --json) JSON_OUTPUT=1 ;;
        --help)
            cat <<EOF
Usage: $(basename "$0") [--hermetic-only] [--live-only] [--json] [--help]

Aggregate dogfood wrapper retroactively completing Sprint 7 #35 triplet:
hermetic + Go (-race) + LIVE bash 7-scenario. Operator HARD STOP for LIVE
per cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
EOF
            exit 0
            ;;
        *) echo "unknown flag: $arg" >&2; exit 2 ;;
    esac
done

if [ "$HERMETIC_ONLY" -eq 1 ] && [ "$LIVE_ONLY" -eq 1 ]; then
    echo "mutual exclusion: --hermetic-only and --live-only" >&2
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

run_suite() {
    local id="$1" cmd="$2"
    if (cd "$REPO_ROOT" && eval "$cmd") >/dev/null 2>&1; then
        RESULTS+=("$id:PASS")
    else
        RESULTS+=("$id:FAIL")
    fi
}

# Suite 1 — hermetic syntax test
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-info-disclosure-scrub-syntax_test.sh"
fi

# Suite 2 — Go routes (-race) covers scrubInternalTerms denylist
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S2-go-routes-scrub-race" "go test -race ./api/internal/routes/... -run 'Scrub'"
fi

# Suite 3 — LIVE bash (HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S3-live-info-disclosure-scrub" "bash infra/tests/e2e/dev-public-info-disclosure-scrub_test.sh"
fi

fail_count=0
for r in "${RESULTS[@]}"; do
    case "$r" in *:FAIL) fail_count=$((fail_count + 1)) ;; esac
done

if [ "$JSON_OUTPUT" -eq 1 ]; then
    printf '{"plan":"%s","suites":[' "$PLAN_SLUG"
    first=1
    for r in "${RESULTS[@]}"; do
        IFS=':' read -r id verdict <<<"$r"
        [ $first -eq 0 ] && printf ','
        printf '{"id":"%s","verdict":"%s"}' "$id" "$verdict"
        first=0
    done
    printf '],"fail_count":%d}\n' "$fail_count"
else
    for r in "${RESULTS[@]}"; do echo "$r"; done
    echo "fail_count=$fail_count"
fi

[ "$fail_count" -eq 0 ]
