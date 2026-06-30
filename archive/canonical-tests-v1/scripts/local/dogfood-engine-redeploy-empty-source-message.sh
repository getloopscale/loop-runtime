#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#126 (engine
# redeploy_of empty-source error message).
#
# Wires 2 suites:
#   S1 — hermetic syntax test    (CI, no go, no kubectl)
#   S2 — LIVE bash 3-scenario    (Go-side hermetic — no Cleanroom needed)
#
# Slug:        engine-redeploy-empty-source-message
# Issue:       loopzero-cloud#126
# Wired in:    task test:dogfood:engine-redeploy-empty-source-message
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero-cloud#126 (engine redeploy empty-source msg):
  --hermetic-only   S1 (hermetic syntax) only (CI mode)
  --live-only       S2 (LIVE bash, Go-side) only
  (no flag)         both suites
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
    echo "[dogfood-engine-redeploy-empty-source-message] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-engine-redeploy-empty-source-message-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-engine-redeploy-empty-source-message_test.sh" ; then
        record "S2 LIVE bash 3/3" PASS
    else
        record "S2 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-engine-redeploy-empty-source-message] ============================================================"
echo "[dogfood-engine-redeploy-empty-source-message] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-engine-redeploy-empty-source-message] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
