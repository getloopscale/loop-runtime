#!/usr/bin/env bash
# T4.4 hermetic structural companion for dev-public-login-error-states_test.sh.
# Plan: 2026-06-16-m0-j01-login-polish v1.1, Phase 4 Task T4.4.
# Asserts the LIVE script honors the canonical triplet contract per
# blueprint Q3 + EC absorptions documented in plan v1.1.

set -euo pipefail

LIVE_SCRIPT="${LIVE_SCRIPT:-infra/tests/e2e/dev-public-login-error-states_test.sh}"
PASS=0
FAIL=0

assert() {
  local name="$1" cond="$2"
  if eval "$cond" >/dev/null 2>&1; then
    PASS=$((PASS+1))
    echo "  PASS: $name"
  else
    FAIL=$((FAIL+1))
    echo "  FAIL: $name"
  fi
}

# A. Syntax + executability
assert "bash -n clean" "bash -n '$LIVE_SCRIPT'"
assert "shebang present" "head -1 '$LIVE_SCRIPT' | grep -q '^#!/usr/bin/env bash'"
assert "set -euo pipefail" "grep -q 'set -euo pipefail' '$LIVE_SCRIPT'"

# B. Scenarios A-E declared
for s in A_github_oidc_redirect B_google_oidc_redirect C_dashboard_login_page_reachable D_isenumeration_collision_shipped E_network_timeout_simulation; do
  assert "scenario_$s function present" "grep -q 'scenario_$s' '$LIVE_SCRIPT'"
done

# C. Pre-flight gate + cleanup trap
assert "preflight function present" "grep -q '^preflight()' '$LIVE_SCRIPT'"
assert "cleanup trap on EXIT/INT/TERM" "grep -q 'trap cleanup EXIT INT TERM' '$LIVE_SCRIPT'"

# D. Flags
assert "--skip-live flag wired" "grep -q '\\-\\-skip-live' '$LIVE_SCRIPT'"
assert "--json flag wired" "grep -q '\\-\\-json' '$LIVE_SCRIPT'"
assert "--scenario flag wired" "grep -q '\\-\\-scenario' '$LIVE_SCRIPT'"
assert "--help flag wired" "grep -q '\\-\\-help' '$LIVE_SCRIPT'"

# E. EC absorption citations in script header (audit trail)
assert "EC-2 OWASP A01 cited" "grep -q 'EC-2 OWASP A01' '$LIVE_SCRIPT'"
assert "EC-3 OWASP A01 cited" "grep -q 'EC-3 OWASP A01' '$LIVE_SCRIPT'"
assert "EC-J01-1 cited" "grep -q 'EC-J01-1' '$LIVE_SCRIPT'"
assert "EC-5 application-timeout cited" "grep -q 'EC-5' '$LIVE_SCRIPT'"

# F. Exit code reflects FAIL
assert "exit code reflects FAIL count" "grep -q 'TOTAL_FAIL.*== \"0\".*exit 0' '$LIVE_SCRIPT'"

echo ""
echo "[check-syntax] $PASS PASS / $FAIL FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
