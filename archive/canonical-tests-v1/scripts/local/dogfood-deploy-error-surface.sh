#!/usr/bin/env bash
# Sprint 5 plan qa-sprint5-cleanup-2026-06-25 T3.3 — aggregate dogfood
# wrapper for the discriminated downstream-push error surface. Closes
# loopzero-cloud#33.
#
# Suites:
#   1) T3.1 hermetic syntax check (CI-safe)
#   2) Go unit tests api/internal/source/... (-race)
#   3) Go unit tests pkg/errors/...
#   4) T3.2 LIVE bash (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#
# Flags: --hermetic-only, --live-only, --json
set -euo pipefail

PLAN_SLUG="qa-sprint5-cleanup-2026-06-25"
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

Aggregate dogfood wrapper for loopzero-cloud#33 closure: hermetic + Go (-race) +
LIVE bash. Operator HARD STOP for LIVE per cleanroom mandate.
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

run_suite() {
    local id="$1" cmd="$2"
    if eval "$cmd" >/dev/null 2>&1; then
        RESULTS+=("$id:PASS")
    else
        RESULTS+=("$id:FAIL")
    fi
}

# Suite 1 — hermetic syntax test
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-deploy-error-surface-syntax_test.sh"
fi

# Suite 2 — Go source pkg unit tests (-race)
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S2-go-source-race" "go test -race ./api/internal/source/..."
fi

# Suite 3 — Go pkg/errors unit tests
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S3-go-pkg-errors" "go test ./pkg/errors/..."
fi

# Suite 4 — LIVE bash (HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S4-live-deploy-error-surface" "bash infra/tests/e2e/dev-public-deploy-error-surface_test.sh"
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
