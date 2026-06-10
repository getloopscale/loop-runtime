#!/usr/bin/env bash
# T4.6 hermetic test of infra/scripts/security/rekor-archive-old-entries.sh.
# Pins the --manual-procedure-only flag behavior + the no-flag exit-1
# error path. No network, no external binaries beyond bash.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/security/rekor-archive-old-entries.sh"

# Support being invoked from repo-root cwd or from infra/.
if [[ ! -f "$TARGET_SCRIPT" ]]; then
  ALT="$(cd "$SCRIPT_DIR/../.." && pwd)/infra/scripts/security/rekor-archive-old-entries.sh"
  if [[ -f "$ALT" ]]; then
    TARGET_SCRIPT="$ALT"
  else
    echo "FAIL: rekor-archive-old-entries.sh missing at $TARGET_SCRIPT" >&2
    exit 1
  fi
fi

PASS=0
FAIL=0
TESTS=()

assert_exit() {
  local label="$1" want_exit="$2" got_exit="$3"
  if [[ "$want_exit" == "$got_exit" ]]; then
    PASS=$((PASS + 1))
    TESTS+=("PASS  $label (exit $got_exit)")
  else
    FAIL=$((FAIL + 1))
    TESTS+=("FAIL  $label (want exit $want_exit, got $got_exit)")
  fi
}

assert_contains() {
  local label="$1" needle="$2" haystack="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then
    PASS=$((PASS + 1))
    TESTS+=("PASS  $label")
  else
    FAIL=$((FAIL + 1))
    TESTS+=("FAIL  $label (missing literal '$needle')")
  fi
}

assert_not_contains() {
  local label="$1" needle="$2" haystack="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then
    FAIL=$((FAIL + 1))
    TESTS+=("FAIL  $label (unexpected literal '$needle')")
  else
    PASS=$((PASS + 1))
    TESTS+=("PASS  $label")
  fi
}

# 1. Syntax check ------------------------------------------------------------
if bash -n "$TARGET_SCRIPT"; then
  PASS=$((PASS + 1))
  TESTS+=("PASS  test_rekor_archive_syntax_valid")
else
  FAIL=$((FAIL + 1))
  TESTS+=("FAIL  test_rekor_archive_syntax_valid")
fi

# 2. --manual-procedure-only → exit 0 + WARN line ---------------------------
set +e
out=$(bash "$TARGET_SCRIPT" --manual-procedure-only 2>&1)
exit_code=$?
set -e
assert_exit "test_rekor_archive_manual_procedure_flag_exits_0" 0 "$exit_code"
assert_contains "test_rekor_archive_manual_procedure_flag_emits_WARN" "WARN" "$out"
assert_contains "test_rekor_archive_manual_procedure_flag_references_runbook" \
  "docs/operations/rekor-retention.md" "$out"
assert_contains "test_rekor_archive_manual_procedure_flag_references_T4_6" \
  "T4.6" "$out"

# 3. --help → exit 0 + usage ------------------------------------------------
set +e
out=$(bash "$TARGET_SCRIPT" --help 2>&1)
exit_code=$?
set -e
assert_exit "test_rekor_archive_help_exits_0" 0 "$exit_code"
assert_contains "test_rekor_archive_help_shows_manual_procedure_flag" \
  "--manual-procedure-only" "$out"

# 4. No flag + missing env → pre-flight error (exit 1) ---------------------
# The script's pre-flight requires REKOR_API_URL + S3_ARCHIVE_BUCKET.
# Without --manual-procedure-only AND without those env vars, exit 1.
set +e
out=$(env -u REKOR_API_URL -u S3_ARCHIVE_BUCKET DRY_RUN=0 \
  bash "$TARGET_SCRIPT" 2>&1)
exit_code=$?
set -e
assert_exit "test_rekor_archive_no_flag_missing_env_exits_1" 1 "$exit_code"
assert_contains "test_rekor_archive_no_flag_missing_env_error_message" \
  "REKOR_API_URL not set" "$out"

# 5. No flag + env set but stub branch → exit 1 (operator must opt in) -----
# When env is set, pre-flight passes, the stub branch fires. T4.6
# changed this from silent exit 0 to LOUD exit 1 so a future
# scheduler invocation cannot silently no-op.
PATH_PREFIX="/tmp/rekor-test-$$"
mkdir -p "$PATH_PREFIX"
for mock in rekor-cli aws jq; do
  cat >"$PATH_PREFIX/$mock" <<'MOCK'
#!/usr/bin/env bash
exit 0
MOCK
  chmod +x "$PATH_PREFIX/$mock"
done
set +e
out=$(REKOR_API_URL=http://rekor.test S3_ARCHIVE_BUCKET=mybucket \
  PATH="$PATH_PREFIX:$PATH" bash "$TARGET_SCRIPT" 2>&1)
exit_code=$?
set -e
rm -rf "$PATH_PREFIX"
assert_exit "test_rekor_archive_no_flag_with_env_exits_1_loud" 1 "$exit_code"
assert_contains "test_rekor_archive_no_flag_error_mentions_CR_11" "CR-11" "$out"
assert_not_contains "test_rekor_archive_no_flag_did_not_silently_succeed" "[stub]" "$out"

# 6. DRY_RUN=1 → exit 0 without external binaries (smoke) -----------------
set +e
out=$(DRY_RUN=1 REKOR_API_URL=http://rekor.test S3_ARCHIVE_BUCKET=mybucket \
  bash "$TARGET_SCRIPT" 2>&1)
exit_code=$?
set -e
assert_exit "test_rekor_archive_dry_run_exits_0" 0 "$exit_code"
assert_contains "test_rekor_archive_dry_run_describes_actions" "[dry-run]" "$out"

# Report --------------------------------------------------------------------
echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Rekor archival — hermetic test"
echo "═══════════════════════════════════════════════════════════"
printf '%s\n' "${TESTS[@]}"
echo "-----------------------------------------------------------"
echo "  Results: $PASS PASS / $FAIL FAIL"
echo "═══════════════════════════════════════════════════════════"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
