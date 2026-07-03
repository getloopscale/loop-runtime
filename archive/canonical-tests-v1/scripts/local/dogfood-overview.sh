#!/usr/bin/env bash
# Aggregate dogfood wrapper for Journey J-06 Overview (KPI strip).
# Suites: S1 hermetic syntax gate + S2 dashboard vitest (aggregator + KPI strip)
#         + S3 engine Go build_stats handler tests + S4 LIVE bash (operator).
#
# Flags: --hermetic-only (CI: S1+S2+S3), --live-only (operator: S4), --json.
# The LIVE suite is an OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_MODE=0
for arg in "$@"; do
  case "$arg" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only) LIVE_ONLY=1 ;;
    --json) JSON_MODE=1 ;;
  esac
done

pass=0
fail=0
run() {
  local name="$1"; shift
  printf '[dogfood-overview] >>> %s\n' "$name" >&2
  if "$@"; then pass=$((pass + 1)); printf '[dogfood-overview] PASS %s\n' "$name" >&2
  else fail=$((fail + 1)); printf '[dogfood-overview] FAIL %s\n' "$name" >&2; fi
}

if [[ "$LIVE_ONLY" == "0" ]]; then
  run "S1 hermetic syntax gate" bash "$REPO_ROOT/infra/tests/check-dev-public-overview-flow-syntax_test.sh"
  run "S2 dashboard vitest (overview KPIs + strip)" bash -c \
    "cd '$REPO_ROOT/../loopzero-cloud/dashboard' && npx vitest run src/hooks/use-overview-kpis.test.tsx src/components/overview/overview-kpi-strip.test.tsx src/pages/overview.test.tsx"
  run "S3 engine build_stats handler tests" bash -c \
    "cd '$REPO_ROOT/api' && go test ./internal/routes/ -run TestBuildStats"
fi

if [[ "$HERMETIC_ONLY" == "0" ]]; then
  run "S4 LIVE overview flow (OPERATOR HARD STOP)" bash "$REPO_ROOT/infra/tests/e2e/dev-public-overview-flow_test.sh"
fi

printf '[dogfood-overview] RESULT: %d passed, %d failed\n' "$pass" "$fail" >&2
if [[ "$JSON_MODE" == "1" ]]; then
  printf '{"journey":"J-06-overview","suites_pass":%d,"suites_fail":%d}\n' "$pass" "$fail"
fi
[ "$fail" -eq 0 ]
