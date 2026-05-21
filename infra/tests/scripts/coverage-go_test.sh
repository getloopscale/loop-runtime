#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/coverage-go.sh
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.1 TDD.
#
# Tests (RED before GREEN):
#   test_coverage_go_handles_empty_package — no .go files exits 0
#   test_coverage_go_excludes_vendor — vendor/* not in output
#   test_coverage_go_writes_profile — coverage.out file created
#   test_coverage_go_handles_doc_only_package (EC-10) — doc-only excluded, no error
#
# Hermetic via temp Go workspace + PATH-shadowed `go` stub when needed.

set -euo pipefail

SCRIPT_UNDER_TEST="${SCRIPT_UNDER_TEST:-infra/scripts/quality/coverage-go.sh}"
TESTS_PASSED=0
TESTS_FAILED=0
FAIL_DETAILS=()

# Repo root resolved from script location
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

fail() {
  local msg="$1"
  TESTS_FAILED=$((TESTS_FAILED + 1))
  FAIL_DETAILS+=("FAIL: $msg")
  echo "FAIL: $msg" >&2
}

pass() {
  local msg="$1"
  TESTS_PASSED=$((TESTS_PASSED + 1))
  echo "PASS: $msg"
}

# Setup hermetic temp dir per test
setup_tmpdir() {
  local tmpdir
  tmpdir="$(mktemp -d -t coverage-go-test-XXXXXX)"
  echo "$tmpdir"
}

# Cleanup
cleanup() {
  rm -rf "${TMPDIR_LIST[@]:-}" 2>/dev/null || true
}
TMPDIR_LIST=()
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Test 1: script exists + is executable
test_script_exists() {
  if [ ! -f "$REPO_ROOT/$SCRIPT_UNDER_TEST" ]; then
    fail "test_script_exists: $SCRIPT_UNDER_TEST does not exist"
    return
  fi
  if [ ! -x "$REPO_ROOT/$SCRIPT_UNDER_TEST" ]; then
    fail "test_script_exists: $SCRIPT_UNDER_TEST not executable"
    return
  fi
  pass "test_script_exists"
}

# ---------------------------------------------------------------------------
# Test 2: handles empty Go module (no .go files) — exits 0, no profile error
test_coverage_go_handles_empty_package() {
  local tmpdir
  tmpdir="$(setup_tmpdir)"
  TMPDIR_LIST+=("$tmpdir")

  # Empty go module with only doc.go (no testable statements)
  cd "$tmpdir"
  cat > go.mod <<EOF
module example.com/empty
go 1.25
EOF
  # No .go files with statements

  # Run script with TARGET pointing to tmpdir
  if TARGET_DIR="$tmpdir" OUTPUT_FILE="$tmpdir/coverage.out" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_coverage_go_handles_empty_package"
  else
    fail "test_coverage_go_handles_empty_package: exit non-zero on empty module"
  fi
}

# ---------------------------------------------------------------------------
# Test 3: excludes vendor/ from coverage report
test_coverage_go_excludes_vendor() {
  local tmpdir
  tmpdir="$(setup_tmpdir)"
  TMPDIR_LIST+=("$tmpdir")

  cd "$tmpdir"
  cat > go.mod <<EOF
module example.com/withvendor
go 1.25
EOF
  cat > main.go <<EOF
package main
func main() {}
func Add(a, b int) int { return a + b }
EOF
  cat > main_test.go <<EOF
package main
import "testing"
func TestAdd(t *testing.T) {
  if Add(1, 2) != 3 { t.Fatal("wrong") }
}
EOF
  # Fake vendor with .go file
  mkdir -p vendor/example.com/dep
  cat > vendor/example.com/dep/dep.go <<EOF
package dep
func Foo() int { return 42 }
EOF

  TARGET_DIR="$tmpdir" OUTPUT_FILE="$tmpdir/coverage.out" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1 || true

  if [ -f "$tmpdir/coverage.out" ]; then
    # Strict regex: match ONLY "/vendor/" directory paths (not "withvendor"
    # substring in module names). Profile line format:
    #   <module>/<path>:<line>.<col>,<line>.<col> <statements> <count>
    # Vendor leak would manifest as ".../vendor/<dep>/..." in path.
    if grep -E '(^|/)vendor/' "$tmpdir/coverage.out" 2>/dev/null; then
      fail "test_coverage_go_excludes_vendor: vendor/ paths leaked into coverage.out"
    else
      pass "test_coverage_go_excludes_vendor"
    fi
  else
    fail "test_coverage_go_excludes_vendor: no coverage.out written"
  fi
}

# ---------------------------------------------------------------------------
# Test 4: writes coverage profile for module with tests
test_coverage_go_writes_profile() {
  local tmpdir
  tmpdir="$(setup_tmpdir)"
  TMPDIR_LIST+=("$tmpdir")

  cd "$tmpdir"
  cat > go.mod <<EOF
module example.com/withtest
go 1.25
EOF
  cat > main.go <<EOF
package main
func Multiply(a, b int) int { return a * b }
EOF
  cat > main_test.go <<EOF
package main
import "testing"
func TestMultiply(t *testing.T) {
  if Multiply(2, 3) != 6 { t.Fatal("wrong") }
}
EOF

  TARGET_DIR="$tmpdir" OUTPUT_FILE="$tmpdir/coverage.out" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if [ ! -f "$tmpdir/coverage.out" ]; then
    fail "test_coverage_go_writes_profile: coverage.out not created"
    return
  fi
  if [ ! -s "$tmpdir/coverage.out" ]; then
    fail "test_coverage_go_writes_profile: coverage.out empty"
    return
  fi
  # First line should be the cover mode header (atomic or count)
  local first_line
  first_line=$(head -1 "$tmpdir/coverage.out")
  if [[ ! "$first_line" =~ ^mode: ]]; then
    fail "test_coverage_go_writes_profile: profile missing 'mode:' header (got: $first_line)"
    return
  fi
  pass "test_coverage_go_writes_profile"
}

# ---------------------------------------------------------------------------
# Test 5 (EC-10): handles doc-only package (no testable statements, no error)
test_coverage_go_handles_doc_only_package() {
  local tmpdir
  tmpdir="$(setup_tmpdir)"
  TMPDIR_LIST+=("$tmpdir")

  cd "$tmpdir"
  cat > go.mod <<EOF
module example.com/doconly
go 1.25
EOF
  # Single doc.go with only package declaration + doc comment
  cat > doc.go <<EOF
// Package doconly is documentation-only — no testable statements.
package doconly
EOF

  if TARGET_DIR="$tmpdir" OUTPUT_FILE="$tmpdir/coverage.out" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_coverage_go_handles_doc_only_package (EC-10)"
  else
    fail "test_coverage_go_handles_doc_only_package (EC-10): exit non-zero on doc-only package"
  fi
}

# ---------------------------------------------------------------------------
# Run all tests
echo "[coverage-go_test] Running tests against $SCRIPT_UNDER_TEST"
echo "----------------------------------------"
test_script_exists
test_coverage_go_handles_empty_package
test_coverage_go_excludes_vendor
test_coverage_go_writes_profile
test_coverage_go_handles_doc_only_package
echo "----------------------------------------"

echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"
if [ "$TESTS_FAILED" -gt 0 ]; then
  echo ""
  echo "Failure details:"
  for d in "${FAIL_DETAILS[@]}"; do
    echo "  $d"
  done
  exit 1
fi
echo ""
echo "Tests run:    $((TESTS_PASSED + TESTS_FAILED))"
echo "All tests PASSED"
