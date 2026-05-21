#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/baseline-compare.sh
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T2.2 + EC-2.

set -euo pipefail

SCRIPT_UNDER_TEST="${SCRIPT_UNDER_TEST:-infra/scripts/quality/baseline-compare.sh}"
TESTS_PASSED=0
TESTS_FAILED=0
FAIL_DETAILS=()
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

fail() { TESTS_FAILED=$((TESTS_FAILED + 1)); FAIL_DETAILS+=("FAIL: $1"); echo "FAIL: $1" >&2; }
pass() { TESTS_PASSED=$((TESTS_PASSED + 1)); echo "PASS: $1"; }

TMPDIR_LIST=()
cleanup() { rm -rf "${TMPDIR_LIST[@]:-}" 2>/dev/null || true; }
trap cleanup EXIT
setup_tmpdir() { local t; t="$(mktemp -d -t baselinecmp-XXXXXX)"; TMPDIR_LIST+=("$t"); echo "$t"; }

# Build minimal baseline + current coverage JSONs
write_baseline() {
  local file="$1"; local pkg="$2"; local line="$3"; local min_req="$4"
  cat > "$file" <<EOF
{
  "schema_version": "1.0",
  "critical_packages": [],
  "packages": {
    "$pkg": { "line": $line, "min_required": $min_req }
  }
}
EOF
}

write_current() {
  local file="$1"; local pkg="$2"; local line="$3"
  cat > "$file" <<EOF
{
  "packages": {
    "$pkg": { "line": $line }
  }
}
EOF
}

test_script_exists() {
  [ -x "$REPO_ROOT/$SCRIPT_UNDER_TEST" ] && pass "test_script_exists" || fail "test_script_exists"
}

test_baseline_gate_passes_when_pkg_above_min() {
  local t; t="$(setup_tmpdir)"
  write_baseline "$t/baseline.json" "github.com/x/y" 85.0 80
  write_current  "$t/current.json"  "github.com/x/y" 85.0
  if BASELINE_FILE="$t/baseline.json" CURRENT_FILE="$t/current.json" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_baseline_gate_passes_when_pkg_above_min"
  else
    fail "test_baseline_gate_passes_when_pkg_above_min"
  fi
}

test_baseline_gate_fails_when_pkg_drops_below_min() {
  local t; t="$(setup_tmpdir)"
  write_baseline "$t/baseline.json" "github.com/x/y" 85.0 80
  write_current  "$t/current.json"  "github.com/x/y" 70.0  # below min 80
  if BASELINE_FILE="$t/baseline.json" CURRENT_FILE="$t/current.json" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    fail "test_baseline_gate_fails_when_pkg_drops_below_min"
  else
    pass "test_baseline_gate_fails_when_pkg_drops_below_min"
  fi
}

test_baseline_gate_passes_with_3pp_slack() {
  local t; t="$(setup_tmpdir)"
  write_baseline "$t/baseline.json" "github.com/x/y" 85.0 80
  write_current  "$t/current.json"  "github.com/x/y" 82.5  # within 3pp slack
  if BASELINE_FILE="$t/baseline.json" CURRENT_FILE="$t/current.json" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_baseline_gate_passes_with_3pp_slack"
  else
    fail "test_baseline_gate_passes_with_3pp_slack"
  fi
}

test_baseline_gate_handles_new_package() {
  local t; t="$(setup_tmpdir)"
  # Baseline empty
  cat > "$t/baseline.json" <<'EOF'
{ "schema_version": "1.0", "critical_packages": [], "packages": {} }
EOF
  # Current has new package with 60% (below default 80%)
  write_current "$t/current.json" "github.com/x/newpkg" 60.0
  # Should FAIL because new pkg below default threshold
  if BASELINE_FILE="$t/baseline.json" CURRENT_FILE="$t/current.json" DEFAULT_MIN=80 \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    fail "test_baseline_gate_handles_new_package: new pkg with 60% should fail default 80%"
  else
    pass "test_baseline_gate_handles_new_package"
  fi
}

# EC-2: PR with `coverage-baseline-defer` label + reason → PASS
test_baseline_gate_honors_defer_label() {
  local t; t="$(setup_tmpdir)"
  write_baseline "$t/baseline.json" "github.com/x/y" 85.0 80
  write_current  "$t/current.json"  "github.com/x/y" 50.0  # below — would fail normally
  # Simulate defer label + reason via env vars
  if BASELINE_FILE="$t/baseline.json" CURRENT_FILE="$t/current.json" \
       DEFER_LABEL_PRESENT=1 DEFER_REASON="Refactoring split into 2 packages" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    pass "test_baseline_gate_honors_defer_label (EC-2)"
  else
    fail "test_baseline_gate_honors_defer_label (EC-2)"
  fi
}

# EC-2 guard: defer label WITHOUT reason → still FAIL
test_baseline_gate_rejects_defer_without_reason() {
  local t; t="$(setup_tmpdir)"
  write_baseline "$t/baseline.json" "github.com/x/y" 85.0 80
  write_current  "$t/current.json"  "github.com/x/y" 50.0
  if BASELINE_FILE="$t/baseline.json" CURRENT_FILE="$t/current.json" \
       DEFER_LABEL_PRESENT=1 DEFER_REASON="" \
       bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1; then
    fail "test_baseline_gate_rejects_defer_without_reason (EC-2 guard)"
  else
    pass "test_baseline_gate_rejects_defer_without_reason (EC-2 guard)"
  fi
}

echo "[baseline-compare_test] Running tests against $SCRIPT_UNDER_TEST"
echo "----------------------------------------"
test_script_exists
test_baseline_gate_passes_when_pkg_above_min
test_baseline_gate_fails_when_pkg_drops_below_min
test_baseline_gate_passes_with_3pp_slack
test_baseline_gate_handles_new_package
test_baseline_gate_honors_defer_label
test_baseline_gate_rejects_defer_without_reason
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
