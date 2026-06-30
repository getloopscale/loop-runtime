#!/usr/bin/env bash
# Plan: seed-post-cleanroom-tenant-cr-provisioning v1.0
# Closes: Red Team loopzero-cloud#118 — QA Red Team identity loga em dev-public
#         mas Tenant CR ausente em k3d-loopzero-mgmt → deploys falham.
#
# Aggregate dogfood wrapper per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`
# INQUEBRÁVEL: every issue closure requires hermetic syntax + LIVE bash triplet.
#
# Suites:
#   S1 — hermetic syntax check (CI-safe)
#   S2 — LIVE bash 2-scenario (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#
# Flags: --hermetic-only, --live-only, --json, --help
set -euo pipefail

PLAN_SLUG="seed-post-cleanroom-tenant-cr-provisioning"
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

Aggregate dogfood wrapper for plan ${PLAN_SLUG} (closes Red Team #118).
Operator HARD STOP for LIVE per cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
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

# Suite 1 — hermetic syntax test (no network, no kubectl)
if [ "$LIVE_ONLY" -eq 0 ]; then
    run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-seed-post-cleanroom-tenant-cr-syntax_test.sh"
fi

# Suite 2 — LIVE bash (OPERATOR HARD STOP)
if [ "$HERMETIC_ONLY" -eq 0 ]; then
    run_suite "S2-live-tenant-cr" "bash infra/tests/e2e/dev-public-seed-post-cleanroom-tenant-cr_test.sh"
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
