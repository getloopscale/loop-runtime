#!/usr/bin/env bash
# Hermetic guard for dev-public-promote-canonical-url_test.sh
# (Red Team loopzero-cloud#159 closure — promote URL canonical formula).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
LIVE="infra/tests/e2e/dev-public-promote-canonical-url_test.sh"
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
assert "#159 cited in header" grep -q 'loopzero-cloud#159' "$LIVE"
assert "OPERATOR HARD STOP documented" grep -q 'OPERATOR HARD STOP' "$LIVE"
assert "cleanroom-up-live-test-mandate cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE"
assert "feedback_issue_fix_requires_failing_e2e_test cited" \
  grep -q 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE"
assert "J-25 family cited (analog fix)" grep -q 'J-25' "$LIVE"

# 4 flags + 3 scenarios
assert "4 flags: --skip-live" grep -q -- '--skip-live' "$LIVE"
assert "4 flags: --scenario=" grep -q -- '--scenario=' "$LIVE"
assert "4 flags: --json" grep -q -- '--json' "$LIVE"
assert "4 flags: --help" grep -q -- '--help' "$LIVE"

# Scenario probes
assert "Scenario A probes ResolveEntryAppFromApps (SoT)" \
  grep -Fq 'ResolveEntryAppFromApps' "$LIVE"
assert "Scenario B probes naming.AppURL usage" \
  grep -Fq 'naming.AppURL' "$LIVE"
assert "Scenario B guards against legacy Sprintf formula" \
  grep -Fq 'Sprintf' "$LIVE"
assert "Scenario C POST /promote to non-existent project" \
  grep -Fq 'ghost-project-159' "$LIVE"
assert "Scenario C anti-silent-success guard (never 202+URL)" \
  grep -Fq 'silent-success' "$LIVE"

# Anti-patterns
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$LIVE"
assert "no --no-verify" ! grep -q "no-verify" "$LIVE"
assert "allowlist entry LIVE" grep -q 'dev-public-promote-canonical-url_test.sh' "$ALLOWLIST"
assert "allowlist entry hermetic (self)" grep -q 'check-dev-public-promote-canonical-url-syntax_test.sh' "$ALLOWLIST"

# Fix-source presence
assert "pkg/naming/entry_app.go shipped as SoT" \
  test -f pkg/naming/entry_app.go
assert "pkg/naming exports ResolveEntryAppFromApps" \
  grep -Fq 'func ResolveEntryAppFromApps' pkg/naming/entry_app.go
assert "promotion_orchestrator uses naming.AppURL" \
  grep -Fq 'naming.AppURL' api/internal/services/promotion_orchestrator.go
# Legacy formula detection — direct probe (avoids eval+`!` interaction with
# the assert helper by using a plain `if` test).
if ! grep -Fq 'https://%s-%s.%s' /home/paulo/loopzero-cloud/loopzero/api/internal/services/promotion_orchestrator.go 2>/dev/null; then
  printf '[PASS] %s\n' "promotion_orchestrator no longer emits legacy Sprintf format"
  PASS=$((PASS+1))
else
  printf '[FAIL] %s\n' "promotion_orchestrator no longer emits legacy Sprintf format" >&2
  FAIL=$((FAIL+1))
fi
if ! grep -Fq 'req.ProjectName, req.ToEnv, domain' /home/paulo/loopzero-cloud/loopzero/api/internal/services/promotion_orchestrator.go 2>/dev/null; then
  printf '[PASS] %s\n' "promotion_orchestrator no longer emits legacy 3-arg tuple"
  PASS=$((PASS+1))
else
  printf '[FAIL] %s\n' "promotion_orchestrator no longer emits legacy 3-arg tuple" >&2
  FAIL=$((FAIL+1))
fi

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
