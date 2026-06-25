#!/usr/bin/env bash
# Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T2.3 — aggregate dogfood
# wrapper retroactively closing loopzero-cloud#34 (Sprint 6 BUILD_SUBMIT_FAILED
# discrimination). The Sprint 6 fix shipped Go-side tests; per the new
# INQUEBRÁVEL feedback `feedback_issue_fix_requires_failing_e2e_test_2026
# _06_25` every issue closure now requires a LIVE bash + dogfood wrapper.
#
# Suites:
#   1) T2.2 hermetic syntax check (CI-safe)
#   2) Go unit tests api/internal/services/... (-race) — SubmitBuildError
#      reasons + AsSubmitBuildError dispatch (Sprint 6 origin)
#   3) Go unit tests api/internal/routes/... — mapBuildError ladder
#      (TestMapBuildError_DispatchesViaAsSubmitBuildError + DEPLOY_IN_PROGRESS
#      short-circuit + typed-LoopZeroError dispatch + genuine catchall)
#   4) Go unit tests pkg/errors/... — NewBuildSubmitFailedFromReason and
#      NewBuildSubmitFailedFromReasonWithTrace (Sprint 8 T1.1 trace-id)
#   5) T2.1 LIVE bash (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
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

Aggregate dogfood wrapper retroactively closing loopzero-cloud#34: hermetic +
Go (-race) + LIVE bash 6-scenario. Operator HARD STOP for LIVE per
cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
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

# Resolve repo root (canonical 4-up from archive/canonical-tests-v1/scripts/local/).
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
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-build-trigger-error-surface-syntax_test.sh"
fi

# Suite 2 — Go services SubmitBuildError reasons + AsSubmitBuildError dispatch
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S2-go-services-race" "go test -race ./api/internal/services/..."
fi

# Suite 3 — Go routes mapBuildError ladder (Sprint 6 origin + Sprint 8 trace-id thread-through)
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S3-go-routes-mapbuilderror" "go test ./api/internal/routes/... -run 'MapBuildError'"
fi

# Suite 4 — Go pkg/errors trace-id fallback (Sprint 8 T1.1)
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S4-go-pkg-errors-traceid" "go test ./pkg/errors/..."
fi

# Suite 5 — LIVE bash (HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S5-live-build-trigger-error-surface" "bash infra/tests/e2e/dev-public-build-trigger-error-surface_test.sh"
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
