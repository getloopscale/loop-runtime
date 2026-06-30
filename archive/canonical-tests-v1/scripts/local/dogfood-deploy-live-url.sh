#!/usr/bin/env bash
# Plan m37-tenant-cr-project-sync-canonical-fix v1.0 T3.3 — aggregate
# dogfood wrapper for the cenário 4 BUILD → RELEASE → URL chain. Closes
# the canonical 3-script triplet debt that Sprint 8 #37 closure missed.
#
# Suites:
#   1) T3.2 hermetic syntax check (CI-safe)
#   2) Go unit tests api/internal/services/... (-race) — m37 RED test
#      TestEnsureEnvironment_DifferentProjectSameEnvName_TriggersAddEnvironment
#      MUST be GREEN.
#   3) Go unit tests api/internal/routes/... (-race) — mapBuildError ladder
#      + EnsureEnvironment wiring regression preserved.
#   4) T3.1 LIVE bash (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#      — 4 scenarios A/B/C/D end-to-end.
#
# Flags: --hermetic-only, --live-only, --json
set -euo pipefail

PLAN_SLUG="m37-tenant-cr-project-sync-canonical-fix"
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

Aggregate dogfood wrapper for loopzero-cloud#37 cenário 4 closure (m37 plan):
hermetic + Go (-race) + LIVE bash 4-scenario. Operator HARD STOP for LIVE
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
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-deploy-live-url-syntax_test.sh"
fi

# Suite 2 — Go services -race covers the m37 RED test
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S2-go-services-m37-red" "go test -race ./api/internal/services/... -run 'EnsureEnvironment'"
fi

# Suite 3 — Go routes -race regression
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S3-go-routes-regression" "go test -race ./api/internal/routes/..."
fi

# Suite 4 — LIVE bash (HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S4-live-deploy-live-url" "bash infra/tests/e2e/dev-public-deploy-live-url_test.sh"
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
