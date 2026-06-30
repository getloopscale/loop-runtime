#!/usr/bin/env bash
# Plan: loopzero-promote-no-op-fail-loud-p0 v1.0
# Closes: Red Team loopzero-cloud#120 — loopzero promote silent no-op (rel-rel- double
#         prefix + .loopzero.local URL regression + activation canary).
#
# Aggregate dogfood wrapper per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`
# INQUEBRÁVEL.
#
# Suites:
#   S1 — hermetic syntax check (CI-safe)
#   S2 — Go unit tests (-race) covering promotion_orchestrator + 3 new tests
#        (TestPromote_NewReleaseID_IsCanonical_NoDoublePrefix +
#         TestPromote_PublicDomain_PreferredOverBase +
#         TestPromote_PublicDomainEmpty_FallsBackToBaseDomain)
#   S3 — LIVE bash 3-scenario A/B/C (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#
# Flags: --hermetic-only, --live-only, --json, --help
set -euo pipefail

PLAN_SLUG="loopzero-promote-no-op-fail-loud-p0"
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

Aggregate dogfood wrapper for plan ${PLAN_SLUG} (closes Red Team #120).
Operator HARD STOP for LIVE per cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
EOF
            exit 0
            ;;
        *) echo "unknown flag: $arg" >&2; exit 2 ;;
    esac
done

if [ "$HERMETIC_ONLY" -eq 1 ] && [ "$LIVE_ONLY" -eq 1 ]; then
    echo "mutual exclusion: --hermetic-only and --live-only" >&2; exit 2
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

# S1 — hermetic
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-promote-no-op-syntax_test.sh"
fi

# S2 — Go (-race) covering the engine-side fix
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S2-go-promotion-race" "go test -race -run TestPromote ./api/internal/services/..."
fi

# S3 — LIVE bash (OPERATOR HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S3-live-promote-no-op" "PROJECT_NAME=\${PROJECT_NAME:-loopzerokit-example} bash infra/tests/e2e/dev-public-promote-no-op_test.sh"
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
