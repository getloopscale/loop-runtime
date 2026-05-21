#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/coverage-merge.sh
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.1 TDD.

set -euo pipefail

SCRIPT_UNDER_TEST="${SCRIPT_UNDER_TEST:-infra/scripts/quality/coverage-merge.sh}"
TESTS_PASSED=0
TESTS_FAILED=0
FAIL_DETAILS=()
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

fail() { TESTS_FAILED=$((TESTS_FAILED + 1)); FAIL_DETAILS+=("FAIL: $1"); echo "FAIL: $1" >&2; }
pass() { TESTS_PASSED=$((TESTS_PASSED + 1)); echo "PASS: $1"; }

TMPDIR_LIST=()
cleanup() { rm -rf "${TMPDIR_LIST[@]:-}" 2>/dev/null || true; }
trap cleanup EXIT

setup_tmpdir() {
  local t; t="$(mktemp -d -t coverage-merge-test-XXXXXX)"
  TMPDIR_LIST+=("$t"); echo "$t"
}

# Test 1: script exists + executable
test_script_exists() {
  if [ ! -x "$REPO_ROOT/$SCRIPT_UNDER_TEST" ]; then
    fail "test_script_exists: script missing or not executable"
    return
  fi
  pass "test_script_exists"
}

# Test 2: Go profile → lcov conversion produces SF/DA/LF/LH/end_of_record format
test_merge_converts_go_to_lcov() {
  local t; t="$(setup_tmpdir)"
  # Synthetic Go profile (simulate go test -coverprofile output)
  cat > "$t/coverage.out" <<EOF
mode: atomic
example.com/pkg/foo.go:5.10,7.2 2 1
example.com/pkg/foo.go:9.10,11.2 2 0
example.com/pkg/bar.go:3.5,5.6 1 3
EOF
  : > "$t/cli.lcov"  # empty CLI lcov

  GO_PROFILE="$t/coverage.out" CLI_LCOV="$t/cli.lcov" OUTPUT_FILE="$t/merged.lcov" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if [ ! -s "$t/merged.lcov" ]; then
    fail "test_merge_converts_go_to_lcov: output empty"
    return
  fi
  # Must have SF: lines (one per source file)
  if ! grep -q '^SF:example.com/pkg/foo.go' "$t/merged.lcov"; then
    fail "test_merge_converts_go_to_lcov: missing SF for foo.go"
    return
  fi
  if ! grep -q '^SF:example.com/pkg/bar.go' "$t/merged.lcov"; then
    fail "test_merge_converts_go_to_lcov: missing SF for bar.go"
    return
  fi
  # Must have DA: lines with line numbers + hit counts
  if ! grep -qE '^DA:[0-9]+,[0-9]+' "$t/merged.lcov"; then
    fail "test_merge_converts_go_to_lcov: missing DA: line/count records"
    return
  fi
  # Must have end_of_record per file
  local eor_count
  eor_count=$(grep -c '^end_of_record' "$t/merged.lcov")
  if [ "$eor_count" -lt 2 ]; then
    fail "test_merge_converts_go_to_lcov: expected ≥2 end_of_record (foo + bar), got $eor_count"
    return
  fi
  pass "test_merge_converts_go_to_lcov"
}

# Test 3: concatenates Go + CLI lcov when both present
test_merge_concatenates_go_and_cli() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/coverage.out" <<EOF
mode: atomic
example.com/go/foo.go:1.1,2.2 1 1
EOF
  cat > "$t/cli.lcov" <<EOF
SF:src/cli/auth.ts
DA:1,1
DA:2,0
LF:2
LH:1
end_of_record
EOF
  GO_PROFILE="$t/coverage.out" CLI_LCOV="$t/cli.lcov" OUTPUT_FILE="$t/merged.lcov" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if ! grep -q '^SF:example.com/go/foo.go' "$t/merged.lcov"; then
    fail "test_merge_concatenates: missing Go SF"; return
  fi
  if ! grep -q '^SF:src/cli/auth.ts' "$t/merged.lcov"; then
    fail "test_merge_concatenates: missing CLI SF"; return
  fi
  pass "test_merge_concatenates_go_and_cli"
}

# Test 4: handles missing Go profile (CLI-only)
test_merge_handles_missing_go() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/cli.lcov" <<EOF
SF:src/cli/auth.ts
DA:1,1
LF:1
LH:1
end_of_record
EOF
  GO_PROFILE="$t/does-not-exist.out" CLI_LCOV="$t/cli.lcov" OUTPUT_FILE="$t/merged.lcov" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if ! grep -q '^SF:src/cli/auth.ts' "$t/merged.lcov"; then
    fail "test_merge_handles_missing_go: CLI lcov not preserved"; return
  fi
  pass "test_merge_handles_missing_go"
}

echo "[coverage-merge_test] Running tests against $SCRIPT_UNDER_TEST"
echo "----------------------------------------"
test_script_exists
test_merge_converts_go_to_lcov
test_merge_concatenates_go_and_cli
test_merge_handles_missing_go
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
