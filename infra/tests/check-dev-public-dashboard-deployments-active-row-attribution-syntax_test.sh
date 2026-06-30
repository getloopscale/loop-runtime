#!/usr/bin/env bash
# Hermetic structural guard for the LIVE bash that closes #121.
# NO network, NO kubectl — pure grep + bash -n on LIVE script source.

set -euo pipefail

LIVE="infra/tests/e2e/dev-public-dashboard-deployments-active-row-attribution_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"

pass=0; fail=0; fails=()
assert() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then
    pass=$((pass + 1)); printf '  PASS  %s\n' "$name"
  else
    fail=$((fail + 1)); fails+=("$name"); printf '  FAIL  %s\n' "$name"
  fi
}

cd "$(git rev-parse --show-toplevel 2>/dev/null || dirname "$(dirname "$(readlink -f "$0")")" )" 2>/dev/null || true

assert "file_exists" test -f "$LIVE"
[ -f "$LIVE" ] || { printf 'RESULT: missing file\n' >&2; exit 1; }

assert "bash_syntax" bash -n "$LIVE"
assert "strict_mode" grep -q '^set -euo pipefail' "$LIVE"
assert "issue_121_cited" grep -q 'loopzero-cloud#121' "$LIVE"
assert "operator_hard_stop" grep -q 'OPERATOR HARD STOP' "$LIVE"
assert "cleanroom_mandate_cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE"
assert "feedback_e2e_cited" grep -q 'feedback_issue_fix_requires_failing_e2e_test' "$LIVE"

assert "flag_skip_live" grep -q -- '--skip-live' "$LIVE"
assert "flag_help" grep -q -- '--help' "$LIVE"
assert "flag_json" grep -q -- '--json' "$LIVE"
assert "flag_scenario" grep -q -- '--scenario=' "$LIVE"

assert "scenario_a_function" grep -q '^scenario_a()' "$LIVE"
assert "scenario_b_function" grep -q '^scenario_b()' "$LIVE"
assert "scenario_c_function" grep -q '^scenario_c()' "$LIVE"

# Domain-specific invariants
assert "per_env_status_probe" grep -q 'status?env=preview' "$LIVE"
assert "per_env_preview_signal_check" grep -q 'env:"preview"' "$LIVE"
assert "environments_probe" grep -q '/engine/api/v1/environments' "$LIVE"
assert "bundle_grep_proof" grep -q 'deployments-' "$LIVE"

assert "no_co_authored" bash -c "! grep -q 'Co-Authored-By' '$LIVE'"
assert "no_no_verify" bash -c "! grep -q -- '--no-verify' '$LIVE'"

if [ -f "$ALLOWLIST" ]; then
  assert "allowlist_live_entry" grep -q "dev-public-dashboard-deployments-active-row-attribution_test.sh" "$ALLOWLIST"
else
  fail=$((fail + 1)); fails+=("allowlist_live_entry (file missing)")
fi

printf '\n============================================================\n'
printf 'RESULT: %d PASS, %d FAIL\n' "$pass" "$fail"
if [ "${#fails[@]}" -gt 0 ]; then printf 'Failed:\n'; printf '  - %s\n' "${fails[@]}"; fi
printf '============================================================\n'

[ "$fail" -eq 0 ]
