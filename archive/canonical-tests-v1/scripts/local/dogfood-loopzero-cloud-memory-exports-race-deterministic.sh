#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#104 (TestMemoryExports
# CreateIgnoresClientFilename flaky under `-race`).
#
# Wires 2 suites:
#   S1 — hermetic syntax test    (CI, no go, no kubectl)
#   S2 — LIVE bash 3-scenario    (Go-side hermetic — no Cleanroom needed)
#
# NOTE on the cleanroom mandate: this triplet is Go-side hermetic. The
# cleanroom-up-live-test-mandate INQUEBRÁVEL only applies to LIVE tests
# against the dev-public dashboard surface (curl + cookie chain against
# https://app-dev.loopzero.example). The #104 fix is a pure server-side
# concurrency invariant exercised by `go test -race`, runnable on any
# developer workstation. Scenario C in S2 takes ~30s for 10 iterations.
#
# Slug:        loopzero-cloud-memory-exports-race-deterministic
# Issue:       loopzero-cloud#104
# Wired in:    task test:dogfood:loopzero-cloud-memory-exports-race-deterministic
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero-cloud#104 (memory_exports race deterministic):
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
    echo "[dogfood-loopzero-cloud-memory-exports-race-deterministic] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-loopzero-cloud-memory-exports-race-deterministic_test.sh" ; then
        record "S2 LIVE bash 3/3" PASS
    else
        record "S2 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-loopzero-cloud-memory-exports-race-deterministic] ============================================================"
echo "[dogfood-loopzero-cloud-memory-exports-race-deterministic] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-loopzero-cloud-memory-exports-race-deterministic] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
