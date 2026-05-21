#!/usr/bin/env bash
# Hermetic test for the baseline-aware behavior of check-skip-link.sh.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.5 + EC-8.
#
# Validates the grandfather-baseline contract:
#   1. Violations listed in baseline are silently allowed (PASS).
#   2. A NEW violation outside baseline FAILs the gate.
#   3. Mixed (baseline + new) reports ONLY the new one.
#   4. Comments + blank lines in baseline are ignored.
#   5. Baseline shrink is allowed (fewer grandfathered entries → still PASS).
#   6. IGNORE_BASELINE=1 bypasses baseline (used by audits).
#   7. Missing baseline file → behaves like empty baseline (every viol is new).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$REPO_ROOT/infra/scripts/quality/check-skip-link.sh"

TESTS_PASSED=0
TESTS_FAILED=0
FAIL_DETAILS=()

fail() { TESTS_FAILED=$((TESTS_FAILED + 1)); FAIL_DETAILS+=("FAIL: $1"); echo "FAIL: $1" >&2; }
pass() { TESTS_PASSED=$((TESTS_PASSED + 1)); echo "PASS: $1"; }

# --- fixture helpers ---

make_tmpdir() {
  mktemp -d -t skip-link-baseline-test.XXXXXX
}

make_violating_file() {
  # writes a Go test file that contains a no-URL t.Skip at line 5
  local dir="$1" name="$2"
  mkdir -p "$dir"
  cat > "$dir/$name" <<'EOF'
package fixture

import "testing"

func TestThing(t *testing.T) {
	t.Skip("intentional no-URL skip for fixture")
}
EOF
}

# Path inside the tmpdir as the gate would emit it (REPO_ROOT==TARGET_DIR==tmpdir,
# so relpath == basename or subpath, no leading slash).
relpath_of() { echo "${1#"$2"/}"; }

# --- tests ---

test_script_exists() {
  [ -x "$SCRIPT" ] && pass "test_script_exists" || fail "test_script_exists"
}

test_baseline_grandfathers_violation() {
  local tmp; tmp=$(make_tmpdir)
  make_violating_file "$tmp" "thing_test.go"
  cat > "$tmp/baseline.txt" <<EOF
thing_test.go:6
EOF
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/baseline.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    pass "test_baseline_grandfathers_violation"
  else
    fail "test_baseline_grandfathers_violation (gate should pass with viol in baseline)"
  fi
  rm -rf "$tmp"
}

test_new_violation_fails_gate() {
  local tmp; tmp=$(make_tmpdir)
  make_violating_file "$tmp" "thing_test.go"
  # baseline empty → every violation is new
  : > "$tmp/baseline.txt"
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/baseline.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    fail "test_new_violation_fails_gate (gate should fail with new viol)"
  else
    pass "test_new_violation_fails_gate"
  fi
  rm -rf "$tmp"
}

test_mixed_baseline_and_new_reports_only_new() {
  local tmp; tmp=$(make_tmpdir)
  make_violating_file "$tmp" "old_test.go"
  make_violating_file "$tmp" "new_test.go"
  cat > "$tmp/baseline.txt" <<EOF
old_test.go:6
EOF
  local out
  out=$(TARGET_DIR="$tmp" BASELINE_FILE="$tmp/baseline.txt" \
          bash "$SCRIPT" 2>&1 || true)
  if echo "$out" | grep -q "FAIL: 1 NEW skip(s)" \
     && echo "$out" | grep -q "new_test.go" \
     && ! echo "$out" | grep -q "old_test.go:6:"; then
    pass "test_mixed_baseline_and_new_reports_only_new"
  else
    fail "test_mixed_baseline_and_new_reports_only_new — got: $out"
  fi
  rm -rf "$tmp"
}

test_baseline_ignores_comments_and_blank_lines() {
  local tmp; tmp=$(make_tmpdir)
  make_violating_file "$tmp" "thing_test.go"
  cat > "$tmp/baseline.txt" <<EOF
# leading comment
   # indented comment

thing_test.go:6

# trailing comment
EOF
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/baseline.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    pass "test_baseline_ignores_comments_and_blank_lines"
  else
    fail "test_baseline_ignores_comments_and_blank_lines"
  fi
  rm -rf "$tmp"
}

test_baseline_shrink_allowed() {
  # Baseline lists a viol that no longer exists in the tree → gate still PASS
  # (no new viols, and the gate doesn't enforce baseline-must-be-current).
  local tmp; tmp=$(make_tmpdir)
  # no source file: only baseline mentions a path
  cat > "$tmp/baseline.txt" <<EOF
ghost_test.go:42
EOF
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/baseline.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    pass "test_baseline_shrink_allowed"
  else
    fail "test_baseline_shrink_allowed"
  fi
  rm -rf "$tmp"
}

test_ignore_baseline_env_var_bypasses() {
  local tmp; tmp=$(make_tmpdir)
  make_violating_file "$tmp" "thing_test.go"
  cat > "$tmp/baseline.txt" <<EOF
thing_test.go:6
EOF
  if IGNORE_BASELINE=1 TARGET_DIR="$tmp" BASELINE_FILE="$tmp/baseline.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    fail "test_ignore_baseline_env_var_bypasses (gate should fail when baseline is ignored)"
  else
    pass "test_ignore_baseline_env_var_bypasses"
  fi
  rm -rf "$tmp"
}

test_missing_baseline_treated_as_empty() {
  local tmp; tmp=$(make_tmpdir)
  make_violating_file "$tmp" "thing_test.go"
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/does-not-exist.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    fail "test_missing_baseline_treated_as_empty (no baseline = every viol is new)"
  else
    pass "test_missing_baseline_treated_as_empty"
  fi
  rm -rf "$tmp"
}

test_issue_url_in_context_still_passes_without_baseline() {
  local tmp; tmp=$(make_tmpdir)
  mkdir -p "$tmp"
  cat > "$tmp/ok_test.go" <<'EOF'
package fixture

import "testing"

func TestThing(t *testing.T) {
	// https://github.com/loopzero/loopzero/issues/9999
	t.Skip("documented skip with URL")
}
EOF
  # No baseline file at all
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/none.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    pass "test_issue_url_in_context_still_passes_without_baseline"
  else
    fail "test_issue_url_in_context_still_passes_without_baseline"
  fi
  rm -rf "$tmp"
}

test_skip_filename_convention_still_honored() {
  local tmp; tmp=$(make_tmpdir)
  mkdir -p "$tmp"
  # *_skip_test.go is the documented "intentional permanent skip" convention
  cat > "$tmp/perma_skip_test.go" <<'EOF'
package fixture

import "testing"

func TestThing(t *testing.T) {
	t.Skip("no URL needed — permanent skip via filename convention")
}
EOF
  if TARGET_DIR="$tmp" BASELINE_FILE="$tmp/none.txt" \
       bash "$SCRIPT" >/dev/null 2>&1; then
    pass "test_skip_filename_convention_still_honored"
  else
    fail "test_skip_filename_convention_still_honored"
  fi
  rm -rf "$tmp"
}

# --- runner ---

test_script_exists
test_baseline_grandfathers_violation
test_new_violation_fails_gate
test_mixed_baseline_and_new_reports_only_new
test_baseline_ignores_comments_and_blank_lines
test_baseline_shrink_allowed
test_ignore_baseline_env_var_bypasses
test_missing_baseline_treated_as_empty
test_issue_url_in_context_still_passes_without_baseline
test_skip_filename_convention_still_honored

TOTAL=$((TESTS_PASSED + TESTS_FAILED))
echo ""
echo "=== check-skip-link-baseline_test.sh ==="
echo "PASSED: $TESTS_PASSED / $TOTAL"
if [ "$TESTS_FAILED" -gt 0 ]; then
  echo "FAILED: $TESTS_FAILED"
  for d in "${FAIL_DETAILS[@]}"; do echo "  - $d"; done
  exit 1
fi
exit 0
