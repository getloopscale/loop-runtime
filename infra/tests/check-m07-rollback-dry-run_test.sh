#!/usr/bin/env bash
# T4.5 hermetic test of infra/scripts/production/m07-cutover-rollback.sh.
# Mocks argocd / kubectl / helm / psql / pulumi via PATH override (no
# network, no kube cluster). 8 cases — same pattern as
# check-m07-cutover-preflight_test.sh / check-cosign-v3-deadline_test.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# infra/tests/<this>.sh → repo root is two ups.
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/infra/scripts/production/m07-cutover-rollback.sh"

if [[ ! -f "$TARGET_SCRIPT" ]]; then
  echo "FAIL: target script $TARGET_SCRIPT missing" >&2
  exit 1
fi

# Mock harness ---------------------------------------------------------------

MOCK_DIR=$(mktemp -d)
trap 'rm -rf "$MOCK_DIR"' EXIT

make_mock() {
  local name="$1" body="${2:-echo "mock-$name $*"}"
  cat >"$MOCK_DIR/$name" <<MOCK
#!/usr/bin/env bash
$body
MOCK
  chmod +x "$MOCK_DIR/$name"
}

# Default happy-path mocks. Tests can override individually.
make_mock argocd  'echo "mock-argocd $*"; exit 0'
make_mock kubectl 'echo "mock-kubectl $*"; exit 0'
make_mock helm    'echo "mock-helm $*"; exit 0'
make_mock psql    'echo "mock-psql $*"; exit 0'
make_mock pulumi  'echo "mock-pulumi $*"; exit 0'

export PATH="$MOCK_DIR:$PATH"

# Test helpers ---------------------------------------------------------------

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
  TESTS+=("PASS  test_rollback_script_syntax_valid")
else
  FAIL=$((FAIL + 1))
  TESTS+=("FAIL  test_rollback_script_syntax_valid")
fi

# 2. Missing flags ----------------------------------------------------------
set +e
out=$(bash "$TARGET_SCRIPT" 2>&1)
exit_code=$?
set -e
assert_exit "test_rollback_script_missing_flags_exits_1" 1 "$exit_code"
assert_contains "test_rollback_script_missing_flags_shows_usage" "Usage:" "$out"

# 3. --dry-run does NOT mutate (no side effects from mock binaries) ---------
set +e
out=$(bash "$TARGET_SCRIPT" --dry-run --target-rev 17 --build-rev 9 2>&1)
exit_code=$?
set -e
assert_exit "test_rollback_script_dry_run_exits_0" 0 "$exit_code"
assert_contains "test_rollback_script_dry_run_prints_DRY-RUN_lines" "DRY-RUN:" "$out"
# In dry-run mode the script must NOT invoke the mocks (which would
# echo "mock-<name>"). Absence of those echoes proves no mutation.
assert_not_contains "test_rollback_script_dry_run_no_mutations" "mock-argocd" "$out"
assert_not_contains "test_rollback_script_dry_run_no_helm_uninstall" "mock-helm" "$out"

# 4. --execute happy path invokes the rollback commands in order ------------
set +e
out=$(bash "$TARGET_SCRIPT" --execute --target-rev 17 --build-rev 9 --skip-step3 2>&1)
exit_code=$?
set -e
assert_exit "test_rollback_script_execute_happy_path_exits_0" 0 "$exit_code"
# Order assertion: argocd app rollback loopzero-api should appear BEFORE
# argocd app rollback loopzero-build-cwts.
api_line=$(grep -n "rollback loopzero-api" <<<"$out" | head -1 | cut -d: -f1 || true)
build_line=$(grep -n "rollback loopzero-build-cwts" <<<"$out" | head -1 | cut -d: -f1 || true)
if [[ -n "$api_line" && -n "$build_line" && "$api_line" -lt "$build_line" ]]; then
  PASS=$((PASS + 1))
  TESTS+=("PASS  test_rollback_script_argocd_rollback_called_in_order")
else
  FAIL=$((FAIL + 1))
  TESTS+=("FAIL  test_rollback_script_argocd_rollback_called_in_order (api_line=$api_line build_line=$build_line)")
fi
assert_contains "test_rollback_script_execute_skips_step3_with_flag" "Step 3 SKIPPED" "$out"
assert_contains "test_rollback_script_execute_skips_step4_by_default" "Step 4 SKIPPED" "$out"

# 5. Audit marker on --emit-audit -------------------------------------------
set +e
out=$(bash "$TARGET_SCRIPT" --execute --target-rev 17 --build-rev 9 --skip-step3 --emit-audit 2>&1)
exit_code=$?
set -e
assert_exit "test_rollback_script_emit_audit_exits_0" 0 "$exit_code"
assert_contains "test_rollback_script_emit_audit_runs_psql" "release.cutover_rollback" "$out"

# 6. argocd rollback failure → exit 2 ---------------------------------------
# Override argocd to fail on the first rollback call.
cat >"$MOCK_DIR/argocd" <<'MOCK'
#!/usr/bin/env bash
if [[ "$1" == "app" && "$2" == "rollback" ]]; then
  echo "mock-argocd rollback FAILED for $3" >&2
  exit 1
fi
echo "mock-argocd $*"
exit 0
MOCK
chmod +x "$MOCK_DIR/argocd"

set +e
out=$(bash "$TARGET_SCRIPT" --execute --target-rev 17 --build-rev 9 --skip-step3 2>&1)
exit_code=$?
set -e
assert_exit "test_rollback_script_argocd_rollback_failure_exits_2" 2 "$exit_code"
assert_contains "test_rollback_script_argocd_failure_emits_escalation_message" "escalate to Platform Lead" "$out"

# 7. Both --dry-run and --execute → exit 1 (mutually exclusive) -------------
# Restore happy-path argocd before the next case.
make_mock argocd 'echo "mock-argocd $*"; exit 0'

set +e
out=$(bash "$TARGET_SCRIPT" --dry-run --execute --target-rev 17 --build-rev 9 2>&1)
exit_code=$?
set -e
assert_exit "test_rollback_script_dry_run_execute_mutually_exclusive_exits_1" 1 "$exit_code"
assert_contains "test_rollback_script_mutually_exclusive_error_message" "mutually exclusive" "$out"

# Report --------------------------------------------------------------------
echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  M07 cutover rollback — hermetic test"
echo "═══════════════════════════════════════════════════════════"
printf '%s\n' "${TESTS[@]}"
echo "-----------------------------------------------------------"
echo "  Results: $PASS PASS / $FAIL FAIL"
echo "═══════════════════════════════════════════════════════════"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
