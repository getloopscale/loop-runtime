#!/usr/bin/env bash
# check-seed-argocd-token-canonical-syntax_test.sh
# T3.1 hermetic guard for seed-argocd-token-canonical.sh (M2 plan).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
SCRIPT="infra/scripts/local/seed-argocd-token-canonical.sh"
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

assert "file exists" test -f "$SCRIPT"
assert "bash -n syntax clean" bash -n "$SCRIPT"
assert "strict mode set -euo pipefail" grep -q '^set -euo pipefail$' "$SCRIPT"
assert "--verify wired" grep -q -- '--verify' "$SCRIPT"
assert "--dry-run wired" grep -q -- '--dry-run' "$SCRIPT"
assert "--help wired" grep -q -- '--help' "$SCRIPT"
assert "argocd-initial-admin-secret read" grep -q 'argocd-initial-admin-secret' "$SCRIPT"
assert "loopzero-api-tokens sync target" grep -q 'loopzero-api-tokens' "$SCRIPT"
# EC-6 absorption: PF_PID captured immediately after background port-forward
assert "EC-6 PF_PID=\$! captured" grep -q 'PF_PID=\$!' "$SCRIPT"
assert "trap on EXIT INT TERM" grep -q 'trap.*EXIT' "$SCRIPT"
assert "size ≤ 120 LoC" test "$(wc -l < "$SCRIPT")" -le 120
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$SCRIPT"
assert "no --no-verify" ! grep -q -- "--no-verify" "$SCRIPT"
assert "allowlist entry present" grep -q 'seed-argocd-token-canonical.sh' "$ALLOWLIST"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
