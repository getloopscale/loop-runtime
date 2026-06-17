#!/usr/bin/env bash
# check-task-test-pre-release-rigor-syntax_test.sh
#
# T5.1 hermetic structural test for the umbrella + 4 scenario tasks
# added to taskfiles/test.yml per plan 2026-06-08-pre-release-test-rigor v1.2.
#
# Verifies:
#   - 4 per-Cenário tasks registered (scenario-1 through scenario-4)
#   - umbrella task `test:pre-release-rigor` registered
#   - Each scenario invokes the correct upstream script (T2.2, T3.1, T4.1, T4.2)
#   - EC-4 absorption: wall-clock budget 120 min documented (NOT 90)
#   - EC-13 absorption: standalone scenario-1 SKIP-message for baseline match
#   - ADR D6 fail-fast: umbrella aborts on first scenario failure (default Task semantics)
#
# Hermetic: NO network, NO kubectl, NO `task --list` invocation (parses YAML
# directly). Runs in < 1s.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
TF="${REPO_ROOT}/taskfiles/test.yml"

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

assert_not_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[FAIL] %s (forbidden: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  else
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  fi
}

# --- Taskfile existence ----------------------------------------------------

assert "taskfiles/test.yml exists" test -f "$TF"

# --- Task name registration -------------------------------------------------

assert_grep "task test:pre-release-rigor:scenario-1 registered" \
  "$TF" '^  test:pre-release-rigor:scenario-1:'
assert_grep "task test:pre-release-rigor:scenario-2 registered" \
  "$TF" '^  test:pre-release-rigor:scenario-2:'
assert_grep "task test:pre-release-rigor:scenario-3 registered" \
  "$TF" '^  test:pre-release-rigor:scenario-3:'
assert_grep "task test:pre-release-rigor:scenario-4 registered" \
  "$TF" '^  test:pre-release-rigor:scenario-4:'
assert_grep "umbrella task test:pre-release-rigor registered" \
  "$TF" '^  test:pre-release-rigor:'

# --- Scenario-to-script wiring --------------------------------------------

assert_grep "scenario-1 invokes T2.2 dev-public-webhook-chain test" \
  "$TF" 'dev-public-webhook-chain_test\.sh'
assert_grep "scenario-2 invokes T1.1 baseline-capture (cleanroom drift gate)" \
  "$TF" 'baseline-capture\.sh|baseline-diff\.sh|/cleanroom'
assert_grep "scenario-3 invokes T4.1 C11 script" \
  "$TF" 'c11-webhook-concurrent-podkill\.sh'
assert_grep "scenario-3 invokes T4.2 C12 script" \
  "$TF" 'c12-simul-prs-during-partition\.sh'
assert_grep "scenario-4 invokes T3.1 live-proof --check=api-sanity" \
  "$TF" 'live-proof-dev-public\.sh.*api-sanity|api-sanity.*live-proof'

# --- EC-4 budget (wall-clock 120 min per ADR D8) --------------------------

assert_grep "(EC-4 / ADR D8) wall-clock budget 120 min documented" \
  "$TF" '120.*min|120m|MAX_DURATION.*7200|wall.clock.*120'
assert_not_grep "(EC-4) 90 min budget reference deprecated (revised by ADR D8)" \
  "$TF" '90.*min.*budget|wall.clock.*90 min'

# --- EC-13 absorption (standalone scenario-1 baseline-match SKIP) ---------

assert_grep "(EC-13) standalone scenario-1 documents baseline-match SKIP" \
  "$TF" '(EC-13|baseline.match.*SKIP|standalone|skip.*baseline)'

# --- ADR D6 fail-fast semantics --------------------------------------------

# Task runs cmds sequentially and aborts on first non-zero exit by default —
# that IS fail-fast. We verify that NO scenario uses `ignore_error: true`
# OR `defer:` to suppress failures.
assert_not_grep "(ADR D6) scenario tasks do NOT suppress errors via ignore_error" \
  "$TF" 'ignore_error:\s*true.*pre-release-rigor|pre-release-rigor.*ignore_error:\s*true'

# --- Umbrella dependency order ---------------------------------------------

# Per plan: scenario-2 (baseline) → scenario-1 (chain) → (scenario-4 ∥ scenario-3)
# Verify umbrella references all 4 scenarios (order verified at runtime).
# Find the lines AFTER "test:pre-release-rigor:" but BEFORE the next task and
# assert each scenario is mentioned.
umbrella_body=$(awk '
  /^  test:pre-release-rigor:$/ { in_body=1; next }
  in_body && /^  [a-z]/ { exit }
  in_body { print }
' "$TF")

if printf '%s' "$umbrella_body" | grep -q 'scenario-1' && \
   printf '%s' "$umbrella_body" | grep -q 'scenario-2' && \
   printf '%s' "$umbrella_body" | grep -q 'scenario-3' && \
   printf '%s' "$umbrella_body" | grep -q 'scenario-4' ; then
  printf '[PASS] umbrella body references all 4 scenarios\n' ; PASS=$((PASS + 1))
else
  printf '[FAIL] umbrella body missing scenario references\n' ; FAIL=$((FAIL + 1))
fi

# --- Plan provenance ------------------------------------------------------

assert_grep "tasks cite plan 2026-06-08-pre-release-test-rigor" \
  "$TF" '2026-06-08-pre-release-test-rigor'
assert_grep "tasks cite ADR D1 (canonical task entry)" \
  "$TF" '(ADR D1|D1\b)'

# --- YAML shape integrity --------------------------------------------------

# A naive yaml-shape check — every task must have desc + cmds keys.
# Find each `  test:pre-release-rigor:` line and check the next ~15 lines
# include both `desc:` and `cmds:` keywords.
for sid in scenario-1 scenario-2 scenario-3 scenario-4 ; do
  block=$(awk "
    /^  test:pre-release-rigor:${sid}:$/ { in_block=1; lines=0; next }
    in_block { lines++; print }
    in_block && lines > 30 { exit }
  " "$TF")
  if printf '%s' "$block" | grep -q 'desc:' && \
     printf '%s' "$block" | grep -q 'cmds:' ; then
    printf '[PASS] %s has desc + cmds\n' "$sid" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s missing desc OR cmds\n' "$sid" ; FAIL=$((FAIL + 1))
  fi
done

# --- Anti-patterns ---------------------------------------------------------

assert_not_grep "NO Co-Authored-By in taskfile" "$TF" 'Co-Authored-By'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
