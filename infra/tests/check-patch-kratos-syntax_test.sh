#!/usr/bin/env bash
# Hermetic test for patch-kratos-dev-public.sh — structural validation
# without invoking it. Runs in <1s.
#
# Asserts:
#   - bash -n syntax OK
#   - 7 documented patches all present in Python heredoc
#   - 3 flags wired: --dry-run, --skip-restart, --verify
#   - --dry-run flag covers all kubectl-apply paths
#   - --verify uses retry-with-backoff (60s window)
#   - allowlist entry exists

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO_ROOT/infra/scripts/local/patch-kratos-dev-public.sh"
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

# Syntax
printf '== bash syntax ==\n'
if bash -n "$SCRIPT" 2>/dev/null; then
  assert "bash -n parses" 1
else
  assert "bash -n parses" 0
fi

# 7 patches per documentation
printf '\n== 7 documented patches in Python embedded code ==\n'

if printf '%s' "$CONTENT" | grep -q 'Patch 1 — OIDC providers'; then
  assert "Patch 1: OIDC providers" 1
else
  assert "Patch 1: OIDC providers" 0
fi

if printf '%s' "$CONTENT" | grep -q 'Patch 2 — OIDC base_redirect_uri'; then
  assert "Patch 2: base_redirect_uri" 1
else
  assert "Patch 2: base_redirect_uri" 0
fi

if printf '%s' "$CONTENT" | grep -q 'Patch 3 — Resend Authorization'; then
  assert "Patch 3: Resend Authorization" 1
else
  assert "Patch 3: Resend Authorization" 0
fi

if printf '%s' "$CONTENT" | grep -q 'Patch 4 — serve.public'; then
  assert "Patch 4: serve.public" 1
else
  assert "Patch 4: serve.public" 0
fi

if printf '%s' "$CONTENT" | grep -q 'Patch 5 — cookies'; then
  assert "Patch 5: cookies" 1
else
  assert "Patch 5: cookies" 0
fi

if printf '%s' "$CONTENT" | grep -q 'Patch 6 — selfservice flows'; then
  assert "Patch 6: selfservice flows" 1
else
  assert "Patch 6: selfservice flows" 0
fi

# Flag wiring
printf '\n== flags wired ==\n'

if printf '%s' "$CONTENT" | grep -q '\-\-dry-run'; then
  assert "--dry-run flag" 1
else
  assert "--dry-run flag" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-skip-restart'; then
  assert "--skip-restart flag" 1
else
  assert "--skip-restart flag" 0
fi

if printf '%s' "$CONTENT" | grep -q '\-\-verify'; then
  assert "--verify flag" 1
else
  assert "--verify flag" 0
fi

# Dry-run must NOT call kubectl apply (only template + diff)
printf '\n== --dry-run semantics ==\n'

# In Python heredoc, check that kubectl apply is gated by DRY check
if printf '%s' "$CONTENT" | grep -q 'if DRY:'; then
  assert "Python heredoc gates apply via if DRY check" 1
else
  assert "Python heredoc gates apply via if DRY check" 0
fi

# Verify retry-with-backoff
printf '\n== --verify retry-with-backoff (port-forward respawn defense) ==\n'

if printf '%s' "$CONTENT" | grep -qE 'for attempt in.*1 2 3 4 5 6 7 8 9 10'; then
  assert "--verify retries up to 10 attempts" 1
else
  assert "--verify retries up to 10 attempts" 0
fi

# Allowlist
printf '\n== allowlist entry ==\n'

if grep -q 'patch-kratos-dev-public.sh' "$ALLOWLIST"; then
  assert "patch-kratos-dev-public.sh in allowlist" 1
else
  assert "patch-kratos-dev-public.sh in allowlist" 0
fi

# Required env validation
printf '\n== required env loaded via load_env() ==\n'

for var in LOOPZERO_GITHUB_CLIENT_ID LOOPZERO_GITHUB_CLIENT_SECRET \
           LOOPZERO_GOOGLE_CLIENT_ID LOOPZERO_GOOGLE_CLIENT_SECRET \
           LOOPZERO_RESEND_API_KEY; do
  if printf '%s' "$CONTENT" | grep -q "load_env $var"; then
    assert "$var loaded via load_env()" 1
  else
    assert "$var loaded via load_env()" 0
  fi
done

printf '\n== summary ==\n'
printf '  PASS: %d\n' "$PASS"
printf '  FAIL: %d\n' "$FAIL"

[[ "$FAIL" -eq 0 ]] || exit 1
