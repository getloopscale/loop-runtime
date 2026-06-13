#!/usr/bin/env bash
# T0.1 of plan 2026-06-10-j22-openbao-transit-eso-sync-plan v1.2
# Hermetic structural test for infra/scripts/local/check-openbao-prereqs.sh
# NO network, NO kubectl, NO curl invocation. Pure shape + flag wiring.

set -uo pipefail
cd "$(dirname "$0")/.."

readonly SCRIPT="scripts/local/check-openbao-prereqs.sh"
readonly TEST_NAME="check-openbao-prereqs-syntax_test"

PASS_COUNT=0
FAIL_COUNT=0
FAILED_ASSERTIONS=()

assert() {
  local desc="$1"; local cond="$2"
  if eval "$cond"; then
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
    FAILED_ASSERTIONS+=("$desc")
    echo "  FAIL: $desc" >&2
    echo "    cond: $cond" >&2
  fi
}

# A1: script file exists
assert "A1: script file exists at $SCRIPT" "[ -f '$SCRIPT' ]"

# A2: bash -n parses (syntax-valid)
assert "A2: bash -n parses without syntax error" "bash -n '$SCRIPT' 2>/dev/null"

# A3: strict mode enabled (set -euo pipefail)
assert "A3: strict mode set -euo pipefail" "grep -qE '^\s*set -e?u?o pipefail|^set -euo pipefail' '$SCRIPT'"

# A4: shebang is bash-portable
assert "A4: shebang uses env bash" "head -1 '$SCRIPT' | grep -qE '^#!/usr/bin/env bash'"

# A5: --json flag wired
assert "A5: --json flag declared" "grep -qE '(--json|JSON_MODE)' '$SCRIPT'"

# A6: --hermetic flag wired (allows skip of LIVE probes for CI)
assert "A6: --hermetic OR --skip-live flag declared" "grep -qE '(--hermetic|--skip-live|HERMETIC)' '$SCRIPT'"

# A7: --context= flag declared (for multi-cluster)
assert "A7: --context= flag declared" "grep -qE '(--context=|CTX|KUBE_CONTEXT)' '$SCRIPT'"

# A8: --help flag declared
assert "A8: --help flag declared" "grep -qE '(--help|-h|usage\(\))' '$SCRIPT'"

# A9: usage() function present
assert "A9: usage() function defined" "grep -qE '^usage\(\)' '$SCRIPT'"

# A10: declares the 4 phase variables (phase1..phase4) per plan TDD
assert "A10: phase1 reported" "grep -qE 'phase1' '$SCRIPT'"
assert "A11: phase2 reported" "grep -qE 'phase2' '$SCRIPT'"
assert "A12: phase3 reported" "grep -qE 'phase3' '$SCRIPT'"
assert "A13: phase4 reported" "grep -qE 'phase4' '$SCRIPT'"

# A14: emits verdict
assert "A14: verdict reported" "grep -qE 'verdict' '$SCRIPT'"

# A15: JSON output mode emits valid JSON shape
assert "A15: JSON mode emits {\"verdict\":...} shape" "grep -qE '\"verdict\"' '$SCRIPT'"

# A16: probes openbao service when not hermetic
assert "A16: references openbao service (vault.vault-system OR openbao.vault-system)" "grep -qE '(openbao|vault)\.vault-system' '$SCRIPT'"

# A17: probes ESO ClusterSecretStore openbao-transit
assert "A17: references ClusterSecretStore openbao-transit" "grep -qE 'clustersecretstore.*openbao-transit|openbao-transit.*clustersecretstore' '$SCRIPT'"

# A18: exit code contract (0 = PASS, 1 = FAIL, 2 = usage error)
assert "A18: exit 0 path present" "grep -qE 'exit 0' '$SCRIPT'"
assert "A19: exit 1 path present (FAIL verdict)" "grep -qE 'exit 1' '$SCRIPT'"
assert "A20: exit 2 path present (usage error)" "grep -qE 'exit 2' '$SCRIPT'"

# A21: no destructive operations (no rm -rf, no kubectl delete)
assert "A21: no rm -rf in script" "! grep -qE 'rm -rf|rm -fr|kubectl.*delete' '$SCRIPT'"

# A22: cites the plan + task ID
assert "A22: cites plan + T0.1 task" "grep -qE 'T0\.1|2026-06-10-j22-openbao-transit' '$SCRIPT'"

# A23: no Co-Authored-By trailer (per global Rule 4)
assert "A23: no Co-Authored-By trailer in script header" "! grep -q 'Co-Authored-By' '$SCRIPT'"

# A24: no --no-verify usage
assert "A24: no --no-verify in script" "! grep -qE -- '--no-verify' '$SCRIPT'"

# A25: allowlist entry exists for this script
assert "A25: allowlist entry exists" "grep -qE 'check-openbao-prereqs\.sh' ../.claude/rules/shell-scripts-allowlist.md"

# Summary
echo ""
TOTAL=$((PASS_COUNT + FAIL_COUNT))
echo "$TEST_NAME: $PASS_COUNT/$TOTAL PASS / $FAIL_COUNT FAIL"

if [ $FAIL_COUNT -gt 0 ]; then
  echo "Failed assertions:"
  for a in "${FAILED_ASSERTIONS[@]}"; do
    echo "  - $a"
  done
  exit 1
fi
exit 0
