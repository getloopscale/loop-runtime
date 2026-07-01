#!/usr/bin/env bash
# Hermetic guard for dev-public-cli-logs-listbuilds-inference_test.sh
# (Red Team loopzero-cloud#160 closure — CLI logs inference via listBuilds).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
LIVE="infra/tests/e2e/dev-public-cli-logs-listbuilds-inference_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
CLI_LOGS_TS="$HOME/loopzero-cloud/loopzero-cli/src/commands/logs.ts"
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
assert "#160 cited in header" grep -q 'loopzero-cloud#160' "$LIVE"
assert "OPERATOR HARD STOP documented" grep -q 'OPERATOR HARD STOP' "$LIVE"
assert "cleanroom-up-live-test-mandate cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE"
assert "feedback_issue_fix_requires_failing_e2e_test cited" \
  grep -q 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE"

assert "4 flags: --skip-live" grep -q -- '--skip-live' "$LIVE"
assert "4 flags: --scenario=" grep -q -- '--scenario=' "$LIVE"
assert "4 flags: --json" grep -q -- '--json' "$LIVE"
assert "4 flags: --help" grep -q -- '--help' "$LIVE"

# Scenario probes
assert "Scenario A probes builds endpoint" \
  grep -Fq '/builds?env=' "$LIVE"
assert "Scenario A checks canonical shape (builds+total)" \
  grep -Fq 'builds' "$LIVE"
assert "Scenario B probes CLI source" \
  grep -Fq 'commands/logs.ts' "$LIVE"
assert "Scenario B probes listBuilds usage" \
  grep -Fq 'this.deps.listBuilds' "$LIVE"
assert "Scenario C probes version bump" \
  grep -Fq 'package.json' "$LIVE"
assert "Scenario C requires version 0.3.6+" \
  grep -Fq '0.3.6' "$LIVE"

# Governance
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$LIVE"
assert "no --no-verify" ! grep -q "no-verify" "$LIVE"
assert "allowlist entry LIVE" grep -q 'dev-public-cli-logs-listbuilds-inference_test.sh' "$ALLOWLIST"
assert "allowlist entry hermetic (self)" grep -q 'check-dev-public-cli-logs-listbuilds-inference-syntax_test.sh' "$ALLOWLIST"

# Fix-source presence (loopzero-cli side — allow SKIP if loopzero-cli not cloned)
if [ -f "$CLI_LOGS_TS" ] ; then
  assert "loopzero-cli logs.ts declares listBuilds in LogsDeps" \
    grep -Fq 'listBuilds?:' "$CLI_LOGS_TS"
  assert "loopzero-cli logs.ts prefers listBuilds via this.deps.listBuilds" \
    grep -Fq 'this.deps.listBuilds' "$CLI_LOGS_TS"
fi

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
