#!/usr/bin/env bash
# T2.4 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — hermetic
# render test for the openbao-transit ClusterSecretStore template.
#
# Asserts the helm template emits the canonical CSS shape per plan T2.4
# acceptance criteria when openbaoTransit.enabled=true, AND emits zero
# resources when the flag is false (opt-in semantic).
#
# NO kubectl, NO cluster access — pure `helm template` + grep/yq probes.

set -uo pipefail
cd "$(dirname "$0")/../../.."

readonly CHART="infra/helm/loopzero-external-secrets"
readonly TEST_NAME="clustersecretstore-openbao-transit_test"

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

# === Section A: opt-out (default) — flag false renders ZERO CSS ===
DEFAULT_OUT="$(helm template t "$CHART" --set envName=dev 2>/dev/null || true)"
assert "A1: default helm template includes the loopzero-vault-secrets ExternalSecrets" "echo \"\$DEFAULT_OUT\" | grep -q 'kind: ExternalSecret'"
assert "A2: default helm template does NOT render openbao-transit CSS" "! echo \"\$DEFAULT_OUT\" | grep -q 'name: openbao-transit'"
assert "A3: default helm template does NOT include the openbao-transit CSS definition" "! echo \"\$DEFAULT_OUT\" | grep -qE '^  name: openbao-transit$'"

# === Section B: opt-in — flag true with minimal valid values ===
ENABLED_OUT="$(helm template t "$CHART" \
  --set envName=dev \
  --set openbaoTransit.enabled=true \
  --set openbaoTransit.server="https://openbao.vault-system.svc:8200" \
  --set openbaoTransit.roleId="rid-abc123" \
  --set openbaoTransit.secretRef.name="openbao-eso-secret-id" \
  --set openbaoTransit.secretRef.namespace="external-secrets" \
  --set openbaoTransit.secretRef.key="secret-id" \
  2>&1)"
ENABLED_EXIT=$?
assert "B1: enabled helm template exit 0" "[ $ENABLED_EXIT -eq 0 ]"
assert "B2: enabled output includes ClusterSecretStore kind" "echo \"\$ENABLED_OUT\" | grep -q 'kind: ClusterSecretStore'"
assert "B3: CSS metadata.name == openbao-transit" "echo \"\$ENABLED_OUT\" | grep -q 'name: openbao-transit'"
assert "B4: spec.provider.vault present" "echo \"\$ENABLED_OUT\" | grep -q 'vault:'"
assert "B5: spec.provider.vault.server set to canonical URL" "echo \"\$ENABLED_OUT\" | grep -q 'server: \"https://openbao.vault-system.svc:8200\"'"
assert "B6: auth.appRole present (D4 ADR per-tenant key isolation requires AppRole)" "echo \"\$ENABLED_OUT\" | grep -q 'appRole:'"
assert "B7: auth.appRole.path defaults to approle" "echo \"\$ENABLED_OUT\" | grep -q 'path: \"approle\"'"
assert "B8: auth.appRole.roleId injected from values" "echo \"\$ENABLED_OUT\" | grep -q 'roleId: \"rid-abc123\"'"
assert "B9: auth.appRole.secretRef.name injected" "echo \"\$ENABLED_OUT\" | grep -q 'name: \"openbao-eso-secret-id\"'"
assert "B10: auth.appRole.secretRef.namespace injected" "echo \"\$ENABLED_OUT\" | grep -q 'namespace: \"external-secrets\"'"
assert "B11: auth.appRole.secretRef.key injected" "echo \"\$ENABLED_OUT\" | grep -q 'key: \"secret-id\"'"
assert "B12: loopzero.io/plan label cites the canonical plan slug" "echo \"\$ENABLED_OUT\" | grep -q 'loopzero.io/plan: 2026-06-10-j22-openbao-transit-eso-sync'"
assert "B13: loopzero.io/task label cites T2.4" "echo \"\$ENABLED_OUT\" | grep -q 'loopzero.io/task: T2.4'"

# === Section C: misconfig — fail-loud refusal ===
MISSING_SERVER_OUT="$(helm template t "$CHART" \
  --set envName=dev \
  --set openbaoTransit.enabled=true \
  --set openbaoTransit.roleId="rid-abc" \
  --set openbaoTransit.secretRef.name="openbao-eso-secret-id" \
  2>&1)"
MISSING_SERVER_EXIT=$?
assert "C1: helm template FAILS when openbaoTransit.server is empty (fail-loud)" "[ $MISSING_SERVER_EXIT -ne 0 ]"
assert "C2: error message mentions openbaoTransit.server" "echo \"\$MISSING_SERVER_OUT\" | grep -q 'openbaoTransit.server'"

MISSING_ROLEID_OUT="$(helm template t "$CHART" \
  --set envName=dev \
  --set openbaoTransit.enabled=true \
  --set openbaoTransit.server="https://openbao.vault-system.svc:8200" \
  --set openbaoTransit.secretRef.name="openbao-eso-secret-id" \
  2>&1)"
MISSING_ROLEID_EXIT=$?
assert "C3: helm template FAILS when openbaoTransit.roleId is empty (fail-loud)" "[ $MISSING_ROLEID_EXIT -ne 0 ]"
assert "C4: error message mentions openbaoTransit.roleId" "echo \"\$MISSING_ROLEID_OUT\" | grep -q 'openbaoTransit.roleId'"

MISSING_SECRETREF_OUT="$(helm template t "$CHART" \
  --set envName=dev \
  --set openbaoTransit.enabled=true \
  --set openbaoTransit.server="https://openbao.vault-system.svc:8200" \
  --set openbaoTransit.roleId="rid-abc" \
  --set openbaoTransit.secretRef.name="" \
  2>&1)"
MISSING_SECRETREF_EXIT=$?
assert "C5: helm template FAILS when openbaoTransit.secretRef.name is empty" "[ $MISSING_SECRETREF_EXIT -ne 0 ]"

# === Section D: caBundle conditional ===
WITH_CA_OUT="$(helm template t "$CHART" \
  --set envName=dev \
  --set openbaoTransit.enabled=true \
  --set openbaoTransit.server="https://openbao.vault-system.svc:8200" \
  --set openbaoTransit.roleId="rid" \
  --set openbaoTransit.secretRef.name="openbao-eso-secret-id" \
  --set openbaoTransit.caBundle="LS0tLS1CRUdJTi==" \
  2>&1)"
assert "D1: caBundle rendered when set" "echo \"\$WITH_CA_OUT\" | grep -q 'caBundle: \"LS0tLS1CRUdJTi==\"'"

# === Section E: anti-patterns ===
assert "E1: template file has no Co-Authored-By trailer" "! grep -q 'Co-Authored-By' '$CHART/templates/clustersecretstore-openbao-transit.yaml'"
assert "E2: template file does NOT hardcode any secret_id value" "! grep -q 'secret_id: ' '$CHART/templates/clustersecretstore-openbao-transit.yaml'"
assert "E3: template file cites the plan + task ID" "grep -q 'T2.4 of plan 2026-06-10-j22-openbao-transit-eso-sync' '$CHART/templates/clustersecretstore-openbao-transit.yaml'"
assert "E4: allowlist entry exists for this test script" "grep -q 'clustersecretstore-openbao-transit_test.sh' .claude/rules/shell-scripts-allowlist.md"

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
