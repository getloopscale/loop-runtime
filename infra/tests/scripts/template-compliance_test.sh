#!/usr/bin/env bash
# Hermetic test for infra/scripts/quality/template-compliance.sh
# Plan 2026-05-21 v1.1 T5.1 + EC-5.

set -euo pipefail

SCRIPT="${SCRIPT:-infra/scripts/quality/template-compliance.sh}"
TESTS_PASSED=0; TESTS_FAILED=0; FAIL_DETAILS=()
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

fail() { TESTS_FAILED=$((TESTS_FAILED + 1)); FAIL_DETAILS+=("FAIL: $1"); echo "FAIL: $1" >&2; }
pass() { TESTS_PASSED=$((TESTS_PASSED + 1)); echo "PASS: $1"; }

BODY_OK='## Summary
test

## Quality Gates Checklist

- [x] Tests added for new code (or N/A justified below)
- [x] Regression test added if fixing bug (or N/A)
- [x] Coverage diff PASS (will be verified by CI)
- [x] No tests deleted (or `test-deletion-approved` label + justification below)
- [x] CHANGELOG.md entry added under `[Unreleased]` (or N/A)
'

BODY_UNCHECKED='## Summary
test

## Quality Gates Checklist

- [ ] Tests added for new code (or N/A justified below)
- [x] Regression test added if fixing bug (or N/A)
'

BODY_UNCHECKED_WITH_NA='## Summary
test

## Quality Gates Checklist

- [ ] Tests added for new code (or N/A justified below)
- [x] Regression test added if fixing bug (or N/A)

### N/A justifications (if any)

This is a doc-only PR; no tests apply.
'

test_script_exists() {
  [ -x "$REPO_ROOT/$SCRIPT" ] && pass "test_script_exists" || fail "test_script_exists"
}

test_template_gate_passes_all_checked() {
  if PR_BODY="$BODY_OK" PR_AUTHOR="alice" PR_LABELS="" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_template_gate_passes_all_checked"
  else
    fail "test_template_gate_passes_all_checked"
  fi
}

test_template_gate_fails_unchecked() {
  if PR_BODY="$BODY_UNCHECKED" PR_AUTHOR="alice" PR_LABELS="" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    fail "test_template_gate_fails_unchecked"
  else
    pass "test_template_gate_fails_unchecked"
  fi
}

test_template_gate_accepts_n_a_with_reason() {
  if PR_BODY="$BODY_UNCHECKED_WITH_NA" PR_AUTHOR="alice" PR_LABELS="" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_template_gate_accepts_n_a_with_reason"
  else
    fail "test_template_gate_accepts_n_a_with_reason"
  fi
}

# EC-5: bot author bypasses gate
test_template_gate_skips_dependabot_PRs() {
  if PR_BODY="" PR_AUTHOR="dependabot[bot]" PR_LABELS="" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_template_gate_skips_dependabot_PRs (EC-5)"
  else
    fail "test_template_gate_skips_dependabot_PRs (EC-5)"
  fi
}

test_template_gate_skips_renovate_PRs() {
  if PR_BODY="" PR_AUTHOR="renovate[bot]" PR_LABELS="" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_template_gate_skips_renovate_PRs (EC-5)"
  else
    fail "test_template_gate_skips_renovate_PRs (EC-5)"
  fi
}

test_template_gate_skips_PRs_with_dependencies_label() {
  if PR_BODY="" PR_AUTHOR="alice" PR_LABELS="dependencies" \
       bash "$REPO_ROOT/$SCRIPT" >/dev/null 2>&1; then
    pass "test_template_gate_skips_PRs_with_dependencies_label (EC-5)"
  else
    fail "test_template_gate_skips_PRs_with_dependencies_label (EC-5)"
  fi
}

echo "[template-compliance_test] Running against $SCRIPT"
echo "----------------------------------------"
test_script_exists
test_template_gate_passes_all_checked
test_template_gate_fails_unchecked
test_template_gate_accepts_n_a_with_reason
test_template_gate_skips_dependabot_PRs
test_template_gate_skips_renovate_PRs
test_template_gate_skips_PRs_with_dependencies_label
echo "----------------------------------------"

echo "Tests passed: $TESTS_PASSED / failed: $TESTS_FAILED"
if [ "$TESTS_FAILED" -gt 0 ]; then
  for d in "${FAIL_DETAILS[@]}"; do echo "  $d"; done; exit 1
fi
echo "All tests PASSED"
