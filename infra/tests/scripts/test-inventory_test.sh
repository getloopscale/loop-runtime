#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/test-inventory.sh
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.3 TDD.

set -euo pipefail

SCRIPT_UNDER_TEST="${SCRIPT_UNDER_TEST:-infra/scripts/quality/test-inventory.sh}"
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
  local t; t="$(mktemp -d -t test-inventory-XXXXXX)"
  TMPDIR_LIST+=("$t"); echo "$t"
}

test_script_exists() {
  if [ ! -x "$REPO_ROOT/$SCRIPT_UNDER_TEST" ]; then
    fail "test_script_exists: script missing or not executable"
    return
  fi
  pass "test_script_exists"
}

# Classifier behavior: file naming convention determines category
test_inventory_classifies_unit() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/pkg/foo"
  cat > "$t/pkg/foo/bar_test.go" <<'EOF'
package foo
import "testing"
func TestBar(t *testing.T) {}
func TestBaz(t *testing.T) {}
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if [ ! -s "$t/inv.json" ]; then
    fail "test_inventory_classifies_unit: no output JSON"; return
  fi
  # *_test.go (no _integration_ or _e2e_) = unit
  if ! grep -q '"category": "unit"' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_classifies_unit: *_test.go not classified as 'unit'"
    return
  fi
  pass "test_inventory_classifies_unit"
}

test_inventory_classifies_integration() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/pkg"
  cat > "$t/pkg/db_integration_test.go" <<'EOF'
package pkg
import "testing"
func TestDBIntegration(t *testing.T) {}
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if ! grep -q '"category": "integration"' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_classifies_integration: *_integration_test.go not classified"
    return
  fi
  pass "test_inventory_classifies_integration"
}

test_inventory_classifies_e2e() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/tests"
  cat > "$t/tests/journey_e2e_test.go" <<'EOF'
package tests
import "testing"
func TestJourneyE2E(t *testing.T) {}
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if ! grep -q '"category": "e2e"' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_classifies_e2e: *_e2e_test.go not classified"
    return
  fi
  pass "test_inventory_classifies_e2e"
}

test_inventory_counts_test_functions() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/pkg"
  cat > "$t/pkg/multi_test.go" <<'EOF'
package pkg
import "testing"
func TestOne(t *testing.T) {}
func TestTwo(t *testing.T) {}
func TestThree(t *testing.T) {}
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  # Expect count 3 in JSON
  if ! grep -q '"test_count": 3' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_counts_test_functions: expected test_count=3 for 3 TestXxx funcs"
    return
  fi
  pass "test_inventory_counts_test_functions"
}

test_inventory_excludes_vendor_and_node_modules() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/vendor/dep" "$t/node_modules/dep" "$t/pkg/real"
  cat > "$t/vendor/dep/dep_test.go" <<'EOF'
package dep
import "testing"
func TestVendor(t *testing.T) {}
EOF
  cat > "$t/node_modules/dep/index.test.ts" <<'EOF'
test('foo', () => {});
EOF
  cat > "$t/pkg/real/real_test.go" <<'EOF'
package real
import "testing"
func TestReal(t *testing.T) {}
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if grep -q 'vendor/' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_excludes_vendor: vendor/ files leaked"; return
  fi
  if grep -q 'node_modules' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_excludes_node_modules: node_modules files leaked"; return
  fi
  if ! grep -q 'pkg/real/real_test.go' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_excludes: real test file missing"; return
  fi
  pass "test_inventory_excludes_vendor_and_node_modules"
}

test_inventory_classifies_ts_unit() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/src"
  cat > "$t/src/foo.test.ts" <<'EOF'
import { it } from 'vitest'
it('does a thing', () => {});
it('does another', () => {});
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if ! grep -q 'src/foo.test.ts' "$t/inv.json" 2>/dev/null; then
    fail "test_inventory_classifies_ts_unit: TS test file missing"; return
  fi
  # *.test.ts (no .integration or .e2e) = unit
  if ! grep -A1 'src/foo.test.ts' "$t/inv.json" 2>/dev/null | grep -q '"unit"'; then
    fail "test_inventory_classifies_ts_unit: *.test.ts not classified as unit"; return
  fi
  pass "test_inventory_classifies_ts_unit"
}

test_inventory_emits_summary() {
  local t; t="$(setup_tmpdir)"
  mkdir -p "$t/pkg"
  cat > "$t/pkg/u_test.go" <<'EOF'
package pkg
import "testing"
func TestU(t *testing.T) {}
EOF
  cat > "$t/pkg/i_integration_test.go" <<'EOF'
package pkg
import "testing"
func TestI(t *testing.T) {}
EOF
  TARGET_DIR="$t" OUTPUT_FILE="$t/inv.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  # Summary block should report counts per category
  if ! grep -q '"summary"' "$t/inv.json"; then
    fail "test_inventory_emits_summary: missing summary block"; return
  fi
  if ! grep -q '"unit": 1' "$t/inv.json"; then
    fail "test_inventory_emits_summary: unit count wrong"; return
  fi
  if ! grep -q '"integration": 1' "$t/inv.json"; then
    fail "test_inventory_emits_summary: integration count wrong"; return
  fi
  pass "test_inventory_emits_summary"
}

echo "[test-inventory_test] Running tests against $SCRIPT_UNDER_TEST"
echo "----------------------------------------"
test_script_exists
test_inventory_classifies_unit
test_inventory_classifies_integration
test_inventory_classifies_e2e
test_inventory_counts_test_functions
test_inventory_excludes_vendor_and_node_modules
test_inventory_classifies_ts_unit
test_inventory_emits_summary
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
