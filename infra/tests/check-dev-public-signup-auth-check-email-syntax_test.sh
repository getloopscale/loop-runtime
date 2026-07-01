#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash at
# infra/tests/e2e/dev-public-signup-auth-check-email_test.sh — closes
# Red Team loopzero/loopzero#162 (BLOCKER P0: post-signup redirect to
# /auth/check-email returns 404 Caddy raw; funil de onboarding quebrado).
#
# Also validates the SPA route registration + Caddyfile named matcher fix:
# - app.tsx registers /auth/check-email + /auth/verify SPA routes
# - Caddyfile @auth_bff matcher restricts handle to 4 BFF routes only
#
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-signup-auth-check-email_test.sh"
DASHBOARD_APP="../loopzero-cloud/dashboard/src/app.tsx"
DASHBOARD_CHECKEMAIL="../loopzero-cloud/dashboard/src/pages/check-email.tsx"
CADDYFILE="infra/local/Caddyfile"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
PASS=0
FAIL=0

assert() {
  local label="$1"; shift
  if "$@"; then
    PASS=$((PASS+1))
    echo "OK $label"
  else
    FAIL=$((FAIL+1))
    echo "FAIL $label"
  fi
}

# --- Section A: LIVE bash script exists + is well-formed ---
assert "A1 LIVE script exists"                    test -f "$SCRIPT"
assert "A2 bash -n syntax clean"                  bash -n "$SCRIPT"
assert "A3 strict mode set -euo pipefail"         grep -q '^set -euo pipefail' "$SCRIPT"
assert "A4 issue #162 cited in header"            grep -q "#162" "$SCRIPT"
assert "A5 slug signup-auth-check-email cited"    grep -q "signup-auth-check-email" "$SCRIPT"

# --- Section B: scenarios A + B + preflight declared ---
assert "B1 scenario_a defined"                    grep -q '^scenario_a()' "$SCRIPT"
assert "B2 scenario_b defined"                    grep -q '^scenario_b()' "$SCRIPT"
assert "B3 preflight() defined"                   grep -q '^preflight()' "$SCRIPT"

# --- Section C: flags wired ---
assert "C1 --skip-live flag"                      grep -q -- "--skip-live" "$SCRIPT"
assert "C2 --scenario= flag"                      grep -q -- "--scenario=" "$SCRIPT"
assert "C3 --json flag"                           grep -q -- "--json" "$SCRIPT"
assert "C4 --help flag"                           grep -q -- "--help" "$SCRIPT"

# --- Section D: OPERATOR HARD STOP + cleanroom mandate + INQUEBRÁVEL feedback cited ---
assert "D1 OPERATOR HARD STOP header"             grep -q "OPERATOR HARD STOP" "$SCRIPT"
assert "D2 cleanroom-up-live-test-mandate cited"  grep -q "cleanroom-up-live-test-mandate" "$SCRIPT"
assert "D3 feedback_issue_fix cited"              grep -q "feedback_issue_fix_requires_failing_e2e_test_2026_06_25" "$SCRIPT"

# --- Section E: canonical probes cited ---
assert "E1 /auth/check-email endpoint probed"     grep -q "/auth/check-email" "$SCRIPT"
assert "E2 /auth/verify endpoint probed"          grep -q "/auth/verify" "$SCRIPT"
assert "E3 app-dev.loopzero.example canonical URL"     grep -q "app-dev.loopzero.example" "$SCRIPT"

# --- Section F: SPA fix landed in dashboard app.tsx ---
assert "F1 dashboard/src/app.tsx exists"          test -f "$DASHBOARD_APP"
assert "F2 /auth/check-email SPA route registered" grep -Fq '/auth/check-email' "$DASHBOARD_APP"
assert "F3 /auth/verify SPA route registered"     grep -Fq '/auth/verify' "$DASHBOARD_APP"
assert "F4 CheckEmailPage new component exists"   test -f "$DASHBOARD_CHECKEMAIL"

# --- Section G: Caddyfile @auth_bff named matcher landed (BFF only) ---
assert "G1 Caddyfile @auth_bff matcher declared"  grep -q '@auth_bff' "$CADDYFILE"
assert "G2 handle @auth_bff block exists"         grep -q 'handle @auth_bff' "$CADDYFILE"

# --- Section H: exit code + governance + allowlist ---
assert "H1 exit code reflects FAIL"               grep -Fq 'FAIL_COUNT > 0 ? 1 : 0' "$SCRIPT"

# H2 + H3 use direct grep -q (not the assert helper — bash '!' doesn't parse
# well when the helper spans a subshell pipeline).
if grep -Fq 'Co-Authored-By' "$SCRIPT"; then
  FAIL=$((FAIL+1)); echo "FAIL H2 no Co-Authored-By trailer"
else
  PASS=$((PASS+1)); echo "OK H2 no Co-Authored-By trailer"
fi
if grep -Fq -- '--no-verify' "$SCRIPT"; then
  FAIL=$((FAIL+1)); echo "FAIL H3 no --no-verify bypass"
else
  PASS=$((PASS+1)); echo "OK H3 no --no-verify bypass"
fi

assert "H4 LIVE script in allowlist"              grep -Fq "$SCRIPT" "$ALLOWLIST"
assert "H5 hermetic guard in allowlist"           grep -Fq "check-dev-public-signup-auth-check-email-syntax_test.sh" "$ALLOWLIST"

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
