#!/usr/bin/env bash
# Hermetic structural test for trio-dev-public.sh
# Validates the 4 phases + flag wiring + composition without invoking.
# Runs in <1s. No network, no kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO_ROOT/infra/scripts/local/trio-dev-public.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0

assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n' "$desc"
    FAIL=$((FAIL + 1))
  fi
}

[[ -f "$SCRIPT" ]] || { printf 'FAIL: %s missing\n' "$SCRIPT"; exit 1; }
CONTENT=$(<"$SCRIPT")

# Bash syntax
printf '== bash syntax ==\n'
if bash -n "$SCRIPT" 2>/dev/null; then
  assert "bash -n parses" 1
else
  assert "bash -n parses" 0
fi

# 4 phases present
printf '\n== 4 trio phases composed ==\n'

for p in cleanroom-soft chaos-soft dogfood-soft live-proof; do
  if printf '%s' "$CONTENT" | grep -q "phase \"$p"; then
    assert "phase $p invoked" 1
  else
    assert "phase $p invoked" 0
  fi
done

# Soft cleanroom check coverage: must assert 3 drift signals
printf '\n== soft_cleanroom drift assertions ==\n'

soft_cr_body=$(printf '%s' "$CONTENT" | awk '/^soft_cleanroom\(\)/,/^}/')
if printf '%s' "$soft_cr_body" | grep -q 'REPLACE_VIA_PULUMI'; then
  assert "cleanroom-soft checks REPLACE_VIA_PULUMI placeholders" 1
else
  assert "cleanroom-soft checks REPLACE_VIA_PULUMI placeholders" 0
fi

if printf '%s' "$soft_cr_body" | grep -q 'localhost:4455'; then
  assert "cleanroom-soft checks localhost:4455 drift" 1
else
  assert "cleanroom-soft checks localhost:4455 drift" 0
fi

if printf '%s' "$soft_cr_body" | grep -qE '\$\\\{RESEND_API_KEY|\\\$\{RESEND_API_KEY'; then
  assert "cleanroom-soft checks literal \${RESEND_API_KEY} drift" 1
else
  # Try alternative regex (escaping varies)
  if printf '%s' "$soft_cr_body" | grep -q 'RESEND_API_KEY'; then
    assert "cleanroom-soft checks literal \${RESEND_API_KEY} drift" 1
  else
    assert "cleanroom-soft checks literal \${RESEND_API_KEY} drift" 0
  fi
fi

# Soft chaos must probe all 6 hosts
printf '\n== soft_chaos covers all 6 hosts ==\n'

soft_chaos_body=$(printf '%s' "$CONTENT" | awk '/^soft_chaos\(\)/,/^}/')
hosts=(
  "auth.loopzero.example" "app.loopzero.example" "cloud.loopzero.example"
  "auth-dev.loopzero.example" "app-dev.loopzero.example" "cloud-dev.loopzero.example"
)
all_covered=1
for h in "${hosts[@]}"; do
  if printf '%s' "$soft_chaos_body" | grep -q "$h"; then
    :
  else
    all_covered=0
    printf '  missing host: %s\n' "$h"
  fi
done
assert "all 6 hosts listed in soft_chaos" "$all_covered"

if printf '%s' "$soft_chaos_body" | grep -qi "let.s encrypt"; then
  assert "soft_chaos asserts LE issuer (not just any cert)" 1
else
  assert "soft_chaos asserts LE issuer (not just any cert)" 0
fi

# Dogfood-soft delegates to existing billing portal test
printf '\n== dogfood-soft delegates to e2e test ==\n'

if printf '%s' "$CONTENT" | grep -q 'dev-public-billing-portal_test.sh'; then
  assert "dogfood-soft calls dev-public-billing-portal_test.sh" 1
else
  assert "dogfood-soft calls dev-public-billing-portal_test.sh" 0
fi

# Live-proof delegates to existing script
if printf '%s' "$CONTENT" | grep -q 'live-proof-dev-public.sh'; then
  assert "live-proof phase calls live-proof-dev-public.sh" 1
else
  assert "live-proof phase calls live-proof-dev-public.sh" 0
fi

# Flags
printf '\n== flags wired ==\n'

if printf '%s' "$CONTENT" | grep -q '\-\-env='; then
  assert "--env= flag present" 1
else
  assert "--env= flag present" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-bail-on-fail'; then
  assert "--bail-on-fail flag present" 1
else
  assert "--bail-on-fail flag present" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-json'; then
  assert "--json flag present" 1
else
  assert "--json flag present" 0
fi

# Exit semantics
printf '\n== exit semantics ==\n'

if printf '%s' "$CONTENT" | grep -q '"\$TRIO_FAIL" -eq 0'; then
  assert "non-zero exit on any phase FAIL" 1
else
  assert "non-zero exit on any phase FAIL" 0
fi

# Allowlist
printf '\n== allowlist entry ==\n'

if grep -q 'trio-dev-public.sh' "$ALLOWLIST"; then
  assert "trio-dev-public.sh entry in allowlist" 1
else
  assert "trio-dev-public.sh entry in allowlist" 0
fi

printf '\n== summary ==\n'
printf '  PASS: %d\n' "$PASS"
printf '  FAIL: %d\n' "$FAIL"

[[ "$FAIL" -eq 0 ]] || exit 1
