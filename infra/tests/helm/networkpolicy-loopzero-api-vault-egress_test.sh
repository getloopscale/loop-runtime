#!/usr/bin/env bash
# T3.3 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — hermetic
# render test for the loopzero-api vault egress NetworkPolicy.
#
# Asserts (a) the policy is omitted by default (opt-in semantic),
# (b) the policy renders with the canonical combined podSelector +
# namespaceSelector + TCP/8200 shape when enabled, and (c) values
# overrides flow through correctly.
#
# NO kubectl, NO cluster — pure `helm template` + grep probes.

set -uo pipefail
cd "$(dirname "$0")/../../.."

readonly CHART="infra/helm/loopzero-api"
readonly TEST_NAME="networkpolicy-loopzero-api-vault-egress_test"

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

# === Section A: opt-out (default) — emits nothing ===
DEFAULT_OUT="$(helm template t "$CHART" 2>/dev/null)"
assert "A1: default helm template renders some content" "[ -n \"\$DEFAULT_OUT\" ]"
assert "A2: default helm template does NOT include loopzero-api-vault-egress NetworkPolicy" "! echo \"\$DEFAULT_OUT\" | grep -qE 'name:.*loopzero-api.*vault-egress'"
assert "A3: default helm template does NOT include T3.3 plan/task label" "! echo \"\$DEFAULT_OUT\" | grep -qE 'loopzero.io/task: T3.3'"

# === Section B: opt-in — flag true renders the policy ===
ENABLED_OUT="$(helm template t "$CHART" \
  --set networkPolicy.vaultEgress.enabled=true \
  2>&1)"
ENABLED_EXIT=$?
assert "B1: enabled helm template exit 0" "[ $ENABLED_EXIT -eq 0 ]"
assert "B2: output includes kind: NetworkPolicy" "echo \"\$ENABLED_OUT\" | grep -q 'kind: NetworkPolicy'"
assert "B3: NetworkPolicy name contains loopzero-api-vault-egress" "echo \"\$ENABLED_OUT\" | grep -qE 'name:.*loopzero-api.*vault-egress'"
assert "B4: spec.podSelector matches app: loopzero-api" "echo \"\$ENABLED_OUT\" | grep -qE 'podSelector:\$' && echo \"\$ENABLED_OUT\" | grep -q 'app: loopzero-api'"
assert "B5: policyTypes includes Egress" "echo \"\$ENABLED_OUT\" | grep -qE 'policyTypes:\$|- Egress'"
assert "B6: egress namespaceSelector matches vault-system" "echo \"\$ENABLED_OUT\" | grep -q 'kubernetes.io/metadata.name: vault-system'"
assert "B7: egress podSelector matches openbao" "echo \"\$ENABLED_OUT\" | grep -q 'app.kubernetes.io/name: openbao'"
assert "B8: egress port 8200 declared" "echo \"\$ENABLED_OUT\" | grep -qE 'port: 8200'"
assert "B9: egress protocol TCP declared" "echo \"\$ENABLED_OUT\" | grep -qE 'protocol: TCP'"
assert "B10: loopzero.io/plan label cites canonical slug" "echo \"\$ENABLED_OUT\" | grep -q 'loopzero.io/plan: 2026-06-10-j22-openbao-transit-eso-sync'"
assert "B11: loopzero.io/task label cites T3.3" "echo \"\$ENABLED_OUT\" | grep -q 'loopzero.io/task: T3.3'"

# === Section C: defense in depth — combined namespaceSelector + podSelector ===
# This is the canonical shape that prevents a "matches any pod in
# vault-system" leak. If someone refactors the template to use OR
# semantics (separate egress entries instead of combined selector under
# a single `to:` block), this assertion fails.
COMBINED_BLOCK="$(echo "$ENABLED_OUT" | awk '/egress:/,/ports:/')"
assert "C1: vault-system + openbao combined in same egress to-block (AND semantic)" "echo \"\$COMBINED_BLOCK\" | grep -A4 'namespaceSelector' | grep -q 'podSelector'"

# === Section D: values overrides flow through ===
OVERRIDE_OUT="$(helm template t "$CHART" \
  --set networkPolicy.vaultEgress.enabled=true \
  --set networkPolicy.vaultEgress.namespace=custom-ns \
  --set networkPolicy.vaultEgress.podName=openbao-edge \
  --set networkPolicy.vaultEgress.port=9999 \
  2>&1)"
assert "D1: namespace override flows through" "echo \"\$OVERRIDE_OUT\" | grep -q 'kubernetes.io/metadata.name: custom-ns'"
assert "D2: podName override flows through" "echo \"\$OVERRIDE_OUT\" | grep -q 'app.kubernetes.io/name: openbao-edge'"
assert "D3: port override flows through" "echo \"\$OVERRIDE_OUT\" | grep -qE 'port: 9999'"

# === Section E: anti-patterns ===
TPL_FILE="$CHART/templates/networkpolicy.yaml"
assert "E1: template file exists at canonical path" "[ -f '$TPL_FILE' ]"
assert "E2: template file has no Co-Authored-By trailer" "! grep -q 'Co-Authored-By' '$TPL_FILE'"
assert "E3: template cites the plan + task ID" "grep -q 'T3.3 of plan 2026-06-10-j22-openbao-transit-eso-sync' '$TPL_FILE'"
assert "E4: allowlist entry exists" "grep -q 'networkpolicy-loopzero-api-vault-egress_test.sh' .claude/rules/shell-scripts-allowlist.md"

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
