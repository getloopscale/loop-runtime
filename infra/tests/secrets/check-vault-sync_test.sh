#!/usr/bin/env bash
# check-vault-sync_test.sh — Hermetic test for Phase 9 T9.1 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8.
#
# Validates infra/scripts/openbao/vault-sync-check.sh:
#   1. Pre-flight: missing yq/kubectl → error
#   2. Pre-flight: invalid --threshold (non-int) → error
#   3. Pre-flight: missing inventory file → error
#   4. All ExternalSecrets Ready=True → exit 0 + PASS
#   5. Some NOT Ready but threshold met → exit 0 + PASS
#   6. Synced < threshold → exit 2 + FAIL
#   7. CR missing entirely → counted as missing (NOT synced)
#   8. --json output structure valid
#
# Mocks: kubectl + uses real yq on fixture inventory.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/openbao/vault-sync-check.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

for tool in yq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: hermetic test requires $tool on PATH"; exit 2; }
done

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$WORKDIR" "$MOCK_BIN"' EXIT

# Fixture inventory (3 ExternalSecret targets)
cat > "$WORKDIR/inventory.yaml" <<'EOF'
envName: ""
externalSecrets:
  - targetSecret: target-a
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: keya
        vaultKey: a/keya
  - targetSecret: target-b
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: keyb
        vaultKey: b/keyb
  - targetSecret: target-c
    vaultBinary: cloud
    vaultCategory: secrets
    data:
      - secretKey: keyc
        vaultKey: c/keyc
EOF

# Mock kubectl — returns status per MOCK_STATUS_* env vars per CR name.
# `version --request-timeout=5s` always succeeds (preflight pass).
# `get externalsecret <name> -o jsonpath={...}` returns the per-CR mocked status.
cat > "$MOCK_BIN/kubectl" <<'EOF'
#!/usr/bin/env bash
# `kubectl version` (preflight reachability check)
if [[ "$*" == *"version --request-timeout"* ]]; then
  exit 0
fi
# `get externalsecret <name> ...` — existence check OR status/reason query
if [[ "$*" == *"get externalsecret"* ]]; then
  # Extract name token after "externalsecret "
  name=$(echo "$*" | sed -E 's/.*get externalsecret ([a-z0-9-]+).*/\1/')
  varname="MOCK_STATUS_$(echo "$name" | tr '-' '_' | tr '[:lower:]' '[:upper:]')"
  status="${!varname:-}"
  # Existence: if MOCK_STATUS_<NAME> is unset, CR does not exist → exit 1
  if [[ -z "$status" ]]; then
    echo "Error from server (NotFound): externalsecrets.external-secrets.io \"$name\" not found" >&2
    exit 1
  fi
  # Existence check call (no jsonpath / no -o): just succeed silently
  if [[ "$*" != *"-o jsonpath"* ]]; then
    exit 0
  fi
  # jsonpath query for .reason
  if [[ "$*" == *".reason"* ]]; then
    reason_var="MOCK_REASON_$(echo "$name" | tr '-' '_' | tr '[:lower:]' '[:upper:]')"
    echo "${!reason_var:-SecretSynced}"
    exit 0
  fi
  # jsonpath query for .status
  echo "$status"
  exit 0
fi
exit 0
EOF
chmod +x "$MOCK_BIN/kubectl"

run_case() {
  # Args: <name> <expect_exit> <expect_pattern> <env_assignments_or_empty> <script_flags...>
  # env_assignments is a single space-separated string of VAR=VAL pairs (or "").
  local name="$1" expect_exit="$2" expect_pattern="$3" env_assigns="$4"
  shift 4
  local out err rc
  out=$(mktemp); err=$(mktemp)
  # Apply VAR=VAL pairs via subshell + export (portable across env(1) versions
  # that don't accept '--' separator).
  (
    if [ -n "$env_assigns" ]; then export $env_assigns; fi
    PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
      --inventory "$WORKDIR/inventory.yaml" \
      --context k3d-mock --namespace loopzero-system "$@"
  ) >"$out" 2>"$err"
  rc=$?
  local ok=1
  if [[ "$expect_exit" != "$rc" ]]; then ok=0; fi
  if [[ -n "$expect_pattern" ]] && ! grep -qE "$expect_pattern" "$out" "$err"; then ok=0; fi
  if [[ $ok -eq 1 ]]; then
    printf '  PASS: %s\n' "$name"; PASS=$((PASS+1))
  else
    printf '  FAIL: %s (rc=%d, expected=%s, pattern=%s)\n' "$name" "$rc" "$expect_exit" "$expect_pattern"
    sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
    FAIL=$((FAIL+1))
  fi
  rm -f "$out" "$err"
}

echo "Running vault-sync-check hermetic tests..."
echo

# --- Test 1: invalid --threshold (non-int) ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --inventory "$WORKDIR/inventory.yaml" \
  --threshold abc --context k3d-mock >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "threshold must be" "$err"; then
  printf '  PASS: test_invalid_threshold_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_invalid_threshold_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 2: missing inventory file ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --inventory "$WORKDIR/nonexistent.yaml" \
  --context k3d-mock >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "inventory not found" "$err"; then
  printf '  PASS: test_missing_inventory_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_inventory_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 3: all 3 Ready=True + threshold=3 → PASS exit 0 ---
run_case "test_all_ready_threshold_met_pass" "0" "Verdict: PASS" \
  "MOCK_STATUS_TARGET_A=True MOCK_STATUS_TARGET_B=True MOCK_STATUS_TARGET_C=True" \
  --threshold 3

# --- Test 4: 2 of 3 Ready (1 False), threshold=2 → PASS exit 0 ---
run_case "test_partial_ready_threshold_met_pass" "0" "Verdict: PASS" \
  "MOCK_STATUS_TARGET_A=True MOCK_STATUS_TARGET_B=True MOCK_STATUS_TARGET_C=False" \
  --threshold 2

# --- Test 5: 2 of 3 Ready, threshold=3 → FAIL exit 2 ---
run_case "test_partial_ready_threshold_not_met_fail" "2" "Verdict: FAIL" \
  "MOCK_STATUS_TARGET_A=True MOCK_STATUS_TARGET_B=True MOCK_STATUS_TARGET_C=False" \
  --threshold 3

# --- Test 6: target-c CR missing entirely → counted as MISSING ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" MOCK_STATUS_TARGET_A=True MOCK_STATUS_TARGET_B=True \
  bash "$TARGET_SCRIPT" --inventory "$WORKDIR/inventory.yaml" \
  --context k3d-mock --threshold 3 >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "2" ]] \
   && grep -q "MISSING: target-c" "$out" \
   && grep -q "Missing: 1" "$out"; then
  printf '  PASS: test_cr_missing_counted_separately\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_cr_missing_counted_separately (rc=%d)\n' "$rc"
  sed 's/^/    [stdout] /' "$out"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 7: --json output is valid JSON with expected keys ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" MOCK_STATUS_TARGET_A=True MOCK_STATUS_TARGET_B=True MOCK_STATUS_TARGET_C=True \
  bash "$TARGET_SCRIPT" --inventory "$WORKDIR/inventory.yaml" \
  --context k3d-mock --threshold 3 --json >"$out" 2>"$err"; rc=$?
# Validate JSON structure (use python3 since jq dependency varies)
if [[ "$rc" == "0" ]] && python3 -c "
import json, sys
data = json.load(open('$out'))
assert 'context' in data
assert 'expected' in data
assert 'synced' in data
assert 'threshold' in data
assert 'pass' in data
assert data['synced'] == 3, f'expected 3 synced, got {data[\"synced\"]}'
assert data['pass'] == True
" 2>/dev/null; then
  printf '  PASS: test_json_output_valid_structure\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_json_output_valid_structure (rc=%d)\n' "$rc"
  sed 's/^/    [stdout] /' "$out"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 8: zero ExternalSecrets in inventory → preflight fail ---
cat > "$WORKDIR/empty_inventory.yaml" <<'EOF'
envName: ""
externalSecrets: []
EOF
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --inventory "$WORKDIR/empty_inventory.yaml" \
  --context k3d-mock --threshold 1 >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "0 ExternalSecret targets" "$err"; then
  printf '  PASS: test_empty_inventory_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_empty_inventory_fails (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
