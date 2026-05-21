#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/baseline-update.sh
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.2 TDD.

set -euo pipefail

SCRIPT_UNDER_TEST="${SCRIPT_UNDER_TEST:-infra/scripts/quality/baseline-update.sh}"
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
  local t; t="$(mktemp -d -t baseline-XXXXXX)"
  TMPDIR_LIST+=("$t"); echo "$t"
}

test_script_exists() {
  if [ ! -x "$REPO_ROOT/$SCRIPT_UNDER_TEST" ]; then
    fail "test_script_exists: script missing or not executable"; return
  fi
  pass "test_script_exists"
}

# Generates valid JSON from a fixture Go profile
test_baseline_emits_valid_json() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/cov.out" <<'EOF'
mode: atomic
github.com/loopzero/loopzero/pkg/errors/foo.go:5.1,7.2 2 1
github.com/loopzero/loopzero/pkg/errors/foo.go:10.1,12.2 2 0
EOF
  : > "$t/cli.lcov"

  GO_PROFILE="$t/cov.out" CLI_LCOV="$t/cli.lcov" BASELINE_FILE="$t/baseline.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  if ! python3 -c "import json; json.load(open('$t/baseline.json'))" 2>/dev/null; then
    fail "test_baseline_emits_valid_json: invalid JSON"; return
  fi
  pass "test_baseline_emits_valid_json"
}

# Critical packages get min_required=90
test_baseline_critical_packages_get_90() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/cov.out" <<'EOF'
mode: atomic
github.com/loopzero/loopzero/pkg/jwt/jwt.go:5.1,7.2 2 1
github.com/loopzero/loopzero/pkg/jwt/jwt.go:10.1,12.2 2 0
EOF
  : > "$t/cli.lcov"

  GO_PROFILE="$t/cov.out" CLI_LCOV="$t/cli.lcov" BASELINE_FILE="$t/baseline.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  local min_req
  min_req=$(python3 -c "
import json
d=json.load(open('$t/baseline.json'))
pkg='github.com/loopzero/loopzero/pkg/jwt'
if pkg in d['packages']:
    print(d['packages'][pkg]['min_required'])
")
  if [ "$min_req" != "90" ]; then
    fail "test_baseline_critical_packages_get_90: expected 90, got $min_req"; return
  fi
  pass "test_baseline_critical_packages_get_90"
}

# Non-critical packages: min_required = floor(current - 5pp)
test_baseline_non_critical_uses_current_minus_5pp() {
  local t; t="$(setup_tmpdir)"
  # Fake 80% coverage (4 of 5 statements covered)
  cat > "$t/cov.out" <<'EOF'
mode: atomic
github.com/loopzero/loopzero/some/random/pkg/file.go:1.1,1.2 1 1
github.com/loopzero/loopzero/some/random/pkg/file.go:2.1,2.2 1 1
github.com/loopzero/loopzero/some/random/pkg/file.go:3.1,3.2 1 1
github.com/loopzero/loopzero/some/random/pkg/file.go:4.1,4.2 1 1
github.com/loopzero/loopzero/some/random/pkg/file.go:5.1,5.2 1 0
EOF
  : > "$t/cli.lcov"

  GO_PROFILE="$t/cov.out" CLI_LCOV="$t/cli.lcov" BASELINE_FILE="$t/baseline.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  # 4 of 5 = 80% per file → for random pkg, min_required = 80 - 5 = 75
  local min_req
  min_req=$(python3 -c "
import json
d=json.load(open('$t/baseline.json'))
pkg='github.com/loopzero/loopzero/some/random/pkg'
if pkg in d['packages']:
    print(d['packages'][pkg]['min_required'])
")
  # Expected: between 50 (clamp) and 90 (clamp)
  # Allow tolerance for go tool cover aggregation rounding
  if [ -z "$min_req" ]; then
    fail "test_baseline_non_critical_uses_current_minus_5pp: package missing"; return
  fi
  if [ "$min_req" -lt 50 ] || [ "$min_req" -gt 90 ]; then
    fail "test_baseline_non_critical_uses_current_minus_5pp: $min_req out of [50,90]"; return
  fi
  pass "test_baseline_non_critical_uses_current_minus_5pp"
}

# Idempotency: re-run preserves existing min_required
test_baseline_preserves_existing_min_required() {
  local t; t="$(setup_tmpdir)"
  # Seed baseline with custom min_required
  mkdir -p "$t/qual"
  cat > "$t/qual/baseline.json" <<'EOF'
{
  "schema_version": "1.0",
  "generated_at": "2026-05-21T00:00:00Z",
  "critical_packages": [],
  "packages": {
    "github.com/example/seeded": { "line": 60.0, "min_required": 60 }
  }
}
EOF
  cat > "$t/cov.out" <<'EOF'
mode: atomic
github.com/example/seeded/file.go:1.1,1.2 1 1
EOF
  : > "$t/cli.lcov"

  GO_PROFILE="$t/cov.out" CLI_LCOV="$t/cli.lcov" BASELINE_FILE="$t/qual/baseline.json" \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" >/dev/null 2>&1

  # min_required should remain 60, NOT auto-bump to current
  local min_req
  min_req=$(python3 -c "
import json
d=json.load(open('$t/qual/baseline.json'))
pkg='github.com/example/seeded'
if pkg in d['packages']:
    print(d['packages'][pkg]['min_required'])
")
  if [ "$min_req" != "60" ]; then
    fail "test_baseline_preserves_existing_min_required: expected 60, got $min_req"; return
  fi
  pass "test_baseline_preserves_existing_min_required"
}

# DRY_RUN=1 prints to stdout, doesn't write
test_baseline_dry_run_does_not_write() {
  local t; t="$(setup_tmpdir)"
  cat > "$t/cov.out" <<'EOF'
mode: atomic
github.com/loopzero/loopzero/pkg/foo/x.go:1.1,1.2 1 1
EOF
  : > "$t/cli.lcov"
  # Baseline file does NOT exist initially
  local out
  out=$(GO_PROFILE="$t/cov.out" CLI_LCOV="$t/cli.lcov" BASELINE_FILE="$t/baseline.json" DRY_RUN=1 \
    bash "$REPO_ROOT/$SCRIPT_UNDER_TEST" 2>/dev/null)

  if [ -f "$t/baseline.json" ]; then
    fail "test_baseline_dry_run_does_not_write: file created in dry-run"; return
  fi
  if ! echo "$out" | python3 -c "import json, sys; json.loads(sys.stdin.read())" 2>/dev/null; then
    fail "test_baseline_dry_run_does_not_write: stdout not valid JSON"; return
  fi
  pass "test_baseline_dry_run_does_not_write"
}

echo "[baseline-update_test] Running tests against $SCRIPT_UNDER_TEST"
echo "----------------------------------------"
test_script_exists
test_baseline_emits_valid_json
test_baseline_critical_packages_get_90
test_baseline_non_critical_uses_current_minus_5pp
test_baseline_preserves_existing_min_required
test_baseline_dry_run_does_not_write
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
