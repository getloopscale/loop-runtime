#!/usr/bin/env bash
# check-seed-loopzero-secrets_test.sh — Hermetic test for T8.1 EC-21 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8.
#
# Validates infra/scripts/openbao/seed-loopzero-secrets.sh:
#   1. Pre-flight: missing --env / --secret-values-file → error exit
#   2. Pre-flight: missing BAO_ADDR / BAO_TOKEN (no dry-run) → error exit
#   3. Dry-run: processes all 13 ExternalSecret targets without bao writes
#   4. Vault path construction: applies vaultBinaryOverride correctly
#      (tenant-operator GITEA_TOKEN → api/secrets/gitea/token, not
#      operators/secrets/gitea/token)
#   5. Missing secret value: logged as SKIP, not failure
#   6. Idempotency: second invocation with same values produces same paths
#   7. Failed bao kv put: counted in failed, exit code 2
#
# Mocks: bao CLI (records args + scriptable exit), uses real yq + jq.
# Fixture: minimal inventory (3 ExternalSecrets, 5 keys) + values file.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/infra"
TARGET_SCRIPT="$REPO_ROOT/scripts/openbao/seed-loopzero-secrets.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

# Pre-flight: yq + jq must be available on the host (script depends on them).
for tool in yq jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "FATAL: hermetic test requires $tool on PATH"
    exit 2
  fi
done

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# --- Fixture inventory (minimal subset mirroring real schema) ---
cat > "$WORKDIR/inventory.yaml" <<'EOF'
envName: ""
secretStoreRef:
  kind: ClusterSecretStore
  name: loopzero-secrets-secrets
defaultRefreshInterval: 1h
externalSecrets:
  - targetSecret: gitea-admin
    targetNamespace: loopzero-system
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: token
        vaultKey: gitea/token

  - targetSecret: loopzero-api-stripe
    targetNamespace: loopzero-system
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: STRIPE_SECRET_KEY
        vaultKey: stripe/secret_key

  - targetSecret: loopzero-tenant-operator
    targetNamespace: loopzero-system
    vaultBinary: operators
    vaultCategory: secrets
    data:
      - secretKey: GITEA_TOKEN
        vaultKey: gitea/token
        vaultBinaryOverride: api
      - secretKey: DIGITALOCEAN_TOKEN
        vaultKey: digitalocean/token
      - secretKey: SPACES_ACCESS_KEY_ID
        vaultKey: spaces/access_key_id
EOF

# --- Fixture values file (operator-provided) ---
cat > "$WORKDIR/values.yaml" <<'EOF'
secrets:
  api:
    gitea/token: "ghp_test_gitea_token"
    stripe/secret_key: "sk_test_stripe"
  operators:
    digitalocean/token: "dop_v1_test"
    # spaces/access_key_id intentionally absent → SKIP path
EOF

# --- Fixture values file with ALL keys (idempotency case) ---
cat > "$WORKDIR/values_full.yaml" <<'EOF'
secrets:
  api:
    gitea/token: "ghp_test_gitea_token"
    stripe/secret_key: "sk_test_stripe"
  operators:
    digitalocean/token: "dop_v1_test"
    spaces/access_key_id: "spaces_test_key"
EOF

MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$WORKDIR" "$MOCK_BIN"' EXIT

# Mock bao — records every invocation to $BAO_LOG (env-controlled);
# exit per MOCK_BAO_EXIT (default 0). Never connects to real Vault.
cat > "$MOCK_BIN/bao" <<'EOF'
#!/usr/bin/env bash
echo "BAO_CALL: $*" >> "${BAO_LOG:-/dev/null}"
exit "${MOCK_BAO_EXIT:-0}"
EOF
chmod +x "$MOCK_BIN/bao"

run_case() {
  local name="$1" expect_exit="$2" expect_pattern="$3"
  shift 3
  local out err rc
  out=$(mktemp); err=$(mktemp)
  PATH="$MOCK_BIN:$PATH" "$@" bash "$TARGET_SCRIPT" \
    --env vault-local \
    --secret-values-file "$WORKDIR/values.yaml" \
    --inventory "$WORKDIR/inventory.yaml" \
    >"$out" 2>"$err"
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

echo "Running seed-loopzero-secrets hermetic tests..."
echo

# --- Test 1: missing --env arg → usage error ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --secret-values-file "$WORKDIR/values.yaml" >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "env required" "$err"; then
  printf '  PASS: test_missing_env_arg_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_env_arg_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 2: missing --secret-values-file → usage error ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --env vault-local >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "secret-values-file required" "$err"; then
  printf '  PASS: test_missing_values_file_arg_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_values_file_arg_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 3: missing BAO_ADDR (no --dry-run) → preflight fail ---
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --env vault-local \
  --secret-values-file "$WORKDIR/values.yaml" \
  --inventory "$WORKDIR/inventory.yaml" >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "BAO_ADDR env var required" "$err"; then
  printf '  PASS: test_missing_bao_addr_no_dry_run_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_bao_addr_no_dry_run_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 4: dry-run processes all 5 keys WITHOUT bao writes ---
BAO_LOG="$WORKDIR/bao_dryrun.log"
: > "$BAO_LOG"
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN BAO_LOG="$BAO_LOG" PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --env vault-local \
  --secret-values-file "$WORKDIR/values.yaml" \
  --inventory "$WORKDIR/inventory.yaml" \
  --dry-run >"$out" 2>"$err"; rc=$?
# Expected: total=5 seeded=4 skipped=1 (spaces key missing) failed=0; no bao calls
dry_total=$(grep -oE "total=[0-9]+" "$err" | head -1 | cut -d= -f2)
dry_seeded=$(grep -oE "seeded=[0-9]+" "$err" | head -1 | cut -d= -f2)
dry_skipped=$(grep -oE "skipped=[0-9]+" "$err" | head -1 | cut -d= -f2)
bao_calls=$(wc -l < "$BAO_LOG")
if [[ "$rc" == "0" ]] && [[ "$dry_total" == "5" ]] && [[ "$dry_seeded" == "4" ]] && [[ "$dry_skipped" == "1" ]] && [[ "$bao_calls" == "0" ]]; then
  printf '  PASS: test_dry_run_processes_all_no_bao_writes (total=5 seeded=4 skipped=1 bao_calls=0)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_processes_all_no_bao_writes (rc=%d total=%s seeded=%s skipped=%s bao_calls=%s)\n' "$rc" "$dry_total" "$dry_seeded" "$dry_skipped" "$bao_calls"
  sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 5: vaultBinaryOverride applied → tenant-operator GITEA_TOKEN
# resolves to api/secrets/gitea/token (NOT operators/secrets/gitea/token) ---
BAO_LOG="$WORKDIR/bao_override.log"
: > "$BAO_LOG"
out=$(mktemp); err=$(mktemp)
BAO_ADDR="https://mock:8200" BAO_TOKEN="mock_root" BAO_LOG="$BAO_LOG" PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" \
  --env vault-local \
  --secret-values-file "$WORKDIR/values_full.yaml" \
  --inventory "$WORKDIR/inventory.yaml" >"$out" 2>"$err"; rc=$?
# Two distinct paths should be written for gitea/token:
#   - gitea-admin (vaultBinary=api): secret/loopzero/vault-local/api/secrets/gitea/token
#   - loopzero-tenant-operator (override=api): same path written 2nd time (idempotent K-V2)
gitea_via_api=$(grep -c "kv put secret/loopzero/vault-local/api/secrets/gitea/token" "$BAO_LOG" || true)
gitea_via_operators=$(grep -c "kv put secret/loopzero/vault-local/operators/secrets/gitea/token" "$BAO_LOG" || true)
if [[ "$rc" == "0" ]] && [[ "$gitea_via_api" == "2" ]] && [[ "$gitea_via_operators" == "0" ]]; then
  printf '  PASS: test_vault_binary_override_resolves_to_api_path (api_path=2 operators_path=0)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_vault_binary_override_resolves_to_api_path (rc=%d api=%s ops=%s)\n' "$rc" "$gitea_via_api" "$gitea_via_operators"
  sed 's/^/    [bao_log] /' "$BAO_LOG"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 6: missing value → SKIP (not failure) ---
BAO_LOG="$WORKDIR/bao_skip.log"
: > "$BAO_LOG"
out=$(mktemp); err=$(mktemp)
BAO_ADDR="https://mock:8200" BAO_TOKEN="mock_root" BAO_LOG="$BAO_LOG" PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" \
  --env vault-local \
  --secret-values-file "$WORKDIR/values.yaml" \
  --inventory "$WORKDIR/inventory.yaml" >"$out" 2>"$err"; rc=$?
# spaces/access_key_id absent → 1 SKIP, exit 0 (not 2)
skipped_count=$(grep -oE "skipped=[0-9]+" "$err" | head -1 | cut -d= -f2)
failed_count=$(grep -oE "failed=[0-9]+" "$err" | head -1 | cut -d= -f2)
if [[ "$rc" == "0" ]] && [[ "$skipped_count" == "1" ]] && [[ "$failed_count" == "0" ]]; then
  printf '  PASS: test_missing_value_skipped_not_failed (skipped=1 failed=0 rc=0)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_value_skipped_not_failed (rc=%d skipped=%s failed=%s)\n' "$rc" "$skipped_count" "$failed_count"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 7: idempotency — same values 2x produces same paths ---
BAO_LOG1="$WORKDIR/bao_run1.log"
BAO_LOG2="$WORKDIR/bao_run2.log"
: > "$BAO_LOG1"
: > "$BAO_LOG2"
out=$(mktemp); err=$(mktemp)
BAO_ADDR="https://mock:8200" BAO_TOKEN="mock_root" BAO_LOG="$BAO_LOG1" PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local \
  --secret-values-file "$WORKDIR/values_full.yaml" \
  --inventory "$WORKDIR/inventory.yaml" >"$out" 2>"$err"
BAO_ADDR="https://mock:8200" BAO_TOKEN="mock_root" BAO_LOG="$BAO_LOG2" PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local \
  --secret-values-file "$WORKDIR/values_full.yaml" \
  --inventory "$WORKDIR/inventory.yaml" >"$out" 2>"$err"
# Extract paths (strip value=... payload which may differ in mock log) + sort
sed -E 's/value=[^ ]+//' "$BAO_LOG1" | sort > "$WORKDIR/run1_paths"
sed -E 's/value=[^ ]+//' "$BAO_LOG2" | sort > "$WORKDIR/run2_paths"
if diff -q "$WORKDIR/run1_paths" "$WORKDIR/run2_paths" >/dev/null; then
  printf '  PASS: test_idempotent_same_inventory_same_paths\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_idempotent_same_inventory_same_paths\n'
  diff "$WORKDIR/run1_paths" "$WORKDIR/run2_paths" | sed 's/^/    /'
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 8: bao kv put fails → counted in failed, exit 2 ---
BAO_LOG="$WORKDIR/bao_fail.log"
: > "$BAO_LOG"
out=$(mktemp); err=$(mktemp)
BAO_ADDR="https://mock:8200" BAO_TOKEN="mock_root" BAO_LOG="$BAO_LOG" \
  MOCK_BAO_EXIT=1 PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local \
  --secret-values-file "$WORKDIR/values_full.yaml" \
  --inventory "$WORKDIR/inventory.yaml" >"$out" 2>"$err"; rc=$?
failed_count=$(grep -oE "failed=[0-9]+" "$err" | head -1 | cut -d= -f2)
if [[ "$rc" == "2" ]] && [[ "$failed_count" == "5" ]]; then
  printf '  PASS: test_bao_failure_exits_2_counts_failed (failed=5 rc=2)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_bao_failure_exits_2_counts_failed (rc=%d failed=%s)\n' "$rc" "$failed_count"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 9 (SECURITY INVARIANT): dry-run does NOT leak raw secret values ---
# Per plan v1.4 EC-7 + pkg/quality/secretsnologs: scripts handling secrets MUST
# NEVER print raw values to stdout/stderr. seed-loopzero-secrets.sh dry-run emits
# `<${#secret_value} chars>` instead of literal value — this test asserts the
# values defined in fixture (e.g. "ghp_test_gitea_token") NEVER appear.
BAO_LOG="$WORKDIR/bao_noleak.log"
: > "$BAO_LOG"
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN BAO_LOG="$BAO_LOG" PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --env vault-local \
  --secret-values-file "$WORKDIR/values_full.yaml" \
  --inventory "$WORKDIR/inventory.yaml" \
  --dry-run >"$out" 2>"$err"; rc=$?
# Fixture secrets that MUST NOT appear in script output
leaked=0
for secret in "ghp_test_gitea_token" "sk_test_stripe" "dop_v1_test" "spaces_test_key"; do
  if grep -qF "$secret" "$out" "$err"; then
    leaked=1
    printf '    LEAK: %s appeared in script output\n' "$secret"
  fi
done
if [[ "$rc" == "0" ]] && [[ "$leaked" == "0" ]]; then
  printf '  PASS: test_dry_run_no_raw_secret_leak (4 fixtures verified absent)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_no_raw_secret_leak (rc=%d leaked=%s)\n' "$rc" "$leaked"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
