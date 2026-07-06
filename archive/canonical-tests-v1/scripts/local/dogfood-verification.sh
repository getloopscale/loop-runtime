#!/usr/bin/env bash
# Aggregate dogfood wrapper for Journey J-03 Verification (M0).
#
# Suites:
#   1. Hermetic syntax  — check-dev-public-verification-flow-syntax_test.sh
#   2. Frontend vitest  — verification page + ResendCooldownButton
#   3. LIVE bash        — dev-public-verification-flow_test.sh (operator, dev-public)
#
# Flags: --hermetic-only (CI) | --live-only (operator) | --json
# OPERATOR HARD STOP for suite 3 per .claude/rules/cleanroom-up-live-test-mandate.md.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
CLOUD_DIR="${LOOPZERO_CLOUD_DIR:-$REPO_ROOT/../loopzero-cloud}"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_MODE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --json) JSON_MODE=1; shift ;;
    -h|--help) sed -n '2,14p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[dogfood-verification] %s\n' "$*" >&2; }
PASS=0; FAIL=0
run() { local name="$1"; shift; if "$@"; then log "PASS $name"; PASS=$((PASS + 1)); else log "FAIL $name"; FAIL=$((FAIL + 1)); fi; }

suite_hermetic() { run "hermetic-syntax" bash "$REPO_ROOT/infra/tests/check-dev-public-verification-flow-syntax_test.sh"; }
suite_frontend() { run "dashboard-verification-vitest" bash -c "cd '$CLOUD_DIR/dashboard' && npx vitest run src/pages/verification.test.tsx src/components/verification/"; }
suite_live() { run "live-verification-flow" bash "$REPO_ROOT/infra/tests/e2e/dev-public-verification-flow_test.sh"; }

if [[ "$LIVE_ONLY" == "1" ]]; then
  suite_live
elif [[ "$HERMETIC_ONLY" == "1" ]]; then
  suite_hermetic; suite_frontend
else
  suite_hermetic; suite_frontend; suite_live
fi

log "RESULT: $PASS passed, $FAIL failed"
[[ "$JSON_MODE" == "1" ]] && printf '{"journey":"J-03-verification","pass":%d,"fail":%d}\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
