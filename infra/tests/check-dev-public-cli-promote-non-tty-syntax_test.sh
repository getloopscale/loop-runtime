#!/usr/bin/env bash
# Hermetic guard for dev-public-cli-promote-non-tty_test.sh
# (Red Team loopzero-cloud#161 closure — CLI promote non-TTY fail-fast).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
LIVE="infra/tests/e2e/dev-public-cli-promote-non-tty_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
CLI_PROMOTE_TS="$HOME/loopzero-cloud/loopzero-cli/src/commands/promote.ts"
CLI_DEPLOY_TS="$HOME/loopzero-cloud/loopzero-cli/src/register/deploy.ts"
PASS=0; FAIL=0

assert() {
  local desc="$1"; shift
  if eval "$@" >/dev/null 2>&1; then
    printf '[PASS] %s\n' "$desc"; PASS=$((PASS+1))
  else
    printf '[FAIL] %s\n' "$desc" >&2; FAIL=$((FAIL+1))
  fi
}

assert "file exists" test -f "$LIVE"
assert "bash -n syntax clean" bash -n "$LIVE"
assert "strict mode set -euo pipefail" grep -q '^set -euo pipefail$' "$LIVE"
assert "#161 cited in header" grep -q 'loopzero-cloud#161' "$LIVE"
assert "feedback_issue_fix_requires_failing_e2e_test cited" \
  grep -q 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE"

assert "4 flags: --skip-live" grep -q -- '--skip-live' "$LIVE"
assert "4 flags: --scenario=" grep -q -- '--scenario=' "$LIVE"
assert "4 flags: --json" grep -q -- '--json' "$LIVE"
assert "4 flags: --help" grep -q -- '--help' "$LIVE"

assert "Scenario A probes isNonInteractive declaration" \
  grep -Fq 'isNonInteractive' "$LIVE"
assert "Scenario B probes stdin.isTTY wire" \
  grep -Fq 'process.stdin.isTTY' "$LIVE"
assert "Scenario C probes version bump" \
  grep -Fq '0.3.7' "$LIVE"

# Governance
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$LIVE"
assert "no --no-verify" ! grep -q "no-verify" "$LIVE"
assert "allowlist entry LIVE" grep -q 'dev-public-cli-promote-non-tty_test.sh' "$ALLOWLIST"
assert "allowlist entry hermetic (self)" grep -q 'check-dev-public-cli-promote-non-tty-syntax_test.sh' "$ALLOWLIST"

# Fix-source presence
if [ -f "$CLI_PROMOTE_TS" ] ; then
  assert "loopzero-cli promote.ts declares isNonInteractive" \
    grep -Fq 'isNonInteractive' "$CLI_PROMOTE_TS"
  assert "loopzero-cli promote.ts honors it in gate" \
    grep -Fq 'this.deps.isNonInteractive' "$CLI_PROMOTE_TS"
  assert "loopzero-cli promote.ts error mentions non-interactive" \
    grep -Fq 'non-interactive' "$CLI_PROMOTE_TS"
fi
if [ -f "$CLI_DEPLOY_TS" ] ; then
  assert "loopzero-cli deploy.ts wires isNonInteractive from stdin.isTTY" \
    grep -Fq 'isNonInteractive: !process.stdin.isTTY' "$CLI_DEPLOY_TS"
fi

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
