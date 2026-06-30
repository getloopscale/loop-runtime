#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#123 (engine error message
# references canonical .loopzero/services.json, NOT legacy loopzero.yaml).
#
# Wires 3 suites:
#   S1 — hermetic syntax test  (CI, no cluster)
#   S2 — Go unit tests         (CI, api/internal/routes + services)
#   S3 — LIVE bash 2-scenario  (OPERATOR HARD STOP — post-Cleanroom)

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero-cloud#123 (build-error-canonical-manifest):
  --hermetic-only   S1 (hermetic syntax) + S2 (Go unit) only (CI mode)
  --live-only       S3 (LIVE bash) only (operator mode, post-Cleanroom)
  (no flag)         all 3 suites
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
    echo "[dogfood-build-error-canonical-manifest] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-build-error-canonical-manifest-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
    if (cd "$REPO_ROOT" && go test -count=1 ./api/internal/routes/... -run TestBuilds_Create_MissingApps ./api/internal/services/... -run TestSuggestionFor 2>&1 | tail -5 | grep -q '^ok\|PASS') ; then
        record "S2 Go unit MissingApps + SuggestionFor" PASS
    else
        record "S2 Go unit MissingApps + SuggestionFor" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-build-error-canonical-manifest_test.sh" ; then
        record "S3 LIVE bash 2/2" PASS
    else
        record "S3 LIVE bash 2/2" FAIL
    fi
fi

echo "[dogfood-build-error-canonical-manifest] ============================================================"
echo "[dogfood-build-error-canonical-manifest] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-build-error-canonical-manifest] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d}\n' "$PASS" "$FAIL"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
