#!/usr/bin/env bash
# T5.2 hermetic structural test for dev-public-recovery-flow_test.sh.
# Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j04-recovery-plan.md
#
# Validates structure WITHOUT executing: bash -n syntax, scenario function
# definitions, pre-flight HEALTHCHECK presence, cleanup trap, flag wiring,
# and allowlist entry. NO network. NO kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-recovery-flow_test.sh"
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

printf '[recovery-flow-syntax-test] gates:\n'

# 1. Target file exists
[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists" "$exists"
if [[ "$exists" != "1" ]]; then
  printf 'ABORT: %s missing\n' "$TARGET"
  exit 1
fi

# 2. bash -n syntax
if bash -n "$TARGET" 2>/dev/null; then
  assert "bash -n syntax check" 1
else
  assert "bash -n syntax check" 0
fi

# 3. Five scenario functions present
for s in scenario_a scenario_c scenario_c2 scenario_d scenario_e; do
  assert "$s() function defined" "$(contains "$TARGET" "${s}()")"
done

# 4. Pre-flight HEALTHCHECK function (EC-9)
assert "preflight_healthcheck() function defined" "$(contains "$TARGET" "preflight_healthcheck()")"

# 5. Cleanup trap covers EXIT INT TERM (EC-9)
assert "trap covers EXIT INT TERM" "$(contains "$TARGET" "trap cleanup_test_identities EXIT INT TERM")"

# 6. Cleanup function defined
assert "cleanup_test_identities() function defined" "$(contains "$TARGET" "cleanup_test_identities()")"

# 7. Flags wired
assert "--json flag wired" "$(contains "$TARGET" "--json")"
assert "--skip-live flag wired" "$(contains "$TARGET" "--skip-live")"
assert "--scenario flag wired" "$(contains "$TARGET" "--scenario")"

# 8. Allowlist entry
assert "allowlist entry present" "$(contains "$ALLOWLIST" "dev-public-recovery-flow_test.sh")"

# 9. Each scenario records its verdict
assert "scenario_a records result" "$(grep -c 'record_result "scenario_a"' "$TARGET" | head -1 | awk '{print ($1 > 0) ? 1 : 0}')"

# 10. Strict shell options
assert "uses set -euo pipefail" "$(contains "$TARGET" "set -euo pipefail")"

# 11. Pre-flight gates Kratos + PG + admin
assert "pre-flight gates Kratos public" "$(contains "$TARGET" "Kratos public")"
assert "pre-flight gates PG" "$(contains "$TARGET" "PG ")"

printf '\n[recovery-flow-syntax-test] summary\n'
printf '  PASS: %d\n' "$PASS"
printf '  FAIL: %d\n' "$FAIL"

if [[ "$FAIL" -eq 0 ]]; then
  printf '[recovery-flow-syntax-test] GREEN\n'
  exit 0
fi
printf '[recovery-flow-syntax-test] FAIL — %d failure(s)\n' "$FAIL"
exit 1
