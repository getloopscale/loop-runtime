#!/usr/bin/env bash
# check-seed-vault-cross-cluster-addr-syntax_test.sh
# T3.1 hermetic guard for seed-vault-cross-cluster-addr.sh (M2 plan).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
SCRIPT="infra/scripts/local/seed-vault-cross-cluster-addr-canonical.sh"
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

# Helper: literal string grep (handles dashes in pattern via -F)
fgrep_str() { grep -qF "$1" "$2"; }
egrep() { grep -qE "$1" "$2"; }

assert "file exists" test -f "$SCRIPT"
assert "bash -n syntax clean" bash -n "$SCRIPT"
assert "strict mode set -euo pipefail" grep -q '^set -euo pipefail$' "$SCRIPT"
assert "--verify wired" grep -q -- '--verify' "$SCRIPT"
assert "--dry-run wired" grep -q -- '--dry-run' "$SCRIPT"
assert "--help wired" grep -q -- '--help' "$SCRIPT"
assert "k3d-loopzero-vault-serverlb cross-cluster discovery" grep -q 'k3d-loopzero-vault-serverlb' "$SCRIPT"
assert "OPENBAO_VAULT_ADDR env var target" grep -q 'OPENBAO_VAULT_ADDR' "$SCRIPT"
assert "docker inspect IP extraction" grep -q 'docker inspect' "$SCRIPT"
# EC-1 absorption: distinguish container-not-running from IP-not-yet-assigned
assert "EC-1 .State.Running pre-check" grep -q '.State.Running' "$SCRIPT"
# EC-2 absorption: wait for in-progress rollout before set env
if grep -qF -- "rollout status deploy/loopzero-api --timeout=120s" "$SCRIPT"; then printf "[PASS] EC-2 rollout status pre-set env\n"; PASS=$((PASS+1)); else printf "[FAIL] EC-2 rollout status pre-set env\n" >&2; FAIL=$((FAIL+1)); fi
assert "size ≤ 100 LoC" test "$(wc -l < "$SCRIPT")" -le 100
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$SCRIPT"
assert "allowlist entry present" grep -q 'seed-vault-cross-cluster-addr-canonical.sh' "$ALLOWLIST"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
