#!/usr/bin/env bash
# Hermetic gate: Caddyfile dev-public block MUST proxy /auth/* to loopzero-cloud.
#
# Without this, GET /auth/login?login_challenge=... (Hydra consent handler
# wired at cmd/main.go:508 → consent.Login → 302 to Kratos) falls through to
# the SPA file_server catch-all and the React Router renders "Page not found",
# which silently kills the entire `loopzero login --device-flow` chain (issue
# loopzero-cloud#112 BLOCKER P0).
#
# Per the canonical mirror pattern: the /auth/* handle MUST point at
# localhost:13002 (loopzero-cloud BFF) with `header_up Host cloud-dev.loopzero.example`,
# matching the existing /v1/* shape.
#
# Pure-text grep on the Caddyfile source; zero kubectl, zero network.
set -euo pipefail

CADDYFILE="${CADDYFILE:-/home/paulo/loopzero-cloud/loopzero/infra/local/Caddyfile}"
FAIL_COUNT=0

assert() {
  local description="$1"
  local actual="$2"
  local expected="$3"
  if [ "$actual" = "$expected" ]; then
    echo "PASS  $description"
  else
    echo "FAIL  $description  (got '$actual', expected '$expected')"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# A. /auth/* handle exists in the dev-public app-dev.loopzero.example block
DEV_PUBLIC_BLOCK=$(awk '/^app-dev\.loopzero\.dev \{/,/^}/' "$CADDYFILE")
HAS_AUTH_HANDLE=$(echo "$DEV_PUBLIC_BLOCK" | grep -cE '^[[:space:]]*handle[[:space:]]+/auth/\*[[:space:]]*\{' || true)
assert "A1 dev-public block has 'handle /auth/*'" "$HAS_AUTH_HANDLE" "1"

# B. Handle proxies to localhost:13002 (loopzero-cloud BFF, NOT Kratos at :14433)
AUTH_HANDLE_BLOCK=$(echo "$DEV_PUBLIC_BLOCK" | awk '/^[[:space:]]*handle[[:space:]]+\/auth\/\*[[:space:]]*\{/,/^[[:space:]]*\}[[:space:]]*$/')
HAS_BFF_PROXY=$(echo "$AUTH_HANDLE_BLOCK" | grep -c 'reverse_proxy localhost:13002' || true)
assert "B1 /auth/* proxies to loopzero-cloud BFF (localhost:13002)" "$HAS_BFF_PROXY" "1"

# C. NEVER proxies to Kratos (14433) — that would break Hydra consent flow
HAS_KRATOS_PROXY=$(echo "$AUTH_HANDLE_BLOCK" | grep -c 'localhost:14433' || true)
assert "C1 /auth/* does NOT proxy to Kratos (14433)" "$HAS_KRATOS_PROXY" "0"

# D. Sets `header_up Host cloud-dev.loopzero.example` (mirrors /v1/* canonical)
HAS_HOST_REWRITE=$(echo "$AUTH_HANDLE_BLOCK" | grep -c 'header_up Host cloud-dev.loopzero.example' || true)
assert "D1 /auth/* rewrites Host to cloud-dev.loopzero.example" "$HAS_HOST_REWRITE" "1"

# E. Issue #112 cited in inline comment audit trail
HAS_112_REF=$(echo "$DEV_PUBLIC_BLOCK" | grep -ciE '#112|loopzero-cloud#112' || true)
if [ "$HAS_112_REF" -ge 1 ]; then
  echo "PASS  E1 dev-public block cites issue #112"
else
  echo "FAIL  E1 dev-public block cites issue #112  (got 0, expected ≥1)"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# F. Allowlist entry (this test is part of `task quality:gates`)
ALLOWLIST="${ALLOWLIST:-/home/paulo/loopzero-cloud/loopzero/.claude/rules/shell-scripts-allowlist.md}"
HAS_ALLOWLIST=$(grep -c 'check-caddyfile-auth-route-canonical_test.sh' "$ALLOWLIST" || true)
assert "F1 allowlist cross-check" "$HAS_ALLOWLIST" "1"

echo
if [ "$FAIL_COUNT" -gt 0 ]; then
  echo "FAIL  $FAIL_COUNT assertion(s) failed"
  exit 1
fi
echo "PASS  all assertions passed"
exit 0
