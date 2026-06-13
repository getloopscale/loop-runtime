#!/usr/bin/env bash
# Hermetic structural test for dev-public-preview-flow_test.sh.
# NO network, NO kubectl, NO curl — verifies the LIVE test script
# is parseable, declares the right scenarios, wires flags correctly,
# and stays in the shell-scripts allowlist. Mirrors the pattern from
# check-dev-public-deployments-flow-syntax_test.sh.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-preview-flow_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
PASS=0
FAIL=0

assert() {
  local name="$1" cond="$2"
  if eval "$cond" >/dev/null 2>&1; then
    PASS=$((PASS+1))
    echo "  PASS  $name"
  else
    FAIL=$((FAIL+1))
    echo "  FAIL  $name  (assertion: $cond)"
  fi
}

echo "→ Hermetic checks against $SCRIPT"

assert "script exists"                       "[[ -f '$SCRIPT' ]]"
assert "script is executable"                "[[ -x '$SCRIPT' ]]"
assert "bash -n parses cleanly"              "bash -n '$SCRIPT'"
assert "Scenario A function defined"         "grep -q '^scenario_A()' '$SCRIPT'"
assert "Scenario B function defined"         "grep -q '^scenario_B()' '$SCRIPT'"
assert "Scenario C function defined"         "grep -q '^scenario_C()' '$SCRIPT'"
assert "Scenario D function defined"         "grep -q '^scenario_D()' '$SCRIPT'"
assert "Scenario E function defined"         "grep -q '^scenario_E()' '$SCRIPT'"
assert "Scenario F function defined (D2/D3)" "grep -q '^scenario_F()' '$SCRIPT'"
assert "Scenario G function defined (D5)"    "grep -q '^scenario_G()' '$SCRIPT'"
assert "Scenario F asserts preview_slot single-slot SoT" \
                                             "grep -q 'preview_slot' '$SCRIPT' && grep -q 'active_pr_number' '$SCRIPT'"
assert "Scenario G asserts kind='preview' separation (D5)" \
                                             "grep -qE \"kind\\s*=\\s*'preview'\" '$SCRIPT' && grep -qE \"kind\\s*=\\s*'permanent'\" '$SCRIPT'"
assert "Scenario A references tenant + project pre-flight" \
                                             "grep -q 'tenant CR' '$SCRIPT' && grep -q 'projects' '$SCRIPT'"
assert "Scenario B asserts RBAC previewenvironments (#65)" \
                                             "grep -q 'previewenvironments.loopzero.io' '$SCRIPT'"
assert "Scenario C asserts loopzero-app SA + automount=false (#67)" \
                                             "grep -q 'loopzero-app' '$SCRIPT' && grep -q 'automountServiceAccountToken' '$SCRIPT'"
assert "Scenario D asserts canonical formula (#69)" \
                                             "grep -q 'loopzero-\\\${tenant}-\\\${env_project}-\\\${env_name}' '$SCRIPT'"
assert "Scenario E asserts LE issuer (#68)" \
                                             "grep -qE \"Let.s ?Encrypt|LetsEncrypt\" '$SCRIPT'"
assert "--skip-live flag wired"              "grep -q 'SKIP_LIVE' '$SCRIPT'"
assert "--scenario= flag wired"              "grep -q 'SCENARIO=' '$SCRIPT'"
assert "--json flag wired"                   "grep -q 'JSON_OUTPUT' '$SCRIPT'"
assert "exit code reflects FAIL count"       "grep -q 'exit .*FAIL.* -eq 0' '$SCRIPT'"
assert "PREVIEW_PUBLIC_DOMAIN configurable"  "grep -q 'PREVIEW_PUBLIC_DOMAIN' '$SCRIPT'"
assert "listed in shell-scripts-allowlist.md" \
                                             "grep -q '$SCRIPT' '$ALLOWLIST'"

echo
echo "Result: $PASS passed, $FAIL failed"
exit $FAIL
