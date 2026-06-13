#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic structural test for caddy-build-with-cloudflare.sh.
# NO network, NO xcaddy invocation — verifies that the build script is
# parseable, declares the right pinned versions, gates idempotency on
# the binary path (not a PATH lookup), validates module presence
# post-build, has a cleanup trap, and is listed in the allowlist.

set -euo pipefail

SCRIPT="infra/scripts/local/caddy-build-with-cloudflare.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
PASS=0
FAIL=0

assert() {
  local name="$1" cond="$2"
  if eval "$cond" >/dev/null 2>&1; then
    PASS=$((PASS+1))
    echo "  PASS  $name"
  else
    FAIL=$((FAIL+1))
    echo "  FAIL  $name  (assertion: $cond)"
  fi
}

echo "→ Hermetic checks against $SCRIPT"

assert "script exists"               "[[ -f '$SCRIPT' ]]"
assert "script is executable"        "[[ -x '$SCRIPT' ]]"
assert "bash -n parses cleanly"      "bash -n '$SCRIPT'"
assert "pins caddy version"          "grep -qE 'CADDY_VERSION=.*v2\.11\.3' '$SCRIPT'"
assert "pins xcaddy version"         "grep -qE 'XCADDY_VERSION=.*v0\.4\.6' '$SCRIPT'"
assert "uses caddy-dns/cloudflare"   "grep -q 'github.com/caddy-dns/cloudflare' '$SCRIPT'"
assert "idempotency checks output path directly (not basename)" \
                                     "grep -q '\\[\\[ -x \"\\\$CADDY_OUTPUT\" \\]\\]' '$SCRIPT'"
assert "verifies module presence after build" \
                                     "grep -q 'dns\\.providers\\.cloudflare' '$SCRIPT'"
assert "cleanup trap on EXIT/INT/TERM" \
                                     "grep -q 'trap.*rm.*EXIT INT TERM' '$SCRIPT'"
assert "honors CADDY_FORCE_REBUILD"  "grep -q 'CADDY_FORCE_REBUILD' '$SCRIPT'"
assert "honors CADDY_OUTPUT"         "grep -q 'CADDY_OUTPUT' '$SCRIPT'"
assert "documents CF_API_TOKEN runtime requirement" \
                                     "grep -q 'CF_API_TOKEN' '$SCRIPT'"
assert "listed in shell-scripts-allowlist.md" \
                                     "grep -q '$SCRIPT' '$ALLOWLIST'"

echo
echo "Result: $PASS passed, $FAIL failed"
exit $FAIL
