#!/usr/bin/env bash
# Hermetic structural guard for dev-public-auth-email-delivery_test.sh
# (Red Team loopzero-cloud#117 closure regression-protector).
#
# Pure-text + bash -n; NO network, NO kubectl, NO curl invocation.
set -euo pipefail

LIVE_SCRIPT="infra/tests/e2e/dev-public-auth-email-delivery_test.sh"
PASS=0
FAIL=0

check() {
  local id="$1" cond="$2"
  if eval "$cond" >/dev/null 2>&1; then
    PASS=$((PASS+1))
    printf "  [%s] PASS\n" "$id"
  else
    FAIL=$((FAIL+1))
    printf "  [%s] FAIL — %s\n" "$id" "$cond"
  fi
}

echo "=== hermetic syntax check: ${LIVE_SCRIPT} ==="

check "file-exists" "test -f ${LIVE_SCRIPT}"
check "bash-n-clean" "bash -n ${LIVE_SCRIPT}"
check "strict-mode" "grep -q '^set -euo pipefail' ${LIVE_SCRIPT}"
check "issue-117-cited" "grep -q 'loopzero-cloud#117' ${LIVE_SCRIPT}"
check "operator-hard-stop-cited" "grep -q 'OPERATOR HARD STOP' ${LIVE_SCRIPT}"
check "cleanroom-mandate-cited" "grep -q 'cleanroom-up-live-test-mandate' ${LIVE_SCRIPT}"
check "scenario-a-defined" "grep -q '^scenario_a()' ${LIVE_SCRIPT}"
check "scenario-b-defined" "grep -q '^scenario_b()' ${LIVE_SCRIPT}"
check "scenario-c-defined" "grep -q '^scenario_c()' ${LIVE_SCRIPT}"
check "preflight-defined" "grep -q '^preflight()' ${LIVE_SCRIPT}"
check "anti-enum-asserted" "grep -q 'anti-enumeration\\|correctly muted' ${LIVE_SCRIPT}"
check "resend-api-probed" "grep -q 'api.resend.com/emails' ${LIVE_SCRIPT}"
check "courier-messages-probed" "grep -q 'courier_messages' ${LIVE_SCRIPT}"
check "flag-skip-live" "grep -q '\\-\\-skip-live' ${LIVE_SCRIPT}"
check "flag-json" "grep -q '\\-\\-json' ${LIVE_SCRIPT}"
check "flag-scenario" "grep -q '\\-\\-scenario=' ${LIVE_SCRIPT}"
check "flag-help" "grep -q '\\-\\-help' ${LIVE_SCRIPT}"
check "exit-code-1-fail" "grep -q 'exit 1' ${LIVE_SCRIPT}"
check "exit-code-2-blocked" "grep -q 'exit 2' ${LIVE_SCRIPT}"
check "no-co-authored-by" "! grep -q 'Co-Authored-By' ${LIVE_SCRIPT}"
check "no-skip-verify-flag" "! grep -q -- '--no-verify' ${LIVE_SCRIPT}"
check "allowlist-entry" "grep -q 'dev-public-auth-email-delivery_test.sh' .claude/rules/shell-scripts-allowlist.md"
check "allowlist-syntax-entry" "grep -q 'check-dev-public-auth-email-delivery-syntax_test.sh' .claude/rules/shell-scripts-allowlist.md"

echo ""
echo "=== ${PASS} pass / ${FAIL} fail ==="
[[ $FAIL -gt 0 ]] && exit 1
exit 0
