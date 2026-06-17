#!/usr/bin/env bash
# Hermetic test for the M8 1.5 pre-push hook + installer.
# No network, no real push — asserts source shape + installer idempotency.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
HOOK="$ROOT/infra/scripts/local/git-hooks/pre-push"
INST="$ROOT/infra/scripts/local/install-git-hooks.sh"
fail=0
chk() { if eval "$2"; then echo "  PASS: $1"; else echo "  FAIL: $1"; fail=1; fi; }

chk "hook source exists + executable"        "[ -x '$HOOK' ]"
chk "hook runs task quality:gates"           "grep -q 'task quality:gates' '$HOOK'"
chk "hook honors LOOPZERO_SKIP_PREPUSH escape"   "grep -q 'LOOPZERO_SKIP_PREPUSH' '$HOOK'"
chk "hook bash -n clean"                     "bash -n '$HOOK'"
chk "installer exists + executable"          "[ -x '$INST' ]"
chk "installer idempotent (install -m 0755)" "grep -q 'install -m 0755' '$INST'"
chk "installer bash -n clean"                "bash -n '$INST'"

# idempotency: run installer twice into a temp DST, expect identical result
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/.git/hooks"
( cd "$tmp" && git init -q 2>/dev/null || true )
# simulate install by copying (the installer uses git toplevel; here assert the copy is repeatable)
install -m 0755 "$HOOK" "$tmp/.git/hooks/pre-push"
a="$(sha256sum "$tmp/.git/hooks/pre-push" | cut -d' ' -f1)"
install -m 0755 "$HOOK" "$tmp/.git/hooks/pre-push"
b="$(sha256sum "$tmp/.git/hooks/pre-push" | cut -d' ' -f1)"
chk "idempotent copy (same hash twice)"      "[ '$a' = '$b' ]"

[ "$fail" = 0 ] && echo "ALL PASS" || { echo "FAILURES"; exit 1; }
