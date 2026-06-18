#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# T6.3 of plan deploy-timeline-canonical-5-phases v1.1
#
# Aggregate dogfood wrapper for the canonical Deploy Timeline feature.
# Invokes 4 suites in sequence:
#   1. T6.1 hermetic syntax test (CI-safe, no cluster)
#   2. Go aggregator + adapter unit tests (-race)
#   3. Go scrub + routes hermetic tests (-race)
#   4. T6.2 LIVE bash 5-scenario test (OPERATOR HARD STOP)
#
# Flags:
#   --hermetic-only     CI mode — suites 1+2+3, skip 4
#   --live-only         operator mode — suites 1+4 (hermetic syntax + LIVE)
#   --json              machine-readable verdict
#   --help              this text
#
# Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: suite 4 requires
# Cleanroom TOTAL + UP + post-Cleanroom seeds before invocation.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON=0
HELP=0

for arg in "$@"; do
    case "$arg" in
        --hermetic-only) HERMETIC_ONLY=1 ;;
        --live-only) LIVE_ONLY=1 ;;
        --json) JSON=1 ;;
        --help|-h) HELP=1 ;;
        *) echo "unknown flag: $arg" >&2; exit 1 ;;
    esac
done

if [[ $HELP -eq 1 ]]; then
    sed -n '1,30p' "$0" | sed -n '/^#/p' | sed 's/^# //; s/^#//'
    exit 0
fi

PASS=0
FAIL=0
declare -A RESULTS

run_suite() {
    local id="$1"; shift
    local desc="$1"; shift
    echo "[suite $id] $desc..."
    if "$@"; then
        RESULTS[$id]="PASS"
        PASS=$((PASS+1))
    else
        RESULTS[$id]="FAIL"
        FAIL=$((FAIL+1))
        echo "  FAIL suite $id" >&2
    fi
}

# Suite 1 — hermetic syntax (always runs)
run_suite "1.hermetic-syntax" "T6.1 LIVE script structural test" \
    bash "$REPO_ROOT/infra/tests/check-dev-public-deploy-timeline-syntax_test.sh"

if [[ $LIVE_ONLY -ne 1 ]]; then
    # Suite 2 — Go aggregator + adapter tests
    run_suite "2.go-aggregator" "aggregator + 5 adapter unit tests (-race)" \
        bash -c "cd '$REPO_ROOT' && go test ./api/internal/aggregator/... -race -count=1"

    # Suite 3 — Go scrub + routes tests
    run_suite "3.go-routes-scrub" "scrub + deploy_timeline routes (-race)" \
        bash -c "cd '$REPO_ROOT' && go test ./api/internal/scrub/... ./api/internal/routes/... -race -count=1 -run 'TestScrub|TestDeployTimeline|TestRegisterDeployTimeline'"
fi

if [[ $HERMETIC_ONLY -ne 1 ]]; then
    # Suite 4 — LIVE bash (OPERATOR HARD STOP)
    echo "[suite 4] LIVE bash — OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md"
    run_suite "4.live-bash" "T6.2 LIVE 5-scenario test" \
        bash "$REPO_ROOT/infra/tests/e2e/dev-public-deploy-timeline_test.sh"
fi

VERDICT="PASS"
[[ $FAIL -gt 0 ]] && VERDICT="FAIL"

if [[ $JSON -eq 1 ]]; then
    printf '{"verdict":"%s","pass":%d,"fail":%d,"suites":{' "$VERDICT" "$PASS" "$FAIL"
    first=1
    for k in "${!RESULTS[@]}"; do
        [[ $first -eq 0 ]] && printf ','
        printf '"%s":"%s"' "$k" "${RESULTS[$k]}"
        first=0
    done
    printf '}}\n'
else
    echo "dogfood-deploy-timeline: PASS=$PASS FAIL=$FAIL — $VERDICT"
fi

[[ $FAIL -eq 0 ]]
