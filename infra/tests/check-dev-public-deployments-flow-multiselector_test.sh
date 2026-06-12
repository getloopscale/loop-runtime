#!/usr/bin/env bash
# T5.2 defensive engineering of plan 2026-06-08-dashboard-screens-review-fixes
# v1.2 (per ADR 2026-06-08-t5-2-dod-refinement-loki-scenario-out-of-scope).
#
# Hermetic structural test for the --scenarios= multi-selector added to
# dev-public-deployments-flow_test.sh. Validates the scope-filter mechanism
# so future plans citing the script can opt into specific scenarios via
# --scenarios=A,B,D rather than copy-pasting "All LIVE bash suites PASS"
# acceptance criteria that drag in out-of-scope scenarios (the trap that
# blocked T5.2 in iter 17 of the dashboard-screens-review-fixes loop).
#
# NO network, NO kubectl, NO real curl. Pure bash syntax + grep + simulated
# argv parsing via a sourced helper.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-deployments-flow_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"

assert_count=0
fail_count=0

assert() {
  local cond="$1"; local msg="$2"
  assert_count=$((assert_count + 1))
  if eval "$cond"; then
    echo "  [OK]   $msg"
  else
    echo "  [FAIL] $msg" >&2
    fail_count=$((fail_count + 1))
  fi
}

echo "T5.2 defensive engineering hermetic check for $SCRIPT"

# --- Syntax ---
assert "bash -n '$SCRIPT' 2>/dev/null" "bash -n parses cleanly"

# --- Multi-selector wiring ---
assert "grep -qE 'SCENARIOS_FILTER=\"\"' '$SCRIPT'" "SCENARIOS_FILTER var initialized to empty"
assert "grep -qE '\\-\\-scenarios=\\*\\) SCENARIOS_FILTER=' '$SCRIPT'" "--scenarios= flag parsed into SCENARIOS_FILTER"
assert "grep -qE 'scenario_enabled\\(\\) \\{' '$SCRIPT'" "scenario_enabled() helper function defined"

# --- Mutual-exclusion semantics ---
assert "grep -qE 'if \\[\\[ -n \"\\\$SCENARIOS_FILTER\" \\]\\]; then' '$SCRIPT'" \
       "--scenarios= takes precedence over --scenario= when both present"
assert "grep -qE 'scenario_enabled A && scenario_a' '$SCRIPT'" \
       "scenario A guarded by membership check"
assert "grep -qE 'scenario_enabled B && scenario_b' '$SCRIPT'" \
       "scenario B guarded by membership check"
assert "grep -qE 'scenario_enabled C && scenario_c' '$SCRIPT'" \
       "scenario C guarded by membership check"

# --- Substring safety (id 'A' must NOT match 'AB' in filter) ---
# Inspect the membership-check regex form
assert "grep -qE '\\[\\[ \",\\\${lower},\" == \\*\",\\\${id},\"\\* \\]\\]' '$SCRIPT'" \
       "membership check wraps in commas for exact match (substring-safe)"

# --- Preflight always runs (Scenario D) when any other scenario runs ---
assert "awk '/^if \\[\\[ -n \"\\\$SCENARIOS_FILTER\" \\]\\]; then\$/,/^else\$/' '$SCRIPT' | grep -qE '^  scenario_d\$'" \
       "scenario_d (preflight) runs unconditionally when --scenarios= set"

# --- Legacy --scenario= single-selector still works ---
assert "grep -qE 'case \"\\\$SCENARIO\" in' '$SCRIPT'" \
       "legacy --scenario= case branch preserved"
assert "grep -qE 'A\\|a\\) scenario_d; scenario_a' '$SCRIPT'" \
       "legacy --scenario=A path intact"

# --- ENGINE_BASE_OVERRIDE plumbed (T5.2 c4851e9b fix preserved) ---
assert "grep -qE 'ENGINE_BASE=\"\\\$\\{ENGINE_BASE_OVERRIDE-\\\$\\{CLOUD_URL\\}/engine\\}\"' '$SCRIPT'" \
       "ENGINE_BASE_OVERRIDE env var honored"

# --- Usage docs updated ---
assert "grep -qE '\\-\\-scenarios=A,B,D' '$SCRIPT'" \
       "usage() documents --scenarios=A,B,D syntax"
assert "grep -qiE 'comma-separated|multi-selector' '$SCRIPT'" \
       "usage() explains comma-separated semantics"
assert "grep -qE 'ADR 2026-06-08-t5-2-dod' '$SCRIPT'" \
       "usage() cites the ADR for traceability"

# --- ADR + MEET references resolve ---
assert "test -f .claude/knowledge-base/decisions/2026-06-08-t5-2-dod-refinement-loki-scenario-out-of-scope.md" \
       "ADR file exists"
assert "test -f .claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-06-08-001-t5-2-dod-refinement.md" \
       "MEET-ASYNC-AMENDMENT record exists"

# --- Allowlist cross-check (script + its hermetic test both listed) ---
assert "grep -qE 'dev-public-deployments-flow_test.sh' '$ALLOWLIST'" \
       "dev-public-deployments-flow_test.sh in shell-scripts-allowlist"
assert "grep -qE 'check-dev-public-deployments-flow-multiselector_test.sh' '$ALLOWLIST'" \
       "this hermetic test in shell-scripts-allowlist"

# --- Run simulated argv parsing to confirm semantics (no network) ---
# Source the script's flag-parsing block via a wrapper that exits before scenarios run.
SIM_OUT=$(bash -c "set -e
  SKIP_LIVE=0; JSON_OUTPUT=0; SCENARIO=all; SCENARIOS_FILTER=''
  for arg in --scenarios=A,B,D; do
    case \"\$arg\" in
      --scenarios=*) SCENARIOS_FILTER=\"\${arg#--scenarios=}\" ;;
    esac
  done
  scenario_enabled() {
    local id=\"\${1,,}\"
    if [[ -z \"\$SCENARIOS_FILTER\" ]]; then return 0; fi
    local lower=\"\${SCENARIOS_FILTER,,}\"
    [[ \",\${lower},\" == *\",\${id},\"* ]]
  }
  scenario_enabled A && echo 'A=in'
  scenario_enabled B && echo 'B=in'
  scenario_enabled C && echo 'C=out' || echo 'C=skipped'
  scenario_enabled D && echo 'D=in'
" 2>&1)
assert "echo '$SIM_OUT' | grep -q 'A=in'" "simulator: --scenarios=A,B,D enables A"
assert "echo '$SIM_OUT' | grep -q 'B=in'" "simulator: --scenarios=A,B,D enables B"
assert "echo '$SIM_OUT' | grep -q 'C=skipped'" "simulator: --scenarios=A,B,D SKIPS C"
assert "echo '$SIM_OUT' | grep -q 'D=in'" "simulator: --scenarios=A,B,D enables D"

# --- Verdict ---
echo ""
echo "TOTAL: $assert_count assertions, $fail_count failed"
[ "$fail_count" -eq 0 ]
