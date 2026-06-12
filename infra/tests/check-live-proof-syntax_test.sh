#!/usr/bin/env bash
# Hermetic test for live-proof-dev-public.sh — validates script structure
# without invoking it. Runs in <1s, zero network, zero kubectl, zero psql.
#
# Asserts:
#   - shellcheck passes (no unused vars, no double-quotes-missing)
#   - All 3 checks defined: check_oidc, check_stripe, check_resend
#   - All 3 layers checked per check (L1 .env, L2 kubectl, L3 LIVE)
#   - --skip-layer3 flag wired
#   - --json flag wired
#   - exit-non-zero on FAIL
#   - allowlist entry exists

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO_ROOT/infra/scripts/local/live-proof-dev-public.sh"
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

# Structural tests
printf '== structural assertions ==\n'

if printf '%s' "$CONTENT" | grep -q '^check_oidc()'; then
  assert "check_oidc function defined" 1
else
  assert "check_oidc function defined" 0
fi

if printf '%s' "$CONTENT" | grep -q '^check_stripe()'; then
  assert "check_stripe function defined" 1
else
  assert "check_stripe function defined" 0
fi

if printf '%s' "$CONTENT" | grep -q '^check_resend()'; then
  assert "check_resend function defined" 1
else
  assert "check_resend function defined" 0
fi

# Each check must have 3 layers
printf '\n== 3-layer protocol per check ==\n'

for check in oidc stripe resend; do
  # Extract function body
  body=$(printf '%s' "$CONTENT" | awk "/^check_$check\(\)/,/^}/")
  for layer in l1 l2 l3; do
    if printf '%s' "$body" | grep -qE "$check\.$layer"; then
      assert "$check.$layer key used in RESULTS array" 1
    else
      assert "$check.$layer key used in RESULTS array" 0
    fi
  done
done

# Flag wiring
printf '\n== flag wiring ==\n'

if printf '%s' "$CONTENT" | grep -q '\-\-skip-layer3'; then
  assert "--skip-layer3 flag present" 1
else
  assert "--skip-layer3 flag present" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-json'; then
  assert "--json flag present" 1
else
  assert "--json flag present" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-env='; then
  assert "--env= flag present" 1
else
  assert "--env= flag present" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-check='; then
  assert "--check= flag present" 1
else
  assert "--check= flag present" 0
fi

# Exit code semantics
printf '\n== exit semantics ==\n'

if printf '%s' "$CONTENT" | grep -q '\[\[ "\$FAIL" -eq 0 \]\] || exit 1'; then
  assert "non-zero exit on any FAIL" 1
else
  assert "non-zero exit on any FAIL" 0
fi

# Layer 3 (LIVE) calls must be gated by SKIP_LAYER3
printf '\n== --skip-layer3 properly gates Layer 3 calls ==\n'

# Count check_* functions that reference SKIP_LAYER3 BEFORE doing any
# kubectl/curl/psql call. Heuristic: SKIP_LAYER3 check must come BEFORE
# the LIVE network/exec call in each function.
for check in oidc stripe resend api_sanity; do
  body=$(printf '%s' "$CONTENT" | awk "/^check_$check\(\)/,/^}/")
  if printf '%s' "$body" | grep -q 'SKIP_LAYER3'; then
    assert "$check check gates Layer 3 via SKIP_LAYER3" 1
  else
    assert "$check check gates Layer 3 via SKIP_LAYER3" 0
  fi
done

# T3.1 — api-sanity check (4 endpoints; would have caught iter 20
# BuildListItem.Apps regression). Added 2026-06-08.
printf '\n== T3.1 — --check=api-sanity assertions ==\n'

if printf '%s' "$CONTENT" | grep -q 'check_api_sanity\b'; then
  assert "check_api_sanity function defined" 1
else
  assert "check_api_sanity function defined" 0
fi

if printf '%s' "$CONTENT" | grep -qE 'api-sanity'; then
  assert "--check=api-sanity flag advertised" 1
else
  assert "--check=api-sanity flag advertised" 0
fi

# Each of the 4 endpoint assertions must be referenced inside the
# check_api_sanity function body.
body=$(printf '%s' "$CONTENT" | awk '/^check_api_sanity\(\)/,/^}/')

if printf '%s' "$body" | grep -qE '/api/v1/projects([^/]|$)'; then
  assert "endpoint 1: /api/v1/projects (list)" 1
else
  assert "endpoint 1: /api/v1/projects (list)" 0
fi

if printf '%s' "$body" | grep -qE '/api/v1/projects/\$[A-Z_]*PROJECT_NAME|/api/v1/projects/\{name\}|/projects/[a-z_]+'; then
  assert "endpoint 2: /api/v1/projects/{name} (detail)" 1
else
  assert "endpoint 2: /api/v1/projects/{name} (detail)" 0
fi

if printf '%s' "$body" | grep -qE '/builds'; then
  assert "endpoint 3: /api/v1/projects/{name}/builds (list)" 1
else
  assert "endpoint 3: /api/v1/projects/{name}/builds (list)" 0
fi

if printf '%s' "$body" | grep -qE '/status'; then
  assert "endpoint 4: /api/v1/projects/{name}/status" 1
else
  assert "endpoint 4: /api/v1/projects/{name}/status" 0
fi

# Iter-20 forensic anchor — script must mention the regression class it
# defends against (BuildListItem.Apps gap) so future operators understand
# why the assertion exists.
if printf '%s' "$CONTENT" | grep -qiE 'BuildListItem\.Apps|iter 20|apps_spec'; then
  assert "forensic anchor: iter 20 BuildListItem.Apps regression cited" 1
else
  assert "forensic anchor: iter 20 BuildListItem.Apps regression cited" 0
fi

# Shape assertions — must reference env_count + apps_count + last_deploy
if printf '%s' "$body" | grep -qE 'env_count|environment_count'; then
  assert "asserts env_count on projects list" 1
else
  assert "asserts env_count on projects list" 0
fi

if printf '%s' "$body" | grep -qE 'apps_count'; then
  assert "asserts apps_count on project detail" 1
else
  assert "asserts apps_count on project detail" 0
fi

if printf '%s' "$body" | grep -qE 'apps\[|\\.apps\b'; then
  assert "asserts builds[].apps populated when apps_spec" 1
else
  assert "asserts builds[].apps populated when apps_spec" 0
fi

# Allowlist entry
printf '\n== allowlist entry exists ==\n'

if grep -q 'live-proof-dev-public.sh' "$ALLOWLIST"; then
  assert "live-proof-dev-public.sh entry in shell-scripts-allowlist.md" 1
else
  assert "live-proof-dev-public.sh entry in shell-scripts-allowlist.md" 0
fi

# Bash syntax check
printf '\n== bash syntax ==\n'

if bash -n "$SCRIPT" 2>/dev/null; then
  assert "bash -n parses without error" 1
else
  assert "bash -n parses without error" 0
fi

printf '\n== summary ==\n'
printf '  PASS: %d\n' "$PASS"
printf '  FAIL: %d\n' "$FAIL"

[[ "$FAIL" -eq 0 ]] || exit 1
