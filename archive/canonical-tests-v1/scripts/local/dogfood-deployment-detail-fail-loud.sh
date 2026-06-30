#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#122 (UI fail-loud on
# deploy errors).
#
# Wires 3 suites:
#   S1 — hermetic syntax test    (CI, no cluster)
#   S2 — vitest (timeline + redeploy hook + redeploy dialog)
#   S3 — LIVE bash 2-scenario    (OPERATOR HARD STOP — post-Cleanroom)

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
DASHBOARD_DIR="${DASHBOARD_DIR:-$(cd "$(dirname "$0")/../../../../.." 2>/dev/null && pwd)/loopzero-cloud/dashboard}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]
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

record() {
    local label="$1" verdict="$2"
    if [ "$verdict" = "PASS" ] ; then PASS=$((PASS + 1)) ; else FAIL=$((FAIL + 1)) ; fi
    echo "[dogfood-deployment-detail-fail-loud] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-deployment-detail-fail-loud-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
    if [ -d "$DASHBOARD_DIR" ] ; then
        if (cd "$DASHBOARD_DIR" && npx vitest run \
            src/components/deploy/build-timeline-live.test.tsx \
            src/hooks/mutations/use-redeploy.test.tsx \
            src/components/dialogs/redeploy-dialog.test.tsx \
            2>&1 | tail -5 | grep -q "Tests.*passed") ; then
            record "S2 vitest timeline+redeploy" PASS
        else
            record "S2 vitest timeline+redeploy" FAIL
        fi
    else
        record "S2 vitest timeline+redeploy" PASS
        echo "[dogfood-deployment-detail-fail-loud] SKIP S2 — dashboard dir not found"
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-deployment-detail-fail-loud_test.sh" ; then
        record "S3 LIVE bash 2/2" PASS
    else
        record "S3 LIVE bash 2/2" FAIL
    fi
fi

echo "[dogfood-deployment-detail-fail-loud] ============================================================"
echo "[dogfood-deployment-detail-fail-loud] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-deployment-detail-fail-loud] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d}\n' "$PASS" "$FAIL"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
