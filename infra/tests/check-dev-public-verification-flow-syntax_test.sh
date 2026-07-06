#!/usr/bin/env bash
# Hermetic structural test for dev-public-verification-flow_test.sh (M0 J-03).
# NO network. NO kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-verification-flow_test.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0
assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then printf '  PASS: %s\n' "$desc"; PASS=$((PASS + 1))
  else printf '  FAIL: %s\n' "$desc"; FAIL=$((FAIL + 1)); fi
}
contains() { grep -qF -- "$2" "$1" && echo 1 || echo 0; }

printf '[verification-flow-syntax-test] gates:\n'

[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists" "$exists"
[[ "$exists" == "1" ]] || { printf 'ABORT: %s missing\n' "$TARGET"; exit 1; }

if bash -n "$TARGET" 2>/dev/null; then assert "bash -n syntax clean" 1; else assert "bash -n syntax clean" 0; fi
assert "strict mode set -euo pipefail" "$(contains "$TARGET" 'set -euo pipefail')"

assert "scenario A (verification served)" "$(contains "$TARGET" 'scenario_a_verification_served')"
assert "scenario B (resend cooldown shipped)" "$(contains "$TARGET" 'scenario_b_resend_cooldown_shipped')"
assert "scenario C (preflight)" "$(contains "$TARGET" 'scenario_c_preflight')"
assert "pre-flight function" "$(contains "$TARGET" 'preflight()')"

assert "--json flag wired" "$(contains "$TARGET" '--json')"
assert "--skip-live flag wired" "$(contains "$TARGET" '--skip-live')"
assert "--scenario flag wired" "$(contains "$TARGET" '--scenario')"
assert "--skip-live short-circuits" "$(contains "$TARGET" 'structural mode, no network')"

assert "asserts /verification page" "$(contains "$TARGET" '/verification')"
assert "asserts resend cooldown literal" "$(contains "$TARGET" "Didn't receive the email? Resend")"
assert "asserts persisted-cooldown key" "$(contains "$TARGET" 'loopzero-verify-resend-until')"

assert "read-only: never sends a real email" "$([[ $(grep -cE 'courier|POST.*verification/methods|send.*code' "$TARGET") -eq 0 ]] && echo 1 || echo 0)"
assert "cites cleanroom-up-live-test-mandate" "$(contains "$TARGET" 'cleanroom-up-live-test-mandate')"
assert "cites the runbook" "$(contains "$TARGET" 'm0-live-validation-runbook')"

assert "no Co-Authored-By trailer" "$([[ $(grep -c 'Co-Authored-By' "$TARGET") -eq 0 ]] && echo 1 || echo 0)"

assert "allowlist has LIVE test entry" "$(contains "$ALLOWLIST" 'dev-public-verification-flow_test.sh')"
assert "allowlist has syntax test entry" "$(contains "$ALLOWLIST" 'check-dev-public-verification-flow-syntax_test.sh')"

printf '[verification-flow-syntax-test] %d passed, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
