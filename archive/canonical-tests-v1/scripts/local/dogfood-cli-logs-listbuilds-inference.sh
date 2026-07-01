#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#160
# (loopzero logs sem --build "No builds found" apesar de builds existirem).
#
# Slug:        cli-logs-listbuilds-inference
# Issue:       loopzero-cloud#160
# Wired in:    task test:dogfood:cli-logs-listbuilds-inference
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
CLI_REPO_ROOT="${CLI_REPO_ROOT:-$HOME/loopzero-cloud/loopzero-cli}"

usage() { cat <<EOF
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
SUITES_JSON='['
SUITES_FIRST=1

record() {
    local label="$1" verdict="$2"
    if [ "$verdict" = "PASS" ] ; then PASS=$((PASS + 1)) ; else FAIL=$((FAIL + 1)) ; fi
    [ $SUITES_FIRST -eq 1 ] && SUITES_FIRST=0 || SUITES_JSON+=","
    SUITES_JSON+="{\"suite\":\"$label\",\"verdict\":\"$verdict\"}"
    echo "[dogfood-cli-logs-listbuilds-inference] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-cli-logs-listbuilds-inference-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
    if [ -d "$CLI_REPO_ROOT" ] ; then
        S2_LOG="/tmp/dogfood-160-cli-tests.log"
        ( cd "$CLI_REPO_ROOT" && npm test -- --testPathPattern=logs.test ) >"$S2_LOG" 2>&1 || true
        if fgrep -q "Tests:       36 passed" "$S2_LOG" 2>/dev/null || fgrep -q "36 passed" "$S2_LOG" 2>/dev/null ; then
            record "S2 CLI unit tests (logs #160 pair)" PASS
        else
            record "S2 CLI unit tests (logs #160 pair)" FAIL
        fi
    else
        record "S2 CLI unit tests SKIP (loopzero-cli not cloned)" PASS
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-cli-logs-listbuilds-inference_test.sh" ; then
        record "S3 LIVE bash 3/3" PASS
    else
        record "S3 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-cli-logs-listbuilds-inference] Result: $PASS PASS, $FAIL FAIL"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
