#!/usr/bin/env bash
# check-dev-public-promote-no-op-syntax_test.sh
#
# T1.1 of plan loopzero-promote-no-op-fail-loud-p0 v1.0.
# Hermetic structural guard for the sibling LIVE bash. NO network, NO kubectl.
# Pure grep + bash -n on LIVE script source.

set -euo pipefail

LIVE="infra/tests/e2e/dev-public-promote-no-op_test.sh"
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
if [ ! -f "$LIVE" ]; then
  printf '\nRESULT: 1 file missing — other assertions skipped\n' >&2
  exit 1
fi

assert "bash_syntax" bash -n "$LIVE"
assert "strict_mode" grep -q '^set -euo pipefail' "$LIVE"
assert "issue_120_cited" grep -q 'loopzero-cloud#120' "$LIVE"
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

assert "asserts_release_id_regex" grep -q 'rel-\[0-9a-f\]{16}' "$LIVE"
assert "asserts_forbidden_loopzero_local" grep -q 'loopzero\.local' "$LIVE"
assert "asserts_expected_public_domain" grep -q 'prod-dev\.loopzero\.dev' "$LIVE"

assert "asserts_sha_change" grep -q 'sha changed\|sha_post\|last_deploy_sha' "$LIVE"

assert "no_co_authored" bash -c "! grep -q 'Co-Authored-By' '$LIVE'"
assert "no_no_verify" bash -c "! grep -q -- '--no-verify' '$LIVE'"

if [ -f "$ALLOWLIST" ]; then
  assert "allowlist_live_entry" grep -q "dev-public-promote-no-op_test.sh" "$ALLOWLIST"
else
  fail=$((fail + 1)); fails+=("allowlist_live_entry (file missing)")
fi

printf '\n============================================================\n'
printf 'RESULT: %d PASS, %d FAIL\n' "$pass" "$fail"
if [ "${#fails[@]}" -gt 0 ]; then
  printf 'Failed:\n'
  printf '  - %s\n' "${fails[@]}"
fi
printf '============================================================\n'

[ "$fail" -eq 0 ]
