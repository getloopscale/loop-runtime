#!/usr/bin/env bash
# dogfood-c13-valid-destructive-config.sh
#
# Plan c13-valid-destructive-config-chaos (T3.1). Aggregate dogfood wrapper for
# the c13 chaos scenario. Three suites:
#   S1 — hermetic structural guard (check-chaos-c13-syntax_test.sh)
#   S2 — Go regression test (-race) + benchmark (the deterministic proof + DATA)
#   S3 — LIVE bash chaos scenario (OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md)
#
# Flags: --hermetic-only (CI, S1+S2) · --live-only (operator, S3) · --json
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$REPO_ROOT"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_MODE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1 ; shift ;;
    --live-only)     LIVE_ONLY=1 ; shift ;;
    --json)          JSON_MODE=1 ; shift ;;
    -h|--help)       sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[dogfood-c13] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log()  { printf '[dogfood-c13] %s\n' "$*" >&2 ; }
fail=0

run_s1() {
  log "S1 — hermetic guard (check-chaos-c13-syntax_test.sh)"
  bash infra/tests/check-chaos-c13-syntax_test.sh || fail=$((fail+1))
}
run_s2() {
  log "S2 — Go regression test (-race) + benchmark"
  ( cd api && go test -race ./internal/services/ \
      -run TestReleaseConvergenceLoop_SyncedButUnhealthy_TimedOut_FailsLoud ) || fail=$((fail+1))
  ( cd api && go test ./internal/services/ \
      -bench BenchmarkReleaseConvergenceLoop_FailLoudTick -benchmem -run '^$' ) || fail=$((fail+1))
}
run_s3() {
  log "S3 — LIVE chaos bash (OPERATOR HARD STOP — Cleanroom TOTAL + sponsor JUNTOS)"
  bash infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh --json || fail=$((fail+1))
}

if [ "$LIVE_ONLY" -eq 1 ]; then
  run_s3
elif [ "$HERMETIC_ONLY" -eq 1 ]; then
  run_s1 ; run_s2
else
  run_s1 ; run_s2 ; run_s3
fi

if [ "$JSON_MODE" -eq 1 ]; then
  printf '{"wrapper":"dogfood-c13-valid-destructive-config","fail_count":%d}\n' "$fail"
fi
[ "$fail" -eq 0 ] && { log "dogfood-c13 PASS"; exit 0; } || { log "dogfood-c13 FAIL ($fail)"; exit 1; }
