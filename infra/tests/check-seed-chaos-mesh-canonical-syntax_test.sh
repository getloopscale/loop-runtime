#!/usr/bin/env bash
# check-seed-chaos-mesh-canonical-syntax_test.sh
#
# T3.1 hermetic guard of plan m2-promote-11-canonical-seeds (v1.1 SHIPPABLE 95.2)
# for infra/scripts/local/seed-chaos-mesh-canonical.sh.
#
# Hermetic: NO network, NO kubectl, NO helm. Pure grep + bash -n.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
SCRIPT="infra/scripts/local/seed-chaos-mesh-canonical.sh"
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
assert "--verify flag wired" grep -q -- '--verify' "$SCRIPT"
assert "--dry-run flag wired" grep -q -- '--dry-run' "$SCRIPT"
assert "--help flag wired" grep -q -- '--help' "$SCRIPT"
if grep -qF -- "helm upgrade --install chaos-mesh" "$SCRIPT"; then printf "[PASS] helm upgrade --install chaos-mesh\n"; PASS=$((PASS+1)); else printf "[FAIL] helm upgrade --install chaos-mesh\n" >&2; FAIL=$((FAIL+1)); fi
assert "k3d-loopzero-mgmt canonical cluster" grep -q 'k3d-loopzero-mgmt' "$SCRIPT"
assert "values-k3d.yaml socketPath override" grep -q 'values-k3d.yaml' "$SCRIPT"
# EC-5 absorption: helm idempotency pre-check
assert "EC-5 idempotency pre-check (helm list chaos-mesh)" grep -q 'helm list.*chaos-mesh' "$SCRIPT"
assert "size ≤ 100 LoC" test "$(wc -l < "$SCRIPT")" -le 100
assert "no Co-Authored-By trailer" ! grep -q "Co-Authored-By" "$SCRIPT"
assert "no --no-verify" ! grep -q -- "--no-verify" "$SCRIPT"
assert "allowlist entry present" grep -q 'seed-chaos-mesh-canonical.sh' "$ALLOWLIST"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
