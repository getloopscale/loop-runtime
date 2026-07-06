#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero/loopzero#162 closure.
#
# BLOCKER P0 (2026-07-01): post-signup redirect target /auth/check-email
# returned 404 Caddy raw ("page not found") because the Caddyfile
# handle /auth/* captured EVERY path under /auth/* and forwarded to the
# loopzero-cloud BFF which only registered 4 handlers (login, login/complete,
# consent, device) — /auth/check-email + /auth/verify were dead paths.
# Signup funnel broken: newly-registered users landed on 404 instead of
# the "check your email" instructions.
#
# Root cause (5 Whys):
#   1. Why 404? Caddy hit BFF; BFF returned 404.
#   2. Why BFF 404? No handler registered for /auth/check-email.
#   3. Why no handler? BFF only serves OAuth flow endpoints; check-email
#      is a UI dead-state page, not an auth flow.
#   4. Why did Caddy route to BFF? handle /auth/* captured EVERYTHING.
#   5. Systemic: no named-matcher scoping the /auth/* → BFF proxy to the
#      actual 4 OAuth routes, so any new URL under /auth/* silently
#      goes to a dead BFF path.
#
# Fix: Caddyfile @auth_bff named matcher listing the 4 BFF routes only;
#      dashboard SPA registers /auth/check-email + /auth/verify routes so
#      they fall through the Caddy catch-all handle to the SPA
#      file_server (which serves index.html + React Router picks it up).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL. Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25.
#
# Scenarios:
#   A GET /auth/check-email → HTTP 200 + Content-Type text/html
#   B GET /auth/verify      → HTTP 200 + Content-Type text/html
#
# Flags:
#   --skip-live       hermetic CI mode (no HTTP; just print structural verdict)
#   --scenario=A|B    run only one scenario (default: both)
#   --json            emit JSON verdict
#   --help            print this help

set -euo pipefail

APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON=0
SCENARIO_FILTER=""
PASS_COUNT=0
FAIL_COUNT=0
declare -a RESULTS=()

usage() {
  sed -n '2,40p' "$0"
}

log()  { echo "[signup-162] $*"; }
pass() { PASS_COUNT=$((PASS_COUNT+1)); RESULTS+=("PASS $1"); log "PASS $1"; }
fail() { FAIL_COUNT=$((FAIL_COUNT+1)); RESULTS+=("FAIL $1"); log "FAIL $1"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live)          SKIP_LIVE=1 ;;
    --scenario=*)         SCENARIO_FILTER="${1#*=}" ;;
    --json)               JSON=1 ;;
    --help|-h)            usage; exit 0 ;;
    *) echo "Unknown flag: $1"; usage; exit 1 ;;
  esac
  shift
done

should_run() {
  local id="$1"
  [ -z "$SCENARIO_FILTER" ] || [ "$SCENARIO_FILTER" = "$id" ]
}

preflight() {
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "preflight SKIPPED (--skip-live hermetic mode)"
    return 0
  fi
  log "preflight probing $APP_URL/register (SPA reachable)"
  local code
  code=$(curl -sSk -o /dev/null -w "%{http_code}" "$APP_URL/register" || echo "000")
  if [ "$code" = "200" ]; then
    pass "preflight /register → 200"
  else
    fail "preflight /register → $code (expected 200; check Caddy + dashboard bundle)"
    return 1
  fi
}

scenario_a() {
  should_run "A" || return 0
  log "Scenario A: GET /auth/check-email must return 200 (SPA CheckEmailPage)"
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "SKIP scenario A (--skip-live)"
    return 0
  fi

  local out code ctype
  out=$(curl -sSk -o /tmp/signup-162-a.body -D /tmp/signup-162-a.hdr -w "%{http_code}" "$APP_URL/auth/check-email" || echo "000")
  code="$out"
  ctype=$(grep -i '^content-type:' /tmp/signup-162-a.hdr | head -1 | tr -d '\r' || true)

  if [ "$code" = "200" ]; then
    pass "A1 /auth/check-email → 200 (not Caddy 404 raw)"
  else
    fail "A1 /auth/check-email → $code (expected 200; #162 regression)"
  fi

  if echo "$ctype" | grep -qi 'text/html'; then
    pass "A2 Content-Type text/html (SPA index.html served)"
  else
    fail "A2 Content-Type=$ctype (expected text/html; probable BFF 404 raw)"
  fi

  # Regression-protector: the body must NOT be the Caddy raw 404 string.
  if grep -q "404 page not found" /tmp/signup-162-a.body; then
    fail "A3 body contains 'Caddy 404 page not found' (regression)"
  else
    pass "A3 body is not Caddy raw 404"
  fi
}

scenario_b() {
  should_run "B" || return 0
  log "Scenario B: GET /auth/verify must return 200 (SPA Navigate to /verification)"
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "SKIP scenario B (--skip-live)"
    return 0
  fi

  local out code ctype
  out=$(curl -sSk -o /tmp/signup-162-b.body -D /tmp/signup-162-b.hdr -w "%{http_code}" "$APP_URL/auth/verify" || echo "000")
  code="$out"
  ctype=$(grep -i '^content-type:' /tmp/signup-162-b.hdr | head -1 | tr -d '\r' || true)

  if [ "$code" = "200" ]; then
    pass "B1 /auth/verify → 200 (not Caddy 404 raw)"
  else
    fail "B1 /auth/verify → $code (expected 200; #162 regression)"
  fi

  if echo "$ctype" | grep -qi 'text/html'; then
    pass "B2 Content-Type text/html (SPA served)"
  else
    fail "B2 Content-Type=$ctype (expected text/html)"
  fi

  if grep -q "404 page not found" /tmp/signup-162-b.body; then
    fail "B3 body contains 'Caddy 404 page not found' (regression)"
  else
    pass "B3 body is not Caddy raw 404"
  fi
}

cleanup() {
  rm -f /tmp/signup-162-*.body /tmp/signup-162-*.hdr
}
trap cleanup EXIT INT TERM

preflight
scenario_a
scenario_b

echo
if [ "$JSON" -eq 1 ]; then
  printf '{"issue":"loopzero/loopzero#162","slug":"signup-auth-check-email","pass":%d,"fail":%d,"skip_live":%s,"scenario_filter":"%s","results":[' \
    "$PASS_COUNT" "$FAIL_COUNT" "$([ $SKIP_LIVE -eq 1 ] && echo true || echo false)" "$SCENARIO_FILTER"
  first=1
  for r in "${RESULTS[@]}"; do
    [ $first -eq 1 ] || printf ','
    printf '"%s"' "${r//\"/\\\"}"
    first=0
  done
  printf ']}\n'
else
  echo "=== #162 summary: $PASS_COUNT pass / $FAIL_COUNT fail ==="
fi
exit $(( FAIL_COUNT > 0 ? 1 : 0 ))
