#!/usr/bin/env bash
# check-cleanroom-baseline-integration_test.sh
#
# T1.2 hermetic test for the cleanroom skill's integration of
# baseline-capture.sh + baseline-diff.sh per plan
# 2026-06-08-pre-release-test-rigor v1.2.
#
# The cleanroom skill (.claude/skills/cleanroom/SKILL.md) is operator-invoked
# via Claude — the artifact under test is the markdown contract that documents
# the flow. This hermetic test verifies the SKILL.md declares:
#   - Phase 0 pre-rebuild baseline capture (before destroy)
#   - Phase 5 post-rebuild baseline capture + diff gate
#   - --skip-baseline flag for back-compat
#   - exit-1 escalation when diff verdict = FAIL
#
# Hermetic: NO network, NO kubectl. Pure grep on SKILL.md.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
SKILL="${REPO_ROOT}/.claude/skills/cleanroom/SKILL.md"

PASS=0
FAIL=0

assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s\n' "$msg" ; FAIL=$((FAIL + 1))
  fi
}

assert_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s (pattern: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  fi
}

# --- Skill existence + structure -------------------------------------------

assert "cleanroom SKILL.md exists" test -f "$SKILL"

# --- Phase 0 (pre-rebuild snapshot) ----------------------------------------

assert_grep "Phase 0 pre-rebuild baseline section declared" \
  "$SKILL" '^###\s+Fase\s+0\s+[—\-]\s+(Baseline\s+pre-rebuild|Pre-rebuild)'
assert_grep "Phase 0 invokes baseline-capture.sh" \
  "$SKILL" 'baseline-capture\.sh'
assert_grep "Phase 0 writes pre snapshot path" \
  "$SKILL" '(baseline-pre|snap-pre|baseline-pre-rebuild)\.json'

# --- Phase 5 (post-rebuild snapshot + diff gate) ----------------------------

assert_grep "Phase 5 post-rebuild baseline section declared" \
  "$SKILL" '^###\s+Fase\s+5\s+[—\-]\s+(Baseline\s+post-rebuild|Post-rebuild)'
assert_grep "Phase 5 invokes baseline-capture.sh (post)" \
  "$SKILL" '(baseline-post|snap-post|baseline-post-rebuild)\.json'
assert_grep "Phase 5 invokes baseline-diff.sh as gate" \
  "$SKILL" 'baseline-diff\.sh'
assert_grep "Phase 5 declares exit 1 on diff FAIL verdict" \
  "$SKILL" '(exit\s+1|verdict.*FAIL|aborta.*diff|FAIL.*aborta)'

# --- --skip-baseline flag back-compat ---------------------------------------

assert_grep "--skip-baseline flag documented (back-compat)" \
  "$SKILL" '\-\-skip-baseline'
assert_grep "--skip-baseline rationale explained" \
  "$SKILL" '(back-compat|legacy|escape hatch|pula|skip)'

# --- Plan reference ---------------------------------------------------------

assert_grep "cites plan 2026-06-08-pre-release-test-rigor (provenance)" \
  "$SKILL" 'pre-release-test-rigor'
assert_grep "cites T1.2 task ID (traceability)" \
  "$SKILL" 'T1\.2'

# --- Phase numbering integrity (Phases 1-4 still documented) ---------------

assert_grep "Phase 1 (destroy) preserved"    "$SKILL" '^###\s+Fase\s+1\s'
assert_grep "Phase 2 (recreate) preserved"   "$SKILL" '^###\s+Fase\s+2\s'
assert_grep "Phase 3 (dev loop) preserved"   "$SKILL" '^###\s+Fase\s+3\s'
assert_grep "Phase 4 (health check) preserved" "$SKILL" '^###\s+Fase\s+4\s'

# --- Anti-patterns ---------------------------------------------------------

# The skill must NOT recommend skipping the baseline diff "for speed".
if grep -E 'pula.*baseline.*por.*tempo|skip.*baseline.*for.*speed|fast.*mode.*sem.*baseline' "$SKILL" > /dev/null 2>&1 ; then
  printf '[FAIL] anti-pattern: skill recommends skipping baseline for speed\n'
  FAIL=$((FAIL + 1))
else
  printf '[PASS] anti-pattern absent: skill does NOT recommend skipping baseline for speed\n'
  PASS=$((PASS + 1))
fi

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
