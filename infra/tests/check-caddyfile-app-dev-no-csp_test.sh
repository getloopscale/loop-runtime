#!/usr/bin/env bash
# Hermetic test for plan caddyfile-csp-vite-dev-vs-prod-bundle T1.1.
#
# Asserts that the `app-dev.loopzero.example { ... }` block in `infra/local/Caddyfile`:
#   1. Has NO `Content-Security-Policy` directive (per ADR D1 — let the upstream
#      `<meta>` CSP in `dashboard/index.html` rule; browsers enforce the
#      STRICTER intersection of header CSP vs `<meta>` CSP).
#   2. Has NO `handle /src/*`, `handle /@vite/*`, `handle /node_modules/.vite/*`
#      404 blocks (per ADR D2 — those break Vite HMR module resolution; prod
#      info-disclosure is handled by `dashboard/nginx.conf:try_files`).
#   3. Preserves `X-Frame-Options "DENY"`, `X-Content-Type-Options "nosniff"`,
#      `Referrer-Policy "strict-origin-when-cross-origin"`, and HSTS — per
#      ADR D3 these do NOT intersect with `<meta>` CSP and close real attack
#      surface.
#   4. Preserves `reverse_proxy @ws localhost:5173` (the Vite dev upstream).
#
# Hermetic: no kubectl, no curl, no network. Pure regex on the file.
#
# Allowlist entry: see .claude/rules/shell-scripts-allowlist.md
# Plan: .claude/knowledge-base/plans/caddyfile-csp-vite-dev-vs-prod-bundle-plan.md v1.1

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
CADDYFILE="${REPO_ROOT}/local/Caddyfile"

PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1" >&2; FAIL=$((FAIL+1)); }

# ----------------------------------------------------------------------
# Helper: extract the app-dev.loopzero.example { ... } block via brace balance
# ----------------------------------------------------------------------
# EC-2 absorbed v1.1 — explicit block-bound helper. awk one-liner that
# enters the block on `app-dev.loopzero.example {` and counts brace depth, exiting
# when depth returns to 0.
extract_app_dev_block() {
  awk '
    /^app-dev\.loopzero\.dev[[:space:]]+\{/ { in_block=1; depth=1; next }
    in_block {
      n_open  = gsub(/\{/, "&")
      n_close = gsub(/\}/, "&")
      depth += n_open - n_close
      print
      if (depth <= 0) { exit }
    }
  ' "$CADDYFILE"
}

# ----------------------------------------------------------------------
# Test 0: file exists
# ----------------------------------------------------------------------
echo "[T1.1] check-caddyfile-app-dev-no-csp"
if [ -f "$CADDYFILE" ]; then
  pass "Caddyfile exists at $CADDYFILE"
else
  fail "Caddyfile NOT found at $CADDYFILE"
  echo "RESULT: $PASS pass / $FAIL fail"
  exit 1
fi

APP_DEV_BLOCK="$(extract_app_dev_block)"

# ----------------------------------------------------------------------
# Test 1: assert_app_dev_block_bounded — extraction succeeded
# ----------------------------------------------------------------------
if [ -n "$APP_DEV_BLOCK" ] && echo "$APP_DEV_BLOCK" | grep -q "reverse_proxy"; then
  pass "assert_app_dev_block_bounded — block extracted, contains reverse_proxy"
else
  fail "assert_app_dev_block_bounded — could not extract app-dev block (or extracted empty)"
fi

# ----------------------------------------------------------------------
# Test 2: assert_no_csp_in_app_dev_block
# ----------------------------------------------------------------------
if echo "$APP_DEV_BLOCK" | grep -q "Content-Security-Policy"; then
  fail "assert_no_csp_in_app_dev_block — CSP directive STILL present in app-dev block:"
  echo "$APP_DEV_BLOCK" | grep "Content-Security-Policy" | head -1 | sed 's/^/      /'
else
  pass "assert_no_csp_in_app_dev_block — zero Content-Security-Policy occurrences"
fi

# ----------------------------------------------------------------------
# Test 3: assert_no_vite_handle_blocks_in_app_dev_block
# ----------------------------------------------------------------------
for vite_path in "handle /src/\*" "handle /@vite/\*" "handle /node_modules/.vite/\*"; do
  unescaped="$(echo "$vite_path" | sed 's/\\\*/*/g')"
  if echo "$APP_DEV_BLOCK" | grep -qF "$unescaped"; then
    fail "assert_no_vite_handle_blocks_in_app_dev_block — \`$unescaped\` STILL present"
  else
    pass "assert_no_vite_handle_blocks_in_app_dev_block — no \`$unescaped\`"
  fi
done

# ----------------------------------------------------------------------
# Test 4: assert_xfo_preserved_in_app_dev_block
# ----------------------------------------------------------------------
if echo "$APP_DEV_BLOCK" | grep -qE 'X-Frame-Options[[:space:]]+"DENY"'; then
  pass "assert_xfo_preserved_in_app_dev_block — X-Frame-Options DENY present"
else
  fail "assert_xfo_preserved_in_app_dev_block — X-Frame-Options DENY MISSING (regression of #44)"
fi

# ----------------------------------------------------------------------
# Test 5: assert_xcto_preserved_in_app_dev_block
# ----------------------------------------------------------------------
if echo "$APP_DEV_BLOCK" | grep -qE 'X-Content-Type-Options[[:space:]]+"nosniff"'; then
  pass "assert_xcto_preserved_in_app_dev_block — X-Content-Type-Options nosniff present"
else
  fail "assert_xcto_preserved_in_app_dev_block — X-Content-Type-Options nosniff MISSING (regression of #45)"
fi

# ----------------------------------------------------------------------
# Test 6: assert_referrer_preserved_in_app_dev_block
# ----------------------------------------------------------------------
if echo "$APP_DEV_BLOCK" | grep -qE 'Referrer-Policy[[:space:]]+"strict-origin-when-cross-origin"'; then
  pass "assert_referrer_preserved_in_app_dev_block — Referrer-Policy present"
else
  fail "assert_referrer_preserved_in_app_dev_block — Referrer-Policy MISSING (regression of #45)"
fi

# ----------------------------------------------------------------------
# Test 7: assert_hsts_preserved_in_app_dev_block
# ----------------------------------------------------------------------
if echo "$APP_DEV_BLOCK" | grep -qE 'Strict-Transport-Security[[:space:]]+"max-age=31536000'; then
  pass "assert_hsts_preserved_in_app_dev_block — HSTS max-age=31536000 present"
else
  fail "assert_hsts_preserved_in_app_dev_block — HSTS MISSING (regression of #45)"
fi

# ----------------------------------------------------------------------
# Test 8: assert_reverse_proxy_preserved
# ----------------------------------------------------------------------
proxy_count=$(echo "$APP_DEV_BLOCK" | grep -c "reverse_proxy @ws localhost:5173" || true)
if [ "$proxy_count" -eq 1 ]; then
  pass "assert_reverse_proxy_preserved — reverse_proxy @ws localhost:5173 present (exactly once)"
else
  fail "assert_reverse_proxy_preserved — expected exactly 1 'reverse_proxy @ws localhost:5173', got $proxy_count"
fi

# ----------------------------------------------------------------------
# RESULT
# ----------------------------------------------------------------------
echo ""
echo "RESULT: $PASS pass / $FAIL fail"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
