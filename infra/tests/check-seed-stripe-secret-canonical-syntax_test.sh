#!/usr/bin/env bash
# check-seed-stripe-secret-canonical-syntax_test.sh
# T3.1 hermetic guard for seed-stripe-secret-canonical.sh (M2 plan).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
SCRIPT="infra/scripts/local/seed-stripe-secret-canonical.sh"
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
assert "loopzero-stripe-webhook Secret target" grep -q 'loopzero-stripe-webhook' "$SCRIPT"
# Mode 0600 enforcement (canonical secret-SoT contract)
assert "mode 0600 enforcement" grep -q '"600"' "$SCRIPT"
# EC-8 absorption: graceful SKIP on placeholder
if grep -qE "whsec_PLACEHOLDER|sponsor-blocked" "$SCRIPT"; then printf "[PASS] EC-8 SKIP graceful on placeholder\n"; PASS=$((PASS+1)); else printf "[FAIL] EC-8 SKIP graceful on placeholder\n" >&2; FAIL=$((FAIL+1)); fi
if grep -qE "^[[:space:]]*exit 0$" "$SCRIPT"; then printf "[PASS] EC-8 graceful exit 0 path\n"; PASS=$((PASS+1)); else printf "[FAIL] EC-8 graceful exit 0 path\n" >&2; FAIL=$((FAIL+1)); fi
assert "size ≤ 100 LoC" test "$(wc -l < "$SCRIPT")" -le 100
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$SCRIPT"
assert "allowlist entry present" grep -q 'seed-stripe-secret-canonical.sh' "$ALLOWLIST"
# stripe-l2-wiring-canonical-fix T1.2: Secret data key must match consumer env var name
assert "Secret data key is STRIPE_WEBHOOK_SECRET (canonical env-var-name match)" \
  grep -qE 'from-literal=STRIPE_WEBHOOK_SECRET=' "$SCRIPT"
assert "no legacy 'whsec' data key drift" \
  ! grep -qE 'from-literal=whsec=' "$SCRIPT"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
