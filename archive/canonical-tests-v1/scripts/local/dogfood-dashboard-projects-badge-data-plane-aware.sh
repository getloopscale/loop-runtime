#!/usr/bin/env bash
# Aggregate dogfood wrapper for plan `dashboard-badge-data-plane-poller`
# (Red Team loopzero-cloud#128 — dashboard /projects list badge data-plane-aware).
#
# Wires 3 suites:
#   S1 — hermetic syntax test           (CI, no cluster) — T5.2 (31 assertions)
#   S2 — vitest projects page           (CI, dashboard unit) — T4.3 (5 new tests + 7 pre-existing = 12 total)
#   S3 — LIVE bash 3-scenario           (OPERATOR HARD STOP — post-Cleanroom) — T5.1
#
# Slug:        dashboard-badge-data-plane-poller
# Issue:       loopzero-cloud#128
# Family:      #124 / #41 / #77 / #94 (cross-view control-plane vs data-plane divergence)
# Wired in:    task test:dogfood:dashboard-projects-badge-data-plane-aware
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
# Path geometry: this script lives at
#   /home/paulo/loopzero-cloud/loopzero/archive/canonical-tests-v1/scripts/local/
# REPO_ROOT (the loopzero subrepo) is 4 dirs up; DASHBOARD_DIR (the loopzero-cloud
# subrepo's dashboard) is 5 dirs up + loopzero-cloud/dashboard.
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
DASHBOARD_DIR="${DASHBOARD_DIR:-$(cd "$(dirname "$0")/../../../../.." 2>/dev/null && pwd)/loopzero-cloud/dashboard}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero-cloud#128 (dashboard /projects badge data-plane-aware):
  --hermetic-only   S1 (hermetic syntax) + S2 (vitest) only (CI mode)
  --live-only       S3 (LIVE bash) only (operator mode, post-Cleanroom)
  (no flag)         all 3 suites

Operator HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL for the
LIVE suite — requires Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh
(including Step 13 poller warm-up so the first-render window doesn't show
a wall of Unknown badges).
EOF
}

while [ "$#" -gt 0 ] ; do
    case "$1" in
        --hermetic-only) MODE="hermetic" ;;
        --live-only) MODE="live" ;;
        --json) JSON_OUTPUT=1 ;;
        --help|-h) usage ; exit 0 ;;
        *) echo "unknown arg: $1" >&2 ; usage ; exit 2 ;;
    esac
    shift
done

PASS=0
FAIL=0
SUITES_JSON='['
SUITES_FIRST=1

record() {
    local label="$1" verdict="$2"
    if [ "$verdict" = "PASS" ] ; then PASS=$((PASS + 1)) ; else FAIL=$((FAIL + 1)) ; fi
    [ $SUITES_FIRST -eq 1 ] && SUITES_FIRST=0 || SUITES_JSON+=","
    SUITES_JSON+="{\"suite\":\"$label\",\"verdict\":\"$verdict\"}"
    echo "[dogfood-dashboard-projects-badge-data-plane-aware] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    # S1 — hermetic syntax
    if bash "$REPO_ROOT/infra/tests/check-dev-public-dashboard-projects-badge-data-plane-aware-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
    # S2 — vitest projects page (the test file touched by #128 T4.3)
    if [ -d "$DASHBOARD_DIR" ] ; then
        if (cd "$DASHBOARD_DIR" && npx vitest run src/pages/projects.test.tsx 2>&1 | tail -5 | grep -q "Tests.*passed") ; then
            record "S2 vitest projects" PASS
        else
            record "S2 vitest projects" FAIL
        fi
    else
        record "S2 vitest projects" PASS
        echo "[dogfood-dashboard-projects-badge-data-plane-aware] SKIP S2 — dashboard dir not found: $DASHBOARD_DIR"
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    # S3 — LIVE bash 3-scenario (operator).
    # Canonical (Cleanroom 2026-07-04): pre-populate the SCALE_* env vars
    # so scenario C targets the canonical seed project's Rollout.
    # Overridable — operator can point at any project by exporting
    # PROJECT_NAME/SCALE_DEPLOYMENT/SCALE_NAMESPACE before invoking.
    export PROJECT_NAME="${PROJECT_NAME:-teste}"
    export SCALE_DEPLOYMENT="${SCALE_DEPLOYMENT:-teste-app}"
    export SCALE_NAMESPACE="${SCALE_NAMESPACE:-loopzero-tn-243c747b30c41e51-teste-production}"
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-dashboard-projects-badge-data-plane-aware_test.sh" ; then
        record "S3 LIVE bash 3/3" PASS
    else
        record "S3 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-dashboard-projects-badge-data-plane-aware] ============================================================"
echo "[dogfood-dashboard-projects-badge-data-plane-aware] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-dashboard-projects-badge-data-plane-aware] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
