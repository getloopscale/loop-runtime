#!/usr/bin/env bash
# check-dev-public-production-deploy-syntax_test.sh
#
# T6.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4 — hermetic
# structural test for dev-public-production-deploy_test.sh.
#
# Structural assertions (no network, no kubectl, no curl):
#   - bash -n syntax check
#   - All 13 chain steps documented in header
#   - 4 anti-workaround grep guards present (the 4 manual operator patches
#     T6.1 of plan 2026-06-08-pre-release-test-rigor had to apply)
#   - 3 CLI flags wired (--skip-live, --json, --skip-reset)
#   - Self-grep anti-workaround is ALWAYS executed (not just on full live)
#   - Wall-clock budget asserted as 20min cross-reference
#   - Reuses canonical primitives (reset-for-e2e.sh, seed-admin-loopzero-dev.sh,
#     connect-canonical-fixture.sh) — does NOT redefine them inline
#   - Allowlist entry present at .claude/rules/shell-scripts-allowlist.md

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET="$REPO_ROOT/tests/e2e/dev-public-production-deploy_test.sh"

[[ -f "$TARGET" ]] || { echo "FATAL: target script not found at $TARGET" >&2; exit 2; }

PASS=0
FAIL=0
FAILED=()

assert_match() {
  local name="$1" pattern="$2"
  if grep -qE -- "$pattern" "$TARGET"; then
    PASS=$((PASS+1))
    printf "  PASS  %s\n" "$name"
  else
    FAIL=$((FAIL+1))
    FAILED+=("$name (pattern $pattern not found)")
    printf "  FAIL  %s — pattern %s not in script\n" "$name" "$pattern"
  fi
}

assert_no_match() {
  local name="$1" pattern="$2"
  # Strip comment lines first so the comment that documents the workaround
  # doesn't false-positive the anti-workaround grep.
  local body
  body="$(grep -vE '^[[:space:]]*#' "$TARGET" || true)"
  if echo "$body" | grep -qE "$pattern"; then
    FAIL=$((FAIL+1))
    FAILED+=("$name (pattern $pattern unexpectedly found in non-comment body)")
    printf "  FAIL  %s — pattern %s found in non-comment script body\n" "$name" "$pattern"
  else
    PASS=$((PASS+1))
    printf "  PASS  %s\n" "$name"
  fi
}

echo "=== check-dev-public-production-deploy-syntax_test.sh ==="
echo

# T1 — bash syntax
if bash -n "$TARGET" 2>/dev/null; then
  PASS=$((PASS+1)); echo "  PASS  T1_bash_syntax"
else
  FAIL=$((FAIL+1)); FAILED+=("T1_bash_syntax"); echo "  FAIL  T1_bash_syntax"
fi

# T2 — 13 chain steps documented in header
assert_match "T2_step_1_preflight"      "step 1/13"
assert_match "T2_step_2_reset"          "step 2/13"
assert_match "T2_step_3_seed"           "step 3/13"
assert_match "T2_step_4_project"        "step 4/13"
assert_match "T2_step_5_connect"        "step 5/13"
assert_match "T2_step_6_push"           "step 6/13"
assert_match "T2_step_7_release"        "step 7/13"
assert_match "T2_step_8_m2_namespace"   "step 8/13"
assert_match "T2_step_9_appproject"     "step 9/13"
assert_match "T2_step_10_sa_loopzero_app"   "step 10/13"
assert_match "T2_step_11_pods_running"  "step 11/13"
assert_match "T2_step_12_argocd_health" "step 12/13"
assert_match "T2_step_13_anti_workaround" "step 13/13"

# T3 — flags wired
assert_match "T3_skip_live_flag"  "--skip-live"
assert_match "T3_json_flag"       "--json"
assert_match "T3_skip_reset_flag" "--skip-reset"

# T4 — anti-workaround grep guards present (the 4 manual workarounds from T6.1)
assert_match "T4_anti_wa_appproject_patch" "kubectl\\[\\^\\|\\]\\+patch\\[\\[:space:\\]\\]\\+appproject"
assert_match "T4_anti_wa_create_namespace" "kubectl\\[\\^\\|\\]\\+create\\[\\[:space:\\]\\]\\+namespace\\[\\[:space:\\]\\]\\+loopzero-"
assert_match "T4_anti_wa_sa_copy"          "sa\\[\\[:space:\\]\\]\\+loopzero-app.\\*apply\\[\\[:space:\\]\\]\\+-f"
assert_match "T4_anti_wa_argocd_force_sync" "patch\\[\\[:space:\\]\\]\\+app\\[\\[:space:\\]\\]\\+.\\*sync.\\*revision"

# T5 — self-grep anti-workaround ALWAYS executed (before --skip-live branch)
# In main(), assert_no_workarounds appears BEFORE the SKIP_LIVE check.
SCRIPT_BODY="$(grep -vE '^[[:space:]]*#' "$TARGET")"
ANTI_LINE="$(echo "$SCRIPT_BODY" | grep -n 'assert_no_workarounds' | head -1 | cut -d: -f1)"
SKIP_LINE="$(echo "$SCRIPT_BODY" | grep -n 'SKIP_LIVE.*-eq 1' | head -1 | cut -d: -f1)"
if [ -n "$ANTI_LINE" ] && [ -n "$SKIP_LINE" ] && [ "$ANTI_LINE" -lt "$SKIP_LINE" ]; then
  PASS=$((PASS+1)); echo "  PASS  T5_anti_workaround_runs_before_skip_live_branch"
else
  FAIL=$((FAIL+1)); FAILED+=("T5_anti_workaround_runs_before_skip_live_branch"); echo "  FAIL  T5_anti_workaround_runs_before_skip_live_branch"
fi

# T6 — 20min wall-clock budget cross-referenced
assert_match "T6_budget_20min" "MAX_DURATION_SECONDS.*1200"

# T7 — reuses canonical primitives (no inline DB/Kratos/repo logic)
assert_match "T7_reuses_reset_for_e2e"          "infra/scripts/local/reset-for-e2e.sh"
assert_match "T7_reuses_seed_admin"             "infra/scripts/local/seed-admin-loopzero-dev.sh"
assert_match "T7_reuses_connect_canonical"      "infra/scripts/local/connect-canonical-fixture.sh"

# T8 — CRITICAL: anti-workaround patterns must NOT appear in the script's
# functional body (only in the assert_no_workarounds grep guard itself OR
# in comments). Verifies the script is canonical-by-construction.
# We strip the comments + the assert function block before grepping.
NON_COMMENT_NON_ASSERT="$(awk '
  BEGIN{skip=0}
  /^assert_no_workarounds\(\) \{/ {skip=1}
  /^\}/ && skip==1 {skip=0; next}
  skip==1 {next}
  /^[[:space:]]*#/ {next}
  {print}
' "$TARGET")"
if echo "$NON_COMMENT_NON_ASSERT" | grep -qE 'kubectl[^|]+patch[[:space:]]+appproject'; then
  FAIL=$((FAIL+1)); FAILED+=("T8_no_appproject_patch_in_body")
  echo "  FAIL  T8_no_appproject_patch_in_body — actual kubectl patch appproject call found in script body"
else
  PASS=$((PASS+1)); echo "  PASS  T8_no_appproject_patch_in_body"
fi
if echo "$NON_COMMENT_NON_ASSERT" | grep -qE 'kubectl[^|]+create[[:space:]]+namespace[[:space:]]+loopzero-'; then
  FAIL=$((FAIL+1)); FAILED+=("T8b_no_create_namespace_in_body")
  echo "  FAIL  T8b_no_create_namespace_in_body"
else
  PASS=$((PASS+1)); echo "  PASS  T8b_no_create_namespace_in_body"
fi

# T9 — allowlist entry
ALLOWLIST="$REPO_ROOT/../.claude/rules/shell-scripts-allowlist.md"
if [ -f "$ALLOWLIST" ] && grep -q "dev-public-production-deploy_test.sh" "$ALLOWLIST"; then
  PASS=$((PASS+1)); echo "  PASS  T9_allowlist_entry_present"
else
  FAIL=$((FAIL+1)); FAILED+=("T9_allowlist_entry_present"); echo "  FAIL  T9_allowlist_entry_present (run: add allowlist row)"
fi

# T10 — companion hermetic test allowlist entry
if [ -f "$ALLOWLIST" ] && grep -q "check-dev-public-production-deploy-syntax_test.sh" "$ALLOWLIST"; then
  PASS=$((PASS+1)); echo "  PASS  T10_hermetic_allowlist_entry_present"
else
  FAIL=$((FAIL+1)); FAILED+=("T10_hermetic_allowlist_entry_present"); echo "  FAIL  T10_hermetic_allowlist_entry_present"
fi

echo
echo "=== Summary ==="
echo "PASS: $PASS"
echo "FAIL: $FAIL"
if (( FAIL > 0 )); then
  echo
  echo "Failed cases:"
  printf '  - %s\n' "${FAILED[@]}"
  exit 1
fi
exit 0
