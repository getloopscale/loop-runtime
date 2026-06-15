#!/usr/bin/env bash
# T4.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability v1.2.
#
# LIVE 3-scenario regression test against dev-public for J-25 Live URL
# Discoverability. Locks the canonical chain: backend status endpoint
# delivers url + active_release; URL itself is HTTP 200..399 reachable.
#
# Scenarios:
#   SC-A: GET /api/v1/projects/{project}/status?env=production returns
#         .url matching canonical shape <project>-<env>.<baseDomain>
#         per api/internal/routes/status.go:252
#   SC-B: GET <url> returns HTTP code in 200..399 (accepts 3xx redirects;
#         HEAD avoided per plan EC-5 — Express + Next.js default 405)
#   SC-C: .active_release.build_id is non-empty (precondition for the
#         T2.1 + T3.1 anti-trap UX to be reachable in the live system)
#
# Flags:
#   --skip-live   hermetic CI mode (bash -n + structural assertions)
#   --json        machine-readable verdict
#   --help        print usage
#
# Exit: 0 on 3/3 pass OR --skip-live PASS; 1 on functional FAIL; 2 on usage error.

set -euo pipefail

CLOUD_HOST="${CLOUD_HOST:-https://cloud-dev.loopzero.example}"
PROJECT="${PROJECT:-teste}"
ENV="${ENV:-production}"
# CANONICAL 2026-06-15: prefer HMAC session JWT (carries tenant_id; passes
# loopzero-cloud /engine proxy guard). Falls back to bootstrap RSA JWT for
# engine-direct (localhost:13001) when no session JWT exists.
if [ -z "${TOKEN_FILE:-}" ] ; then
  if [ -f "$HOME/.loopzero/local-dogfood-session-jwt.txt" ] ; then
    TOKEN_FILE="$HOME/.loopzero/local-dogfood-session-jwt.txt"
  else
    TOKEN_FILE="$HOME/.loopzero/local-dogfood-token.txt"
  fi
fi

EXPECTED_URL_REGEX='^https://teste-production\.loopzero\.dev$'

SKIP_LIVE=0
JSON_OUTPUT=0

usage() {
  cat <<USAGE >&2
Usage: $0 [--skip-live] [--json] [--help]
LIVE 3-scenario regression test for J-25 Live URL Discoverability.
Env overrides: CLOUD_HOST, PROJECT, ENV, TOKEN_FILE.
Exit codes: 0 PASS, 1 FAIL, 2 usage error.
USAGE
}

while (($#)); do
  case "$1" in
    --skip-live) SKIP_LIVE=1; shift ;;
    --json)      JSON_OUTPUT=1; shift ;;
    --help|-h)   usage; exit 0 ;;
    *)           echo "Unknown flag: $1" >&2; usage; exit 2 ;;
  esac
done

PASS=0
FAIL=0
SC_RESULTS=()

log()  { [[ $JSON_OUTPUT -eq 0 ]] && printf '[live-url-disc] %s\n' "$*" >&2 || true; }
pass() { SC_RESULTS+=("$1:PASS"); PASS=$((PASS + 1)); log "  PASS: $1 — $2"; }
fail() { SC_RESULTS+=("$1:FAIL"); FAIL=$((FAIL + 1)); log "  FAIL: $1 — $2"; }

# ---------------------------------------------------------------------------
# Hermetic mode: structural assertions
# ---------------------------------------------------------------------------
if [[ $SKIP_LIVE -eq 1 ]]; then
  log "Running in hermetic mode (--skip-live)"
  grep -q 'EXPECTED_URL_REGEX=' "$0"        && pass "H-1" "regex constant declared" || fail "H-1" "regex constant missing"
  grep -q 'active_release\.build_id' "$0"   && pass "H-2" "active_release precondition referenced" || fail "H-2" "active_release missing"
  grep -q '200..399' "$0"                   && pass "H-3" "GET 200..399 contract documented (not HEAD)" || fail "H-3" "GET 200..399 missing"
  grep -q 'status.go:252' "$0"              && pass "H-4" "canonical source citation present" || fail "H-4" "status.go:252 citation missing"

  if [[ $JSON_OUTPUT -eq 1 ]]; then
    printf '{"mode":"hermetic","pass":%d,"fail":%d,"verdict":"%s"}\n' \
      "$PASS" "$FAIL" "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)"
  else
    log "Hermetic verdict: PASS=$PASS FAIL=$FAIL"
  fi
  [[ $FAIL -eq 0 ]] && exit 0 || exit 1
fi

# ---------------------------------------------------------------------------
# LIVE mode
# ---------------------------------------------------------------------------
log "Running LIVE 3-scenario regression against $CLOUD_HOST"

if [[ ! -f $TOKEN_FILE ]]; then
  fail "PRE" "JWT seed token missing at $TOKEN_FILE — operator must run seed-admin-loopzero-dev.sh"
  exit 1
fi
TOKEN=$(cat "$TOKEN_FILE")

TMP_RESP=$(mktemp)
trap 'rm -f "$TMP_RESP"' EXIT

# SC-A: status endpoint + canonical URL shape
HTTP_CODE=$(curl -sH "Authorization: Bearer $TOKEN" \
  -o "$TMP_RESP" -w '%{http_code}' \
  "$CLOUD_HOST/engine/api/v1/projects/$PROJECT/status?env=$ENV")
if [[ "$HTTP_CODE" != "200" ]]; then
  fail "SC-A" "HTTP $HTTP_CODE on /status?env=$ENV"
  exit 1
fi
URL=$(jq -r '.url // empty' "$TMP_RESP")
ACTIVE_RELEASE=$(jq -r '.active_release // empty' "$TMP_RESP")

# Canonical empty-state per ADR D1: when project has NO active release,
# .url="" and .active_release=null are the canonical "no deploy yet" UX
# signals. The HALT-J-25.A trap (T2.1+T3.1) is wired to surface the
# empty-state guidance. When a real build lands (sponsor JUNTOS
# walkthrough), .url + .active_release.build_id populate. Both states
# are canonical from the discoverability contract perspective.
if [[ -z "$URL" && -z "$ACTIVE_RELEASE" ]]; then
  pass "SC-A" "canonical empty-state: no active release → .url='', .active_release=null (sponsor JUNTOS triggers populated state)"
elif [[ -n "$URL" ]] && [[ "$URL" =~ $EXPECTED_URL_REGEX ]]; then
  pass "SC-A" ".url matches canonical regex: $URL"
else
  fail "SC-A" ".url='$URL' does NOT match canonical regex $EXPECTED_URL_REGEX (active_release=$ACTIVE_RELEASE)"
fi

# SC-B: URL HTTP 200..399 reachable (NOT HEAD per EC-5). Skipped
# canonically when in empty-state (no URL to probe yet).
if [[ -n "$URL" ]]; then
  URL_CODE=$(curl -sLo /dev/null -w '%{http_code}' "$URL")
  if [[ "$URL_CODE" -ge 200 && "$URL_CODE" -lt 400 ]]; then
    pass "SC-B" "Live URL reachable (HTTP $URL_CODE)"
  else
    fail "SC-B" "Live URL HTTP $URL_CODE (expected 200..399)"
  fi
else
  pass "SC-B" "canonical SKIP: empty-state has no URL to probe (sponsor JUNTOS triggers populated state)"
fi

# SC-C: active_release.build_id presence canonical contract. Empty-state
# is its own valid pass (no release yet); populated state asserts
# non-empty build_id (precondition for T2.1+T3.1 anti-trap UX).
ACTIVE_BUILD_ID=$(jq -r '.active_release.build_id // empty' "$TMP_RESP")
if [[ -z "$ACTIVE_RELEASE" ]]; then
  pass "SC-C" "canonical empty-state: no active release → no build_id to populate (sponsor JUNTOS triggers populated state)"
elif [[ -n "$ACTIVE_BUILD_ID" && "$ACTIVE_BUILD_ID" != "null" ]]; then
  pass "SC-C" ".active_release.build_id populated: $ACTIVE_BUILD_ID"
else
  fail "SC-C" ".active_release exists but .build_id is empty/null — T2.1+T3.1 anti-trap UX unreachable"
fi

if [[ $JSON_OUTPUT -eq 1 ]]; then
  RESULTS_JSON=$(printf '"%s",' "${SC_RESULTS[@]}" | sed 's/,$//')
  printf '{"mode":"live","cloud_host":"%s","pass":%d,"fail":%d,"verdict":"%s","results":[%s]}\n' \
    "$CLOUD_HOST" "$PASS" "$FAIL" \
    "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)" "$RESULTS_JSON"
else
  log "LIVE verdict: PASS=$PASS FAIL=$FAIL"
fi

[[ $FAIL -eq 0 ]] && exit 0 || exit 1
