#!/usr/bin/env bash
# T8.3 of plan 2026-06-07-runtime-envs-per-project-m2 v1.2.
# Hermetic structural test for dev-public-runtime-env-flow_test.sh.
# NO network, NO kubectl, NO real curl. Asserts:
#   - bash -n syntax clean
#   - 4 scenario functions defined (A/B/C/D)
#   - preflight helper present
#   - 3 flags wired (--skip-live, --scenario=, --json)
#   - exit-code contract (1 on FAIL / 0 on PASS)
#   - key probes (project field POST + ?project= query + EC-1 PROJECT_NOT_FOUND)
#   - allowlist cross-check
set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-runtime-env-flow_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"

assert_count=0
fail_count=0

assert() {
  local cond="$1"; local msg="$2"
  assert_count=$((assert_count + 1))
  if eval "$cond"; then
    echo "  [OK]   $msg"
  else
    echo "  [FAIL] $msg" >&2
    fail_count=$((fail_count + 1))
  fi
}

echo "T8.3 hermetic check for $SCRIPT"

# 1. bash -n syntax
assert "bash -n $SCRIPT" "bash -n parses cleanly"

# 2. Scenario functions A/B/C/D
for s in A B C D; do
  assert "grep -q 'scenario_'$s'()' $SCRIPT" "scenario_$s function defined"
done

# 3. Preflight + mint_jwt helpers
assert "grep -q 'preflight()' $SCRIPT" "preflight() function present"
assert "grep -q 'mint_jwt()' $SCRIPT" "mint_jwt() helper present"

# 4. Flags wired
assert "grep -q -- '--skip-live' $SCRIPT" "--skip-live flag handled"
assert "grep -q -- '--scenario=' $SCRIPT" "--scenario= flag handled"
assert "grep -q -- '--json' $SCRIPT" "--json flag handled"

# 5. Exit contract
assert "grep -q 'FAIL_COUNT.*-gt 0' $SCRIPT" "exit 1 when FAIL_COUNT > 0"
assert "grep -q 'exit 0' $SCRIPT" "exit 0 on clean run"

# 6. Key M2 probes — project field POST + ?project= query + EC-1 check
assert "grep -q '\"project\":\"web\"' $SCRIPT" "project field present in scenario A POST body"
assert "grep -q '?project=web' $SCRIPT" "?project= query in scenario C DELETE"
assert "grep -q 'PROJECT_NOT_FOUND' $SCRIPT" "EC-1 PROJECT_NOT_FOUND probed (D + A fallback)"
assert "grep -q 'project is required' $SCRIPT" "B asserts 'project is required' message"

# 7. EC-1 SEPA-CRITICAL guard
assert "grep -q 'definitely-not-mine' $SCRIPT" "D uses obviously-foreign project to test EC-1"
assert "grep -q 'SEPA-CRITICAL' $SCRIPT" "EC-1 importance documented inline"

# 8. Allowlist cross-check
assert "grep -q 'dev-public-runtime-env-flow_test.sh' $ALLOWLIST" "script listed in shell-scripts-allowlist"

# 9. JSON contract shape (machine-readable verdict)
assert "grep -q '\"results\":\[' $SCRIPT" "JSON output includes results array"

echo ""
echo "TOTAL: $assert_count assertions, $fail_count failed"
[ "$fail_count" -gt 0 ] && exit 1
exit 0
