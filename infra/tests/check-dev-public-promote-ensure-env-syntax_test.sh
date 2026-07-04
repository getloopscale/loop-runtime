#!/usr/bin/env bash
# Hermetic guard for dev-public-promote-ensure-env_test.sh
# (Red Team loopzero-cloud#158 closure — promote path EnsureEnvironment retrofit).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
LIVE="infra/tests/e2e/dev-public-promote-ensure-env_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
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
assert "#158 cited in header" grep -q 'loopzero-cloud#158' "$LIVE"
assert "OPERATOR HARD STOP documented" grep -q 'OPERATOR HARD STOP' "$LIVE"
assert "cleanroom-up-live-test-mandate cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE"
assert "feedback_issue_fix_requires_failing_e2e_test cited" \
  grep -q 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE"
assert "m37/#37 family cited (analog fix)" grep -q 'm37' "$LIVE"

assert "4 flags: --skip-live" grep -q -- '--skip-live' "$LIVE"
assert "4 flags: --scenario=" grep -q -- '--scenario=' "$LIVE"
assert "4 flags: --json" grep -q -- '--json' "$LIVE"
assert "4 flags: --help" grep -q -- '--help' "$LIVE"

# Scenario coverage
assert "Scenario A probes loopzero-api boot log wiring line" \
  grep -Fq 'env ensurer wired to promote handler' "$LIVE"
assert "Scenario B structural probe on WithEnvironmentEnsurer" \
  grep -Fq 'WithEnvironmentEnsurer' "$LIVE"
assert "Scenario C POST /promote to non-existent project" \
  grep -Fq 'ghost-project-158' "$LIVE"

# Anti-silent-success invariant (the exact #158 trap)
assert "Scenario C guards against silent success (202)" \
  grep -Fq 'silent success' "$LIVE"

# Governance
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$LIVE"
assert "no --no-verify" ! grep -q "no-verify" "$LIVE"
assert "allowlist entry LIVE" grep -q 'dev-public-promote-ensure-env_test.sh' "$ALLOWLIST"
assert "allowlist entry hermetic (self)" grep -q 'check-dev-public-promote-ensure-env-syntax_test.sh' "$ALLOWLIST"

# Fix-source presence — the Go-side change that the LIVE proves
assert "PromoteHandler.WithEnvironmentEnsurer shipped in routes/promote.go" \
  grep -Fq 'WithEnvironmentEnsurer' api/internal/routes/promote.go
assert "main.go wires PromoteHandler with ensurer" \
  grep -Fq 'PromoteHandler.WithEnvironmentEnsurer' api/cmd/main.go

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
