#!/usr/bin/env bash
# check-enable-vault-backup_test.sh — Hermetic test for iter 14 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8 (Runbook P4 codification).
#
# Validates infra/scripts/openbao/enable-vault-backup.sh:
#   1. Pre-flight: missing --env → fail
#   2. Pre-flight: missing creds file → fail
#   3. Pre-flight: kubectl missing → fail (impossible to mock, skip)
#   4. Pre-flight: creds file missing OPENBAO_BACKUP_ACCESS_KEY_ID → fail
#   5. Pre-flight: creds file missing OPENBAO_BACKUP_SECRET_ACCESS_KEY → fail
#   6. Dry-run: prints planned actions for 3 steps + DRY-RUN COMPLETE
#   7. --skip-helmfile: Step 2 SKIPPED message + Step 3 SKIP verification
#   8. Happy path: Secret apply + helmfile sync + verify → exit 0
#   9. kubectl apply fails → exit 2 "Secret apply failed"
#  10. helmfile sync fails → exit 2 "snapshotAgentEnabled"
#
# Mocks: kubectl + helmfile via PATH override.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/infra"
TARGET_SCRIPT="$REPO_ROOT/scripts/openbao/enable-vault-backup.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$WORKDIR" "$MOCK_BIN"' EXIT

# Fixture creds file with both keys (happy path default)
cat > "$WORKDIR/creds.env" <<'EOF'
OPENBAO_BACKUP_ACCESS_KEY_ID="DOTEST_ACCESS_KEY"
OPENBAO_BACKUP_SECRET_ACCESS_KEY="dotest+secret+access+key+xxxxx"
EOF
chmod 600 "$WORKDIR/creds.env"

# Fixture creds file missing the access key id
cat > "$WORKDIR/creds_no_access.env" <<'EOF'
OPENBAO_BACKUP_SECRET_ACCESS_KEY="dotest+secret+access+key+xxxxx"
EOF
chmod 600 "$WORKDIR/creds_no_access.env"

# Fixture creds file missing the secret access key
cat > "$WORKDIR/creds_no_secret.env" <<'EOF'
OPENBAO_BACKUP_ACCESS_KEY_ID="DOTEST_ACCESS_KEY"
EOF
chmod 600 "$WORKDIR/creds_no_secret.env"

# Fixture helmfile (just needs to exist; not actually parsed)
touch "$WORKDIR/secrets.helmfile.yaml.gotmpl"

# Mock kubectl
cat > "$MOCK_BIN/kubectl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *"version --request-timeout"*) exit 0 ;;
  *"create secret"*"--dry-run=client"*)
    echo "apiVersion: v1
kind: Secret
metadata:
  name: openbao-snapshot-s3-creds"
    exit 0
    ;;
  *"apply -f -"*)
    # Drain stdin: the script pipes `kubectl create secret ... --dry-run -o yaml
    # | kubectl apply -f -`. If apply exits without consuming stdin, the upstream
    # create-secret gets SIGPIPE → with the script's set -o pipefail the pipeline
    # fails flakily ("Secret apply failed"). Real kubectl reads stdin.
    cat >/dev/null 2>&1 || true
    exit "${MOCK_APPLY_EXIT:-0}"
    ;;
  *"get cronjob"*)
    echo "${MOCK_CRONJOB_SCHEDULE:-0 * * * *}"
    exit 0
    ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$MOCK_BIN/kubectl"

# Mock helmfile
cat > "$MOCK_BIN/helmfile" <<'EOF'
#!/usr/bin/env bash
exit "${MOCK_HELMFILE_EXIT:-0}"
EOF
chmod +x "$MOCK_BIN/helmfile"

run() {
  # Args: <name> <expect_exit> <expect_pattern> <env_assigns> <extra_flags...>
  local name="$1" expect_exit="$2" expect_pattern="$3" env_assigns="$4"
  shift 4
  local out err rc
  out=$(mktemp); err=$(mktemp)
  (
    if [ -n "$env_assigns" ]; then export $env_assigns; fi
    PATH="$MOCK_BIN:$PATH" SKIP_PREFLIGHT=0 bash "$TARGET_SCRIPT" \
      --env vault-local \
      --creds-file "$WORKDIR/creds.env" \
      --vault-context k3d-mock \
      --helmfile-path "$WORKDIR/secrets.helmfile.yaml.gotmpl" "$@"
  ) >"$out" 2>"$err"
  rc=$?
  local ok=1
  if [[ "$expect_exit" != "$rc" ]]; then ok=0; fi
  if [[ -n "$expect_pattern" ]] && ! grep -qE "$expect_pattern" "$out" "$err"; then ok=0; fi
  if [[ $ok -eq 1 ]]; then
    printf '  PASS: %s\n' "$name"; PASS=$((PASS+1))
  else
    printf '  FAIL: %s (rc=%d, expected=%s)\n' "$name" "$rc" "$expect_exit"
    sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
    FAIL=$((FAIL+1))
  fi
  rm -f "$out" "$err"
}

echo "Running enable-vault-backup hermetic tests..."
echo

# --- Test 1: missing --env arg → usage error ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "env required" "$err"; then
  printf '  PASS: test_missing_env_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_env_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 2: missing creds file → exit 1 ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --env vault-local --creds-file "$WORKDIR/nonexistent.env" \
  --helmfile-path "$WORKDIR/secrets.helmfile.yaml.gotmpl" \
  --vault-context k3d-mock >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "creds file not found" "$err"; then
  printf '  PASS: test_missing_creds_file_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_creds_file_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 3: creds file missing ACCESS_KEY_ID → exit 1 ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --env vault-local --creds-file "$WORKDIR/creds_no_access.env" \
  --helmfile-path "$WORKDIR/secrets.helmfile.yaml.gotmpl" \
  --vault-context k3d-mock >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "OPENBAO_BACKUP_ACCESS_KEY_ID not set" "$err"; then
  printf '  PASS: test_creds_missing_access_key_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_creds_missing_access_key_fails (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 4: creds file missing SECRET_ACCESS_KEY → exit 1 ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --env vault-local --creds-file "$WORKDIR/creds_no_secret.env" \
  --helmfile-path "$WORKDIR/secrets.helmfile.yaml.gotmpl" \
  --vault-context k3d-mock >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "OPENBAO_BACKUP_SECRET_ACCESS_KEY not set" "$err"; then
  printf '  PASS: test_creds_missing_secret_key_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_creds_missing_secret_key_fails (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 5: dry-run prints 3 steps + COMPLETE ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" SKIP_PREFLIGHT=1 bash "$TARGET_SCRIPT" \
  --env vault-local --creds-file "$WORKDIR/creds.env" \
  --vault-context k3d-mock --helmfile-path "$WORKDIR/secrets.helmfile.yaml.gotmpl" \
  --dry-run >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] \
   && grep -q "STEP 1/3" "$err" \
   && grep -q "STEP 2/3" "$err" \
   && grep -q "STEP 3/3" "$err" \
   && grep -q "DRY-RUN COMPLETE" "$err"; then
  printf '  PASS: test_dry_run_prints_3_steps\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_prints_3_steps (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 6: dry-run does NOT emit raw secret values (security invariant) ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" SKIP_PREFLIGHT=1 bash "$TARGET_SCRIPT" \
  --env vault-local --creds-file "$WORKDIR/creds.env" \
  --vault-context k3d-mock --helmfile-path "$WORKDIR/secrets.helmfile.yaml.gotmpl" \
  --dry-run >"$out" 2>"$err"; rc=$?
# Must NOT contain literal access key value or secret value
if [[ "$rc" == "0" ]] \
   && ! grep -q "DOTEST_ACCESS_KEY" "$err" "$out" \
   && ! grep -q "dotest+secret+access+key" "$err" "$out"; then
  printf '  PASS: test_dry_run_no_raw_secret_leak\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_no_raw_secret_leak — RAW SECRET VALUE LEAKED\n'
  sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 7: --skip-helmfile gives Step 2 SKIPPED + Step 3 SKIP ---
run "test_skip_helmfile_step_2_skipped" "0" "SKIPPED" "" --skip-helmfile

# --- Test 8: happy path → exit 0 + COMPLETE ---
run "test_happy_path_exit_0" "0" "Vault backup enabled" ""

# --- Test 9: kubectl apply fails → exit 2 ---
run "test_kubectl_apply_failure_exit_2" "2" "Secret apply failed" \
  "MOCK_APPLY_EXIT=1"

# --- Test 10: helmfile sync fails → exit 2 + snapshotAgentEnabled hint ---
run "test_helmfile_failure_exit_2_with_hint" "2" "snapshotAgentEnabled" \
  "MOCK_HELMFILE_EXIT=1"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
