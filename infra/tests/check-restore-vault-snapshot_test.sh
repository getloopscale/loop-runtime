#!/usr/bin/env bash
# check-restore-vault-snapshot_test.sh — Hermetic test for iter 16 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8 (Runbook P5 DR codification).
#
# CRITICAL: validates "dry-run by default" safety pattern. The most important
# invariant for this DR script is: without --execute, NO destructive operation
# runs (no kubectl cp, no kubectl exec bao restore). Tests:
#
#   1. Pre-flight: missing --env → fail
#   2. Pre-flight: missing AWS_ACCESS_KEY_ID → fail
#   3. Pre-flight: missing AWS_SECRET_ACCESS_KEY → fail
#   4. Pre-flight: must provide --list OR --snapshot-key (not both, not neither)
#   5. **SAFETY**: default mode (no --execute) does NOT call kubectl exec
#      "bao operator raft snapshot restore"
#   6. **SAFETY**: default mode does NOT call kubectl cp
#   7. --list mode prints snapshot listing from aws s3 ls
#   8. --execute flag actually triggers kubectl exec + cp paths
#   9. Snapshot not found em bucket → exit 2 "snapshot not found"
#  10. Post-restore COMPLETE message includes mandatory unseal hint

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/openbao/restore-vault-snapshot.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$WORKDIR" "$MOCK_BIN"' EXIT

# Mock aws CLI
cat > "$MOCK_BIN/aws" <<'EOF'
#!/usr/bin/env bash
echo "AWS_CALL: $*" >> "${AWS_LOG:-/dev/null}"
case "$*" in
  *"s3 ls"*)
    # List mode OR existence check
    if [ "${MOCK_AWS_LS_EXIT:-0}" != "0" ]; then exit "${MOCK_AWS_LS_EXIT}"; fi
    # Default: 5 snapshots
    cat <<LISTING
2026-06-03 01:00:00      1024 2026-06-03T01-00-00Z.snap
2026-06-02 01:00:00      1024 2026-06-02T01-00-00Z.snap
2026-06-01 01:00:00      1024 2026-06-01T01-00-00Z.snap
2026-05-31 01:00:00      1024 2026-05-31T01-00-00Z.snap
2026-05-30 01:00:00      1024 2026-05-30T01-00-00Z.snap
LISTING
    exit 0
    ;;
  *"s3 cp"*)
    # Simulate downloading by touching the target
    target=$(echo "$*" | awk '{print $NF}')
    echo "fake-snapshot-content-bytes" > "$target"
    exit "${MOCK_AWS_CP_EXIT:-0}"
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$MOCK_BIN/aws"

# Mock kubectl
cat > "$MOCK_BIN/kubectl" <<'EOF'
#!/usr/bin/env bash
echo "KUBECTL_CALL: $*" >> "${KUBECTL_LOG:-/dev/null}"
case "$*" in
  *"version --request-timeout"*) exit 0 ;;
  *"cp "*) exit "${MOCK_KUBECTL_CP_EXIT:-0}" ;;
  *"exec "*"bao operator raft snapshot restore"*)
    exit "${MOCK_BAO_RESTORE_EXIT:-0}"
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$MOCK_BIN/kubectl"

# Common run helper
run_default_mode() {
  # No --execute; default safety mode
  local out=$(mktemp) err=$(mktemp)
  local rc
  PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
    KUBECTL_LOG="$WORKDIR/kubectl_$1.log" AWS_LOG="$WORKDIR/aws_$1.log" \
    bash "$TARGET_SCRIPT" \
    --env vault-local \
    --snapshot-key 2026-06-01T01-00-00Z.snap \
    >"$out" 2>"$err"
  rc=$?
  echo "$rc"
  cat "$out" "$err"
  rm -f "$out" "$err"
}

echo "Running restore-vault-snapshot hermetic tests..."
echo

# --- Test 1: missing --env arg ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t bash "$TARGET_SCRIPT" --list >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "env required" "$err"; then
  printf '  PASS: test_missing_env_fails\n'; PASS=$((PASS+1))
else printf '  FAIL: test_missing_env_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1)); fi
rm -f "$out" "$err"

# --- Test 2: missing AWS_ACCESS_KEY_ID ---
out=$(mktemp); err=$(mktemp)
env -u AWS_ACCESS_KEY_ID PATH="$MOCK_BIN:$PATH" AWS_SECRET_ACCESS_KEY=t bash "$TARGET_SCRIPT" \
  --env vault-local --list >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "AWS_ACCESS_KEY_ID" "$err"; then
  printf '  PASS: test_missing_aws_access_key_fails\n'; PASS=$((PASS+1))
else printf '  FAIL: test_missing_aws_access_key_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1)); fi
rm -f "$out" "$err"

# --- Test 3: must provide --list OR --snapshot-key (neither) ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t bash "$TARGET_SCRIPT" \
  --env vault-local >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "must provide --list OR --snapshot-key" "$err"; then
  printf '  PASS: test_neither_list_nor_snapshot_key_fails\n'; PASS=$((PASS+1))
else printf '  FAIL: test_neither_list_nor_snapshot_key_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1)); fi
rm -f "$out" "$err"

# --- Test 4: must provide --list OR --snapshot-key (BOTH = mutually exclusive) ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t bash "$TARGET_SCRIPT" \
  --env vault-local --list --snapshot-key foo.snap >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "mutually exclusive" "$err"; then
  printf '  PASS: test_both_list_and_snapshot_key_mutually_exclusive\n'; PASS=$((PASS+1))
else printf '  FAIL: test_both_list_and_snapshot_key_mutually_exclusive (rc=%d)\n' "$rc"; FAIL=$((FAIL+1)); fi
rm -f "$out" "$err"

# --- Test 5 (SAFETY CRITICAL): default mode (NO --execute) MUST NOT exec restore ---
KUBECTL_LOG="$WORKDIR/kubectl_safety.log"
: > "$KUBECTL_LOG"
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t \
  KUBECTL_LOG="$KUBECTL_LOG" bash "$TARGET_SCRIPT" \
  --env vault-local --snapshot-key 2026-06-01T01-00-00Z.snap \
  >"$out" 2>"$err"; rc=$?
restore_calls=$(grep -c "bao operator raft snapshot restore" "$KUBECTL_LOG" 2>/dev/null) || restore_calls=0
cp_calls=$(grep -c "cp " "$KUBECTL_LOG" 2>/dev/null) || cp_calls=0
if [[ "$rc" == "0" ]] && [[ "$restore_calls" -eq 0 ]] && [[ "$cp_calls" -eq 0 ]] && grep -q "DRY-RUN MODE" "$err"; then
  printf '  PASS: test_default_mode_no_destructive_ops (restore=0 cp=0)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_default_mode_no_destructive_ops (rc=%d restore=%s cp=%s)\n' "$rc" "$restore_calls" "$cp_calls"
  sed 's/^/    [kubectl_log] /' "$KUBECTL_LOG"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 6: --list mode prints snapshot listing ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t bash "$TARGET_SCRIPT" \
  --env vault-local --list >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] \
   && grep -q "2026-06-03T01-00-00Z.snap" "$out" \
   && grep -q "2026-05-30T01-00-00Z.snap" "$out"; then
  printf '  PASS: test_list_mode_prints_snapshots\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_list_mode_prints_snapshots (rc=%d)\n' "$rc"
  sed 's/^/    [stdout] /' "$out"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 7: --execute mode DOES call kubectl cp + bao restore ---
KUBECTL_LOG="$WORKDIR/kubectl_execute.log"
: > "$KUBECTL_LOG"
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t \
  KUBECTL_LOG="$KUBECTL_LOG" bash "$TARGET_SCRIPT" \
  --env vault-local --snapshot-key 2026-06-01T01-00-00Z.snap --execute \
  >"$out" 2>"$err"; rc=$?
restore_calls=$(grep -c "bao operator raft snapshot restore" "$KUBECTL_LOG" 2>/dev/null) || restore_calls=0
cp_calls=$(grep -c "cp " "$KUBECTL_LOG" 2>/dev/null) || cp_calls=0
if [[ "$rc" == "0" ]] && [[ "$restore_calls" -ge 1 ]] && [[ "$cp_calls" -ge 1 ]]; then
  printf '  PASS: test_execute_mode_runs_destructive_ops (restore=%s cp=%s)\n' "$restore_calls" "$cp_calls"; PASS=$((PASS+1))
else
  printf '  FAIL: test_execute_mode_runs_destructive_ops (rc=%d restore=%s cp=%s)\n' "$rc" "$restore_calls" "$cp_calls"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 8: snapshot not found em bucket → exit 2 ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t \
  MOCK_AWS_LS_EXIT=1 bash "$TARGET_SCRIPT" \
  --env vault-local --snapshot-key nonexistent.snap >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "2" ]] && grep -q "snapshot not found" "$err"; then
  printf '  PASS: test_snapshot_not_found_exit_2\n'; PASS=$((PASS+1))
else printf '  FAIL: test_snapshot_not_found_exit_2 (rc=%d)\n' "$rc"; FAIL=$((FAIL+1)); fi
rm -f "$out" "$err"

# --- Test 9: post-restore COMPLETE message includes unseal hint ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" AWS_ACCESS_KEY_ID=t AWS_SECRET_ACCESS_KEY=t bash "$TARGET_SCRIPT" \
  --env vault-local --snapshot-key 2026-06-01T01-00-00Z.snap --execute \
  >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] \
   && grep -q "Vault state restored" "$err" \
   && grep -q "Shamir unseal ceremony" "$err"; then
  printf '  PASS: test_post_restore_includes_unseal_hint\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_post_restore_includes_unseal_hint (rc=%d)\n' "$rc"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
