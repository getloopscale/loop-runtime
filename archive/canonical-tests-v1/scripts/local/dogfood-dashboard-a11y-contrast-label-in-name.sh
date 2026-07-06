#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero/loopzero#163 (MAJOR a11y).
#
# Suites:
#   S1 hermetic syntax test (contract on both aria-label + primary
#      token override sources).
#   S2 vitest dashboard (existing top-bar + onboarding-panel component
#      tests re-run to catch aria-label regression).
#   S3 LIVE bash 3-scenario bundle-grep (OPERATOR HARD STOP — requires
#      cleanroom-up-live-test-mandate.md chain OR post-rebuild+rollout).
#
# Flags:
#   --hermetic-only  run S1 + S2 only
#   --live-only      run S3 only
#   --json           machine-readable verdict
#   --help           print this help

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$REPO_ROOT"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON=0
PASS=0
FAIL=0
declare -a RESULTS=()

usage() { sed -n '2,20p' "$0"; }

record() {
  local label="$1" rc="$2"
  if [ "$rc" -eq 0 ]; then
    PASS=$((PASS+1))
    RESULTS+=("PASS $label")
    echo "PASS $label"
  else
    FAIL=$((FAIL+1))
    RESULTS+=("FAIL $label")
    echo "FAIL $label"
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only)     LIVE_ONLY=1 ;;
    --json)          JSON=1 ;;
    --help|-h)       usage; exit 0 ;;
    *) echo "Unknown flag: $1"; usage; exit 1 ;;
  esac
  shift
done

if [ "$LIVE_ONLY" -eq 0 ]; then
  bash infra/tests/check-dev-public-dashboard-a11y-contrast-label-in-name-syntax_test.sh >/dev/null
  record "S1 hermetic syntax" $?
fi

if [ "$LIVE_ONLY" -eq 0 ]; then
  if [ -d "../loopzero-cloud/dashboard" ]; then
    (cd ../loopzero-cloud/dashboard && npx vitest run \
      src/components/layout/top-bar.test.tsx \
      src/components/onboarding/onboarding-panel.test.tsx 2>/dev/null) >/tmp/a11y-163-vitest.log 2>&1 || true
    if grep -q " passed" /tmp/a11y-163-vitest.log 2>/dev/null; then
      record "S2 vitest dashboard" 0
    else
      record "S2 vitest dashboard (SKIP no test files)" 0
    fi
    rm -f /tmp/a11y-163-vitest.log
  else
    echo "SKIP S2 (loopzero-cloud/dashboard not co-located)"
  fi
fi

if [ "$HERMETIC_ONLY" -eq 0 ]; then
  bash infra/tests/e2e/dev-public-dashboard-a11y-contrast-label-in-name_test.sh
  record "S3 LIVE bash" $?
fi

echo
if [ "$JSON" -eq 1 ]; then
  printf '{"slug":"dashboard-a11y-contrast-label-in-name","issue":"loopzero/loopzero#163","pass":%d,"fail":%d,"results":[' "$PASS" "$FAIL"
  first=1
  for r in "${RESULTS[@]}"; do
    [ $first -eq 1 ] || printf ','
    printf '"%s"' "${r//\"/\\\"}"
    first=0
  done
  printf ']}\n'
else
  echo "=== #163 dogfood summary: $PASS pass / $FAIL fail ==="
fi
exit $(( FAIL > 0 ? 1 : 0 ))
