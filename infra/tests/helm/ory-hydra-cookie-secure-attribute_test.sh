#!/bin/bash
# Hermetic test: ory-hydra cookie Secure attribute via hydra.dev: false in dev-public overlay.
#
# T1.2 of plan loopzero-cloud-14-device-flow-csrf-secure-fix v3.0 (closes #14).
#
# Per blueprint hydra-v26-upgrade-canonical Q2: removing --dev from chart args
# activates the production-mode-always-Secure cookie semantic (driver/config/provider.go:
# CookieSecure returns true unconditionally when !IsDevelopmentMode). The chart template
# in infra/helm/ory-hydra/templates/deployment.yaml.gotmpl appends --dev to container.args
# IFF .Values.hydra.dev == true. Therefore setting hydra.dev: false in dev-public overlay
# removes --dev from rendered manifest → activates production cookie semantic.
#
# This test runs WITHOUT a live cluster: pure source-level grep on the overlay file.
# A LIVE counterpart asserts the cookie attribute via curl probe (e2e dev-public-device-flow_test.sh
# Scenario E, also added by this plan).
#
# Pattern follows ory-hydra-canonical-urls_test.sh (T3.1 of plan
# 2026-06-11-cleanroom-canonical-5-defense-layers v1.2).

set -uo pipefail  # NOT -e — assertions intentionally test failure conditions

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
OVERLAY="$REPO_ROOT/infra/helmfile/values/dev-public/ory-hydra.yaml.gotmpl"
CHART_DEFAULT="$REPO_ROOT/infra/helm/ory-hydra/values.yaml.gotmpl"

FAIL=0
PASS=0

assert() {
  local label="$1"; local result="$2"
  if [ "$result" -eq 0 ]; then
    echo "  ✓ PASS  $label"
    PASS=$((PASS+1))
  else
    echo "  ✗ FAIL  $label"
    FAIL=$((FAIL+1))
  fi
}

echo "=== ory-hydra-cookie-secure-attribute_test.sh ==="
echo "Overlay: $OVERLAY"
echo "Chart default: $CHART_DEFAULT"
echo ""

# A1: dev-public overlay file exists.
[ -f "$OVERLAY" ]; assert "A1: dev-public overlay file present at canonical path" $?

# A2: overlay declares hydra.dev: false explicitly (the canonical production-posture knob).
grep -E '^[[:space:]]+dev:[[:space:]]+false[[:space:]]*(#.*)?$' "$OVERLAY" >/dev/null
assert "A2: overlay declares 'hydra.dev: false' (production-mode-always-Secure cookie semantic)" $?

# A3: overlay does NOT declare 'dev: true' anywhere (regression guard against accidental revert).
! grep -E '^[[:space:]]+dev:[[:space:]]+true[[:space:]]*(#.*)?$' "$OVERLAY" >/dev/null
assert "A3: overlay does NOT declare 'dev: true' (anti-regression guard)" $?

# A4: existing canonical URLs preserved (catches #14/#15/#16 from T6.2 session — must not regress).
grep -E '^[[:space:]]+issuer:[[:space:]]+"https://auth-dev\.loopzero\.dev/"$' "$OVERLAY" >/dev/null
assert "A4: existing urls.self.issuer canonical URL preserved (regression guard)" $?

grep -E 'verification:[[:space:]]+"https://app-dev\.loopzero\.dev/device-verify"$' "$OVERLAY" >/dev/null
assert "A5: existing urls.device.verification canonical URL preserved (regression guard)" $?

# A6: chart-default values file still declares dev: true (standalone-multi local k3d HTTP issuer needs it).
# This invariant ensures the fix is ISOLATED to dev-public overlay, NOT a global chart change.
[ -f "$CHART_DEFAULT" ] && grep -E '^[[:space:]]+dev:[[:space:]]+true[[:space:]]*(#.*)?$' "$CHART_DEFAULT" >/dev/null
assert "A6: chart-default still has 'dev: true' (standalone-multi local k3d HTTP issuer preserved)" $?

# A7: overlay file MUST cite the parent plan v3.0 in its top-of-file documentation OR include the audit trail comment.
# This is a process-discipline guard: future readers should be able to trace the dev:false change.
grep -E 'loopzero-cloud-14|cookie.*Secure|hydra-v26-upgrade-canonical|GHSA-r9w3' "$OVERLAY" >/dev/null
assert "A7: overlay documents the loopzero-cloud#14 fix audit trail in comments" $?

# A8: the ADR file exists for this fix.
ADR="$REPO_ROOT/.claude/knowledge-base/decisions/2026-06-24-hydra-v2-3-0-sql-injection-allowlist.md"
[ -f "$ADR" ]; assert "A8: ADR for HIGH CVE allowlist (GHSA-r9w3-57w2-gch2) exists" $?

# A9: allowlist entry present in canonical file with correct sunset.
ALLOWLIST="$REPO_ROOT/.claude/rules/deps-audit-allowlist.txt"
grep -E '^GHSA-r9w3-57w2-gch2\|github\.com/ory/hydra\|v2\.3\.0\|2026-09-22\|' "$ALLOWLIST" >/dev/null
assert "A9: allowlist entry present with sunset 2026-09-22 for GHSA-r9w3-57w2-gch2" $?

echo ""
echo "=== Summary: PASS=$PASS FAIL=$FAIL ==="

if [ "$FAIL" -ne 0 ]; then
  echo "✗ HERMETIC TEST FAILED — investigate above"
  exit 1
fi
echo "✓ All hermetic assertions PASS"
exit 0
