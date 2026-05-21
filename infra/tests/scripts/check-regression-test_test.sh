#!/usr/bin/env bash
# Hermetic test for check-regression-test.sh
# Plan 2026-05-21 v1.1 T5.2 + EC-3.

set -euo pipefail

SCRIPT="${SCRIPT:-infra/scripts/quality/check-regression-test.sh}"
TESTS_PASSED=0; TESTS_FAILED=0; FAIL_DETAILS=()
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

fail() { TESTS_FAILED=$((TESTS_FAILED + 1)); FAIL_DETAILS+=("FAIL: $1"); echo "FAIL: $1" >&2; }
pass() { TESTS_PASSED=$((TESTS_PASSED + 1)); echo "PASS: $1"; }

test_script_exists() {
  [ -x "$REPO_ROOT/$SCRIPT" ] && pass "test_script_exists" || fail "test_script_exists"
}

test_regression_gate_skips_typo_only_PRs() {
  # PR with no "fix #N" refs anywhere
  if PR_TITLE="docs: fix typo" PR_BODY="" PR_COMMITS="" PR_DIFF_FILES="README.md" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_regression_gate_skips_typo_only_PRs"
  else
    fail "test_regression_gate_skips_typo_only_PRs"
  fi
}

test_regression_gate_fails_on_fix_without_test() {
  # PR title has "fix #123" but no test file in diff
  if PR_TITLE="fix #123 wrong port" PR_BODY="" PR_COMMITS="" \
       PR_DIFF_FILES="api/internal/services/builds.go" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    fail "test_regression_gate_fails_on_fix_without_test"
  else
    pass "test_regression_gate_fails_on_fix_without_test"
  fi
}

test_regression_gate_passes_on_fix_with_test() {
  if PR_TITLE="fix #123 wrong port" PR_BODY="" PR_COMMITS="" \
       PR_DIFF_FILES="api/internal/services/builds.go
api/internal/services/builds_test.go" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_regression_gate_passes_on_fix_with_test"
  else
    fail "test_regression_gate_passes_on_fix_with_test"
  fi
}

# EC-3: PR title parsed even if commits/body don't have it (squash-merge case)
test_regression_gate_reads_PR_title() {
  # Title only, body + commits empty
  if PR_TITLE="Fix #456: wrong port" PR_BODY="" PR_COMMITS="" \
       PR_DIFF_FILES="api/services.go" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    fail "test_regression_gate_reads_PR_title (EC-3): should FAIL (no test)"
  else
    pass "test_regression_gate_reads_PR_title (EC-3)"
  fi
}

# EC-3: PR body parsed when title is generic
test_regression_gate_reads_PR_body() {
  if PR_TITLE="Update build pipeline" PR_BODY="Fixes #789" PR_COMMITS="" \
       PR_DIFF_FILES="api/services.go" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    fail "test_regression_gate_reads_PR_body (EC-3): should FAIL (no test)"
  else
    pass "test_regression_gate_reads_PR_body (EC-3)"
  fi
}

# EC-3: multiple fix refs aggregated
test_regression_gate_aggregates_multiple_fix_refs() {
  if PR_TITLE="Fix #100 and resolves #200" PR_BODY="" PR_COMMITS="" \
       PR_DIFF_FILES="api/services.go
api/services_test.go" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_regression_gate_aggregates_multiple_fix_refs (EC-3)"
  else
    fail "test_regression_gate_aggregates_multiple_fix_refs (EC-3): test file present but failed"
  fi
}

echo "[check-regression-test_test] Running"
echo "----------------------------------------"
test_script_exists
test_regression_gate_skips_typo_only_PRs
test_regression_gate_fails_on_fix_without_test
test_regression_gate_passes_on_fix_with_test
test_regression_gate_reads_PR_title
test_regression_gate_reads_PR_body
test_regression_gate_aggregates_multiple_fix_refs
echo "----------------------------------------"
echo "Tests passed: $TESTS_PASSED / failed: $TESTS_FAILED"
if [ "$TESTS_FAILED" -gt 0 ]; then
  for d in "${FAIL_DETAILS[@]}"; do echo "  $d"; done; exit 1
fi
echo "All tests PASSED"
