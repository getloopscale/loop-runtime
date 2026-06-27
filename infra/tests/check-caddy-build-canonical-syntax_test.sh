#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic structural test for caddy-build-canonical.sh (T1.1 of plan
# kratos-self-service-rate-limit-systemic v1.1, EC-1 + EC-6 absorbed).
# NO network, NO xcaddy invocation — verifies the canonical builder
# bundles BOTH `caddy-dns/cloudflare` AND `mholt/caddy-ratelimit@<pinned-SHA>`,
# pins Caddy v2.11.3 + xcaddy v0.4.6, gates idempotency on BOTH modules,
# has cleanup trap, and that the deprecation shim
# `caddy-build-with-cloudflare.sh` is ≤ 5 lines + execs the canonical.

set -euo pipefail

CANONICAL="infra/scripts/local/caddy-build-canonical.sh"
SHIM="infra/scripts/local/caddy-build-with-cloudflare.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
PIN_SHA="5625512f24f6f59d6f64fb3aafe5eecff0b286db"
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

echo "→ Hermetic checks against $CANONICAL"

assert "canonical script exists"          "[[ -f '$CANONICAL' ]]"
assert "canonical script is executable"   "[[ -x '$CANONICAL' ]]"
assert "bash -n parses canonical cleanly" "bash -n '$CANONICAL'"
assert "pins caddy version v2.11.3"       "grep -qE 'CADDY_VERSION=.*v2\.11\.3' '$CANONICAL'"
assert "pins xcaddy version v0.4.6"       "grep -qE 'XCADDY_VERSION=.*v0\.4\.6' '$CANONICAL'"
assert "bundles caddy-dns/cloudflare"     "grep -q 'github.com/caddy-dns/cloudflare' '$CANONICAL'"
assert "bundles mholt/caddy-ratelimit"    "grep -q 'github.com/mholt/caddy-ratelimit' '$CANONICAL'"
assert "pins caddy-ratelimit to SHA $PIN_SHA" \
                                          "grep -q 'mholt/caddy-ratelimit@$PIN_SHA' '$CANONICAL'"
assert "idempotency checks BOTH modules — cloudflare" \
                                          "grep -q 'dns\\.providers\\.cloudflare' '$CANONICAL'"
assert "idempotency checks BOTH modules — rate_limit" \
                                          "grep -q 'http\\.handlers\\.rate_limit' '$CANONICAL'"
assert "idempotency checks output path directly (not basename)" \
                                          "grep -q '\\[\\[ -x \"\\\$CADDY_OUTPUT\" \\]\\]' '$CANONICAL'"
assert "cleanup trap on EXIT/INT/TERM"    "grep -q 'trap.*rm.*EXIT INT TERM' '$CANONICAL'"
assert "honors CADDY_FORCE_REBUILD"       "grep -q 'CADDY_FORCE_REBUILD' '$CANONICAL'"
assert "honors CADDY_OUTPUT"              "grep -q 'CADDY_OUTPUT' '$CANONICAL'"
assert "documents CF_API_TOKEN runtime requirement" \
                                          "grep -q 'CF_API_TOKEN' '$CANONICAL'"
assert "documents EC-1 + EC-6 absorption + plan slug" \
                                          "grep -qE 'kratos-self-service-rate-limit-systemic|EC-1|EC-6' '$CANONICAL'"
assert "canonical script listed in allowlist" \
                                          "grep -q 'caddy-build-canonical.sh' '$ALLOWLIST'"
assert "hermetic test listed in allowlist" \
                                          "grep -q 'check-caddy-build-canonical-syntax_test.sh' '$ALLOWLIST'"

echo
echo "→ Hermetic checks against deprecation shim $SHIM"

assert "shim still exists"                "[[ -f '$SHIM' ]]"
assert "shim bash -n parses cleanly"      "bash -n '$SHIM'"
assert "shim is ≤ 30 lines (compact)"     "[ \$(wc -l < '$SHIM') -le 30 ]"
assert "shim execs the canonical builder" "grep -qE 'exec\\s+.*caddy-build-canonical\\.sh' '$SHIM'"
assert "shim notes deprecation in header" "grep -qiE 'deprecat|shim|exec.*canonical' '$SHIM'"

echo
echo "Result: $PASS passed, $FAIL failed"
exit $FAIL
