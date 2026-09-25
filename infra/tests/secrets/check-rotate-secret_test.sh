#!/usr/bin/env bash
# check-rotate-secret_test.sh — Hermetic test for T8.1 EC-22 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8.
#
# Validates infra/scripts/openbao/rotate-secret.sh:
#   1. Pre-flight: missing --env / --path / --value (non-dry-run) → error
#   2. Pre-flight: missing BAO_ADDR / BAO_TOKEN (non-dry-run) → error
#   3. Unknown path no inventory ExternalSecret reads → fail-fast (EC-22 typo guard)
#   4. Dry-run: prints planned actions for all 4 steps, no bao/kubectl writes
#   5. Path lookup: gitea/token resolves to BOTH loopzero-api + loopzero-tenant-operator
#      restart (cross-binary EC-22 atomic)
#   6. Path lookup: operators/secrets/digitalocean/token resolves to ONLY
#      loopzero-tenant-operator (single-binary path)
#   7. bao kv put fails → exit 2, no restart attempted (atomic abort)
#   8. rollout restart fails → exit 2, PARTIAL warning
#
# Mocks: bao + kubectl via PATH override + log files. Uses no external state.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/infra"
TARGET_SCRIPT="$REPO_ROOT/scripts/openbao/rotate-secret.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$WORKDIR" "$MOCK_BIN"' EXIT

# Mock bao — records every call to BAO_LOG, exit per MOCK_BAO_EXIT
cat > "$MOCK_BIN/bao" <<'EOF'
#!/usr/bin/env bash
echo "BAO_CALL: $*" >> "${BAO_LOG:-/dev/null}"
exit "${MOCK_BAO_EXIT:-0}"
EOF
chmod +x "$MOCK_BIN/bao"

# Mock kubectl — records every call to KUBECTL_LOG, exit per MOCK_KUBECTL_EXIT
# Special handling for `get externalsecret` (returns Ready=True by default).
cat > "$MOCK_BIN/kubectl" <<'EOF'
#!/usr/bin/env bash
echo "KUBECTL_CALL: $*" >> "${KUBECTL_LOG:-/dev/null}"
case "$*" in
  *"get externalsecret"*"jsonpath"*)
    echo "${MOCK_ES_STATUS:-True}"
    exit 0
    ;;
  *"rollout restart deployment"*)
    exit "${MOCK_RESTART_EXIT:-0}"
    ;;
  *"rollout status deployment"*)
    exit "${MOCK_STATUS_EXIT:-0}"
    ;;
  *)
    exit "${MOCK_KUBECTL_EXIT:-0}"
    ;;
esac
EOF
chmod +x "$MOCK_BIN/kubectl"

# Mock jq (uses real if available, else stub)
if ! command -v jq >/dev/null 2>&1; then
  cat > "$MOCK_BIN/jq" <<'EOF'
#!/usr/bin/env bash
echo "mock-jq"
EOF
  chmod +x "$MOCK_BIN/jq"
fi

assert() {
  local name="$1" cond="$2"
  if eval "$cond"; then
    printf '  PASS: %s\n' "$name"; PASS=$((PASS+1))
  else
    printf '  FAIL: %s (cond: %s)\n' "$name" "$cond"; FAIL=$((FAIL+1))
  fi
}

echo "Running rotate-secret hermetic tests..."
echo

# --- Test 1: missing --env arg → usage error ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --path api/secrets/gitea/token --value secret >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "env required" "$err"; then
  printf '  PASS: test_missing_env_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_env_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 2: missing --value (non-dry-run) → error ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "value required" "$err"; then
  printf '  PASS: test_missing_value_non_dry_run_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_value_non_dry_run_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 3: missing BAO_ADDR (non-dry-run) → preflight err ---
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token --value secret >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "BAO_ADDR env var required" "$err"; then
  printf '  PASS: test_missing_bao_addr_non_dry_run_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_bao_addr_non_dry_run_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 4: unknown path no ExternalSecret reads → fail-fast (EC-22 guard) ---
out=$(mktemp); err=$(mktemp)
BAO_ADDR=mock BAO_TOKEN=mock PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/typo/typoed --value secret >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "no ExternalSecret in .* reads 'api/secrets/typo/typoed'" "$err"; then
  printf '  PASS: test_unknown_path_fail_fast_ec22_guard\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_unknown_path_fail_fast_ec22_guard (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 5: dry-run prints 4 steps without bao/kubectl mutating calls ---
BAO_LOG="$WORKDIR/bao_dryrun.log"
KUBECTL_LOG="$WORKDIR/kubectl_dryrun.log"
: > "$BAO_LOG"
: > "$KUBECTL_LOG"
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN BAO_LOG="$BAO_LOG" KUBECTL_LOG="$KUBECTL_LOG" PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token --dry-run >"$out" 2>"$err"; rc=$?
bao_calls=$(wc -l < "$BAO_LOG")
kubectl_calls=$(wc -l < "$KUBECTL_LOG")
if [[ "$rc" == "0" ]] \
   && grep -q "STEP 1/4" "$err" \
   && grep -q "STEP 2/4" "$err" \
   && grep -q "STEP 3/4" "$err" \
   && grep -q "STEP 4/4" "$err" \
   && grep -q "DRY-RUN COMPLETE" "$err" \
   && [[ "$bao_calls" == "0" ]] \
   && [[ "$kubectl_calls" == "0" ]]; then
  printf '  PASS: test_dry_run_prints_4_steps_no_mutations (bao=0 kubectl=0)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_prints_4_steps_no_mutations (rc=%d bao=%s kubectl=%s)\n' "$rc" "$bao_calls" "$kubectl_calls"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 6: gitea/token → BOTH loopzero-api + loopzero-tenant-operator (cross-binary) ---
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token --dry-run >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] \
   && grep -q "binaries to restart: loopzero-api,loopzero-tenant-operator" "$err"; then
  printf '  PASS: test_gitea_token_cross_binary_lookup (loopzero-api,tenant-operator)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_gitea_token_cross_binary_lookup (rc=%d)\n' "$rc"
  grep "binaries" "$err" | sed 's/^/    /'
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 7: digitalocean/token → ONLY loopzero-tenant-operator (single binary) ---
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path operators/secrets/digitalocean/token --dry-run >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] \
   && grep -q "binaries to restart: loopzero-tenant-operator$" "$err"; then
  printf '  PASS: test_digitalocean_token_single_binary_lookup\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_digitalocean_token_single_binary_lookup (rc=%d)\n' "$rc"
  grep "binaries" "$err" | sed 's/^/    /'
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 8: bao kv put fails → exit 2, no rollout attempted (atomic abort) ---
BAO_LOG="$WORKDIR/bao_fail.log"
KUBECTL_LOG="$WORKDIR/kubectl_fail.log"
: > "$BAO_LOG"
: > "$KUBECTL_LOG"
out=$(mktemp); err=$(mktemp)
BAO_ADDR=mock BAO_TOKEN=mock BAO_LOG="$BAO_LOG" KUBECTL_LOG="$KUBECTL_LOG" \
  MOCK_BAO_EXIT=1 PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token --value secret >"$out" 2>"$err"; rc=$?
# Should exit 2; should NOT have called kubectl annotate/restart (Step 1 failed)
restart_calls=$(grep -c "rollout restart" "$KUBECTL_LOG" || true)
if [[ "$rc" == "2" ]] \
   && grep -q "rotation aborted" "$err" \
   && [[ "$restart_calls" == "0" ]]; then
  printf '  PASS: test_bao_failure_atomic_abort_no_restart (rc=2 restart_calls=0)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_bao_failure_atomic_abort_no_restart (rc=%d restart_calls=%s)\n' "$rc" "$restart_calls"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 9: rollout status fails → exit 2 + PARTIAL warning ---
BAO_LOG="$WORKDIR/bao_partial.log"
KUBECTL_LOG="$WORKDIR/kubectl_partial.log"
: > "$BAO_LOG"
: > "$KUBECTL_LOG"
out=$(mktemp); err=$(mktemp)
BAO_ADDR=mock BAO_TOKEN=mock BAO_LOG="$BAO_LOG" KUBECTL_LOG="$KUBECTL_LOG" \
  MOCK_STATUS_EXIT=1 PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token --value secret --skip-verify >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "2" ]] && grep -q "PARTIAL" "$err"; then
  printf '  PASS: test_rollout_status_failure_partial_exit_2\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_rollout_status_failure_partial_exit_2 (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 10 (SECURITY INVARIANT): dry-run does NOT leak raw --value secret ---
# Per plan v1.4 EC-7 + pkg/quality/secretsnologs: scripts handling secrets MUST
# NEVER print raw values. rotate-secret.sh dry-run emits `<${#VALUE} chars>`
# instead of literal — this test asserts the --value content NEVER appears in
# stdout/stderr (regression protection against debug-print additions).
SECRET_PROBE="REDACTED_SECRET_038"
out=$(mktemp); err=$(mktemp)
env -u BAO_ADDR -u BAO_TOKEN PATH="$MOCK_BIN:$PATH" \
  bash "$TARGET_SCRIPT" --env vault-local --path api/secrets/gitea/token \
  --value "$SECRET_PROBE" --dry-run >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] && ! grep -qF "$SECRET_PROBE" "$out" "$err"; then
  printf '  PASS: test_dry_run_no_raw_secret_leak\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_no_raw_secret_leak — RAW SECRET VALUE LEAKED IN OUTPUT\n'
  printf '  rc=%d; probe=%s\n' "$rc" "$SECRET_PROBE"
  sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
