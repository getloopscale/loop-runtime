#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero/loopzero#154 (billing 404 upgrade dead).
#
# Wires 2 suites:
#   S1 — hermetic syntax test    (CI, no cluster; extended for #154)
#   S2 — LIVE bash 3-scenario    (OPERATOR HARD STOP — post-Cleanroom)
#
# Slug:        billing-stripe-seed-restarts-cloud
# Issue:       loopzero/loopzero#154
# Wired in:    task test:dogfood:billing-stripe-seed-restarts-cloud
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero/loopzero#154 (billing 404 upgrade dead):
  --hermetic-only   S1 only (CI mode, no cluster)
  --live-only       S2 only (operator mode, post-Cleanroom)
  (no flag)         both suites

Operator HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL for the
LIVE suite — requires Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
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
    echo "[dogfood-billing-stripe-seed-restarts-cloud] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-seed-stripe-api-keys-canonical-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-billing-stripe-seed-restarts-cloud_test.sh" ; then
        record "S2 LIVE bash 3/3" PASS
    else
        record "S2 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-billing-stripe-seed-restarts-cloud] ============================================================"
echo "[dogfood-billing-stripe-seed-restarts-cloud] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-billing-stripe-seed-restarts-cloud] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
