#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/coverage-diff.sh
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T2.1 TDD + EC-7.

set -euo pipefail

SCRIPT_UNDER_TEST="${SCRIPT_UNDER_TEST:-infra/scripts/quality/coverage-diff.sh}"
TESTS_PASSED=0
TESTS_FAILED=0
FAIL_DETAILS=()
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

fail() { TESTS_FAILED=$((TESTS_FAILED + 1)); FAIL_DETAILS+=("FAIL: $1"); echo "FAIL: $1" >&2; }
pass() { TESTS_PASSED=$((TESTS_PASSED + 1)); echo "PASS: $1"; }

TMPDIR_LIST=()
cleanup() { rm -rf "${TMPDIR_LIST[@]:-}" 2>/dev/null || true; }
trap cleanup EXIT
setup_tmpdir() { local t; t="$(mktemp -d -t covdiff-XXXXXX)"; TMPDIR_LIST+=("$t"); echo "$t"; }

test_script_exists() {
  [ -x "$REPO_ROOT/$SCRIPT_UNDER_TEST" ] && pass "test_script_exists" || fail "test_script_exists"
}

# Diff list with NEW covered lines → exit 0 (PASS)
test_diff_gate_passes_when_new_code_covered() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/diff.txt" <<'EOF'
src/foo.go:10
src/foo.go:11
EOF
  cat > "$t/cov.lcov" <<'EOF'
SF:src/foo.go
DA:10,1
DA:11,1
LF:2
LH:2
end_of_record
EOF
  if DIFF_FILE="$t/diff.txt" LCOV_FILE="$t/cov.lcov" THRESHOLD=80 \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_diff_gate_passes_when_new_code_covered"
  else
    fail "test_diff_gate_passes_when_new_code_covered: gate failed despite 100% coverage"
  fi
}

# Diff list with NEW uncovered lines below threshold → exit non-zero (FAIL)
test_diff_gate_fails_when_new_code_uncovered() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/diff.txt" <<'EOF'
src/foo.go:10
src/foo.go:11
src/foo.go:12
src/foo.go:13
src/foo.go:14
EOF
  cat > "$t/cov.lcov" <<'EOF'
SF:src/foo.go
DA:10,1
DA:11,1
DA:12,0
DA:13,0
DA:14,0
LF:5
LH:2
end_of_record
EOF
  # 2/5 = 40% covered, threshold 80% → should fail
  if DIFF_FILE="$t/diff.txt" LCOV_FILE="$t/cov.lcov" THRESHOLD=80 \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    fail "test_diff_gate_fails_when_new_code_uncovered: gate passed with 40% coverage vs 80% threshold"
  else
    pass "test_diff_gate_fails_when_new_code_uncovered"
  fi
}

# Diff with only renamed files → PASS (EC-7 fix)
test_coverage_diff_passes_for_rename_only_PR() {
  local t; t="$(setup_tmpdir)"
  # Empty diff (rename detection removes from diff)
  : > "$t/diff.txt"
  : > "$t/cov.lcov"
  if DIFF_FILE="$t/diff.txt" LCOV_FILE="$t/cov.lcov" THRESHOLD=80 \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_coverage_diff_passes_for_rename_only_PR (EC-7)"
  else
    fail "test_coverage_diff_passes_for_rename_only_PR (EC-7): empty diff failed"
  fi
}

# PR adding only test files → PASS (test files have trivial coverage)
test_diff_gate_passes_for_test_only_PRs() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/diff.txt" <<'EOF'
src/foo_test.go:1
src/foo.test.ts:1
EOF
  : > "$t/cov.lcov"  # no coverage for test files themselves
  if DIFF_FILE="$t/diff.txt" LCOV_FILE="$t/cov.lcov" THRESHOLD=80 \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_diff_gate_passes_for_test_only_PRs"
  else
    fail "test_diff_gate_passes_for_test_only_PRs"
  fi
}

# Non-source files ignored
test_diff_gate_ignores_non_source_files() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/diff.txt" <<'EOF'
README.md:1
docs/foo.md:5
.github/workflows/x.yml:10
EOF
  : > "$t/cov.lcov"
  if DIFF_FILE="$t/diff.txt" LCOV_FILE="$t/cov.lcov" THRESHOLD=80 \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_diff_gate_ignores_non_source_files"
  else
    fail "test_diff_gate_ignores_non_source_files"
  fi
}

echo "[coverage-diff_test] Running tests against $SCRIPT_UNDER_TEST"
echo "----------------------------------------"
test_script_exists
test_diff_gate_passes_when_new_code_covered
test_diff_gate_fails_when_new_code_uncovered
test_coverage_diff_passes_for_rename_only_PR
test_diff_gate_passes_for_test_only_PRs
test_diff_gate_ignores_non_source_files
echo "----------------------------------------"

echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"
if [ "$TESTS_FAILED" -gt 0 ]; then
  echo ""; echo "Failure details:"
  for d in "${FAIL_DETAILS[@]}"; do echo "  $d"; done
  exit 1
fi
echo ""; echo "Tests run:    $((TESTS_PASSED + TESTS_FAILED))"
echo "All tests PASSED"
