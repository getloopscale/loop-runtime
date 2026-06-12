#!/usr/bin/env bash
# T7.2 hermetic structural test for dev-public-deployments-flow_test.sh.
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j17-deployments-realtime-plan.md
#
# Validates structure WITHOUT executing the LIVE script: bash -n syntax,
# 4 scenario functions, pre-flight, 3 flags wired, allowlist entry,
# exit-code contract. NO network. NO kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-deployments-flow_test.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0

assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n' "$desc"
    FAIL=$((FAIL + 1))
  fi
}

contains() {
  grep -qF -- "$2" "$1" && echo 1 || echo 0
}

contains_re() {
  grep -qE -- "$2" "$1" && echo 1 || echo 0
}

printf '[deployments-flow-syntax-test] gates:\n'

# 1. Target script exists
[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists" "$exists"
if [[ "$exists" != "1" ]]; then
  printf 'ABORT: %s missing\n' "$TARGET"
  exit 1
fi

# 2. bash -n syntax check
if bash -n "$TARGET" 2>/dev/null; then
  assert "bash -n syntax check" 1
else
  assert "bash -n syntax check" 0
fi

# 3. 4 scenario functions present
for fn in scenario_a scenario_b scenario_c scenario_d; do
  assert "function ${fn} defined" "$(contains_re "$TARGET" "^${fn}\(\)")"
done

# 4. pre-flight HEALTHCHECK helper
assert "preflight() helper defined" "$(contains_re "$TARGET" "^preflight\(\)")"

# 5. Three flags wired: --skip-live, --scenario=, --json
assert "--skip-live flag wired" "$(contains "$TARGET" "--skip-live")"
assert "--scenario= flag wired" "$(contains "$TARGET" "--scenario=")"
assert "--json flag wired" "$(contains "$TARGET" "--json")"

# 6. Exit-code contract: non-zero on any FAIL, zero otherwise
assert "exit 1 on FAIL" "$(contains "$TARGET" "exit 1")"
assert "exit 0 on PASS" "$(contains "$TARGET" "exit 0")"

# 7. References the 3 key probes from the plan
assert "SSE stream path probed" "$(contains "$TARGET" "builds/stream")"
assert "Logs endpoint probed" "$(contains "$TARGET" "/logs")"
assert "marker pattern checked" "$(contains "$TARGET" "::loopzero::")"

# 8. Allowlist entry exists for the LIVE target
if [[ -f "$ALLOWLIST" ]]; then
  assert "allowlist contains dev-public-deployments-flow_test.sh" \
    "$(contains "$ALLOWLIST" "dev-public-deployments-flow_test.sh")"
  assert "allowlist contains check-dev-public-deployments-flow-syntax_test.sh" \
    "$(contains "$ALLOWLIST" "check-dev-public-deployments-flow-syntax_test.sh")"
fi

TOTAL=$((PASS + FAIL))
printf '\n[deployments-flow-syntax-test] verdict: %d/%d PASS, %d FAIL\n' \
  "$PASS" "$TOTAL" "$FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
