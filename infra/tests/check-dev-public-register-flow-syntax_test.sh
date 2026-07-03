#!/usr/bin/env bash
# Hermetic structural test for dev-public-register-flow_test.sh (M0 J-02).
# Validates structure WITHOUT executing: bash -n syntax, scenario functions,
# pre-flight, flag wiring, read-only invariant, allowlist entry. NO network.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-register-flow_test.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0
assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then printf '  PASS: %s\n' "$desc"; PASS=$((PASS + 1))
  else printf '  FAIL: %s\n' "$desc"; FAIL=$((FAIL + 1)); fi
}
contains() { grep -qF -- "$2" "$1" && echo 1 || echo 0; }
matches() { grep -qE -- "$2" "$1" && echo 1 || echo 0; }

printf '[register-flow-syntax-test] gates:\n'

[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists" "$exists"
[[ "$exists" == "1" ]] || { printf 'ABORT: %s missing\n' "$TARGET"; exit 1; }

# bash -n syntax
if bash -n "$TARGET" 2>/dev/null; then assert "bash -n syntax clean" 1; else assert "bash -n syntax clean" 0; fi
assert "strict mode set -euo pipefail" "$(contains "$TARGET" 'set -euo pipefail')"

# 3 scenario functions
assert "scenario A (me provisioning contract)" "$(contains "$TARGET" 'scenario_a_me_provisioning_contract')"
assert "scenario B (completing screen shipped)" "$(contains "$TARGET" 'scenario_b_completing_screen_shipped')"
assert "scenario C (preflight)" "$(contains "$TARGET" 'scenario_c_preflight')"
assert "pre-flight function" "$(contains "$TARGET" 'preflight()')"

# flags
assert "--json flag wired" "$(contains "$TARGET" '--json')"
assert "--skip-live flag wired" "$(contains "$TARGET" '--skip-live')"
assert "--scenario flag wired" "$(contains "$TARGET" '--scenario')"
assert "--skip-live short-circuits before network" "$(matches "$TARGET" 'SKIP_LIVE.*==.*1.*\n.*exit 0|--skip-live: structural')"

# contract assertions present
assert "asserts /v1/auth/me endpoint" "$(contains "$TARGET" '/v1/auth/me')"
assert "asserts provisioning in {active,pending,unknown}" "$(contains "$TARGET" 'active|pending|unknown')"
assert "asserts completing-screen literal in bundle" "$(contains "$TARGET" 'Setting up your workspace')"

# read-only invariant (no identity/tenant creation) + operator gate
assert "read-only: no kratos identity creation" "$([[ $(grep -cE 'identities|POST.*registration' "$TARGET") -eq 0 ]] && echo 1 || echo 0)"
assert "cites cleanroom-up-live-test-mandate" "$(contains "$TARGET" 'cleanroom-up-live-test-mandate')"
assert "cites the runbook" "$(contains "$TARGET" 'm0-live-validation-runbook')"

# JWT seed convention
assert "uses ~/.loopzero JWT seed convention" "$(contains "$TARGET" '.loopzero/local-dogfood-token.txt')"

# no Co-Authored-By / no --no-verify
assert "no Co-Authored-By trailer" "$([[ $(grep -c 'Co-Authored-By' "$TARGET") -eq 0 ]] && echo 1 || echo 0)"

# allowlist entries for both scripts
assert "allowlist has LIVE test entry" "$(contains "$ALLOWLIST" 'dev-public-register-flow_test.sh')"
assert "allowlist has syntax test entry" "$(contains "$ALLOWLIST" 'check-dev-public-register-flow-syntax_test.sh')"

printf '[register-flow-syntax-test] %d passed, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
