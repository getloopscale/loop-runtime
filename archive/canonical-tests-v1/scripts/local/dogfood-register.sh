#!/usr/bin/env bash
# Aggregate dogfood wrapper for Journey J-02 Register (M0).
#
# Suites:
#   1. Hermetic syntax  — check-dev-public-register-flow-syntax_test.sh
#   2. Backend Go       — loopzero-cloud go test ./internal/routes -run TestMe_
#      (provisioning snapshot T1.2 + lazy starter provision T1.3)
#   3. Frontend vitest  — register + completing-screen + provisioning hook
#   4. LIVE bash        — dev-public-register-flow_test.sh (operator, dev-public)
#
# Flags: --hermetic-only (CI) | --live-only (operator) | --json
#
# OPERATOR HARD STOP for suite 4 per .claude/rules/cleanroom-up-live-test-mandate.md.

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
    -h|--help) sed -n '2,17p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[dogfood-register] %s\n' "$*" >&2; }
PASS=0; FAIL=0
run() {
  local name="$1"; shift
  if "$@"; then log "PASS $name"; PASS=$((PASS + 1)); else log "FAIL $name"; FAIL=$((FAIL + 1)); fi
}

suite_hermetic() {
  run "hermetic-syntax" bash "$REPO_ROOT/infra/tests/check-dev-public-register-flow-syntax_test.sh"
}
suite_backend() {
  run "cloud-me-provisioning" bash -c "cd '$CLOUD_DIR' && go test ./internal/routes/ -run TestMe_ -count=1"
}
suite_frontend() {
  run "dashboard-register-vitest" bash -c "cd '$CLOUD_DIR/dashboard' && npx vitest run src/pages/register.test.tsx src/components/register/ src/hooks/use-provisioning-status.test.tsx"
}
suite_live() {
  run "live-register-flow" bash "$REPO_ROOT/infra/tests/e2e/dev-public-register-flow_test.sh"
}

if [[ "$LIVE_ONLY" == "1" ]]; then
  suite_live
elif [[ "$HERMETIC_ONLY" == "1" ]]; then
  suite_hermetic; suite_backend; suite_frontend
else
  suite_hermetic; suite_backend; suite_frontend; suite_live
fi

log "RESULT: $PASS passed, $FAIL failed"
[[ "$JSON_MODE" == "1" ]] && printf '{"journey":"J-02-register","pass":%d,"fail":%d}\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
