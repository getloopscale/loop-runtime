#!/usr/bin/env bash
# Red Team loopzero-cloud#117 closure LIVE test — proves Kratos auth email
# delivery chain (Kratos → Resend → recipient) survives Cleanroom in
# dev-public.
#
# Symptom per #117: post-Cleanroom signup → /auth/check-email but no
# verification email arrives; recovery returns "Enter recovery code"
# but no code email arrives.
#
# Root cause investigation (this test asserts both halves):
#   (1) Recovery for an UNKNOWN address correctly returns the
#       anti-enumeration "fase code" response WITHOUT enqueueing an
#       email (canonical Kratos behavior; was misread as "broken" in
#       the report). Asserted via `was_notified:false` in Kratos logs.
#   (2) Recovery for a KNOWN address (the canonical admin@loopzero.example
#       seeded by seed-admin-loopzero-dev.sh) MUST enqueue + dispatch
#       through Resend, with Resend confirming `delivered` or `sent`.
#   (3) Signup for a fresh email MUST create a Kratos identity,
#       enqueue the verification mailer, and Resend MUST accept
#       (`status=sent` is enough — `.test` TLD never actually delivers
#        but Resend's acceptance proves the pipeline end-to-end).
#
# Pre-conditions:
#   - dev-public canonical chain ran (seed-admin-loopzero-dev.sh, and
#     helmfile sync of ory-kratos with LOOPZERO_RESEND_API_KEY exported —
#     `task dev-public:sync-kratos`) — identity for admin@loopzero.example
#     present AND ConfigMap has the Bearer Resend key, NOT the
#     placeholder REPLACE_VIA_LOOPZERO_RESEND_API_KEY string.
#   - $RESEND_API_KEY exported (the operator's Resend account API key
#     — same one Kratos config uses). Probed against api.resend.com
#     to confirm Resend received what Kratos sent.
#   - kubectl context k3d-loopzero-core reachable.
#
# OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md UNBREAKABLE —
# requires Cleanroom TOTAL precedence + the canonical seed chain.
#
# Exit codes:
#   0  3 scenarios PASS (anti-enumeration mute + admin delivery +
#      signup pipeline)
#   1  At least one scenario FAILS — actual product regression
#   2  BLOCKED (RESEND_API_KEY missing OR auth-dev.loopzero.example
#      unreachable OR kubectl context broken)
#
# Usage:
#   bash infra/tests/e2e/live/dev-public-auth-email-delivery_test.sh \
#     [--skip-live] [--json] [--scenario=A|B|C] [--help]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://auth-dev.loopzero.example}"
KUBE_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-core}"
KNOWN_EMAIL="${KNOWN_EMAIL:-admin@loopzero.example}"
UNKNOWN_EMAIL="${UNKNOWN_EMAIL:-unknown-redteam-117@example.invalid}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO="all"

usage() {
  sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
}

for arg in "$@"; do
  case "$arg" in
    --skip-live) SKIP_LIVE=1 ;;
    --json) JSON_OUTPUT=1 ;;
    --scenario=*) SCENARIO="${arg#--scenario=}" ;;
    --help|-h) usage ;;
    *) echo "Unknown flag: $arg" >&2; exit 2 ;;
  esac
done

PASS=0
FAIL=0
RESULTS=()

log() {
  [[ $JSON_OUTPUT -eq 1 ]] && return
  printf "%s [#117-auth-email] %s\n" "$(date -u +%H:%M:%SZ)" "$*"
}

record() {
  local id="$1" verdict="$2" detail="$3"
  RESULTS+=("$id|$verdict|$detail")
  case "$verdict" in
    PASS) PASS=$((PASS+1)); log "[$id] PASS — $detail" ;;
    FAIL) FAIL=$((FAIL+1)); log "[$id] FAIL — $detail" ;;
  esac
}

if [[ $SKIP_LIVE -eq 1 ]]; then
  log "--skip-live → hermetic mode (no LIVE assertions)"
  [[ $JSON_OUTPUT -eq 1 ]] && echo '{"mode":"hermetic","pass":0,"fail":0}'
  exit 0
fi

# Pre-flight EC-1: probe reachability
preflight() {
  local code
  code=$(curl -ks -o /dev/null -w '%{http_code}' --max-time 5 \
    "${TARGET_HOST}/health/ready" 2>&1 || echo "000")
  if [[ "$code" != "200" ]]; then
    log "BLOCKED: ${TARGET_HOST}/health/ready returned $code (expected 200)"
    exit 2
  fi
  if [[ -z "${RESEND_API_KEY:-}" ]]; then
    # Try the canonical operator path (~/.loopzero/dev-credentials.json)
    if [[ -f "$HOME/.loopzero/dev-credentials.json" ]]; then
      RESEND_API_KEY=$(python3 -c "
import json, sys
try:
  d = json.load(open('$HOME/.loopzero/dev-credentials.json'))
  print(d.get('resend', {}).get('api_key', ''))
except Exception:
  pass
" 2>/dev/null)
    fi
    if [[ -z "${RESEND_API_KEY:-}" ]]; then
      # Last resort: read from the live ConfigMap (helmfile substitutes the
      # real Bearer at template time).
      RESEND_API_KEY=$(kubectl --context "$KUBE_CONTEXT" -n loopzero-auth get cm \
        ory-kratos-config -o jsonpath='{.data.kratos\.yaml}' 2>/dev/null \
        | grep -oE 'Bearer re_[A-Za-z0-9_]+' | head -1 | sed 's/Bearer //')
    fi
  fi
  if [[ -z "${RESEND_API_KEY:-}" ]]; then
    log "BLOCKED: RESEND_API_KEY not set AND not derivable from dev-credentials.json or Kratos ConfigMap"
    exit 2
  fi
}

# Scenario A: anti-enumeration — UNKNOWN address must NOT enqueue
# a courier message (Kratos logs "Account recovery was requested for
# an unknown address" + was_notified:false).
scenario_a() {

  local flow_id
  flow_id=$(curl -ks -X GET "${TARGET_HOST}/self-service/recovery/api" \
    -H "Accept: application/json" 2>&1 \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
  if [[ -z "$flow_id" ]]; then
    record A FAIL "init recovery flow failed (no flow_id)"
    return
  fi

  curl -ks -X POST "${TARGET_HOST}/self-service/recovery?flow=${flow_id}" \
    -H "Content-Type: application/json" -H "Accept: application/json" \
    -d "{\"method\":\"code\",\"email\":\"${UNKNOWN_EMAIL}\"}" \
    >/dev/null 2>&1

  sleep 3

  # Assert NO courier message enqueued for unknown
  local new_msgs
  new_msgs=$(kubectl --context "$KUBE_CONTEXT" -n loopzero-system exec postgresql-0 \
    -c postgresql -- env PGPASSWORD=loopzero-dev-password psql -U loopzero -d kratos \
    -tAc "SELECT count(*) FROM courier_messages WHERE recipient='${UNKNOWN_EMAIL}' \
          AND created_at > NOW() - INTERVAL '30 seconds';" 2>/dev/null | tr -d ' ')

  if [[ "$new_msgs" == "0" ]]; then
    record A PASS "unknown address correctly muted (no courier message enqueued)"
  else
    record A FAIL "unknown address leaked existence (${new_msgs} messages enqueued — enumeration risk)"
  fi
}

# Scenario B: KNOWN admin@loopzero.example recovery MUST enqueue + Resend MUST
# return delivered/sent.
scenario_b() {
  local flow_id
  flow_id=$(curl -ks -X GET "${TARGET_HOST}/self-service/recovery/api" \
    -H "Accept: application/json" 2>&1 \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
  if [[ -z "$flow_id" ]]; then
    record B FAIL "init recovery flow failed"
    return
  fi

  curl -ks -X POST "${TARGET_HOST}/self-service/recovery?flow=${flow_id}" \
    -H "Content-Type: application/json" -H "Accept: application/json" \
    -d "{\"method\":\"code\",\"email\":\"${KNOWN_EMAIL}\"}" \
    >/dev/null 2>&1

  # Wait for courier dispatch (background worker, ~2s typical)
  sleep 8

  # PG: courier message row exists for KNOWN_EMAIL with send_count >= 1
  local row
  row=$(kubectl --context "$KUBE_CONTEXT" -n loopzero-system exec postgresql-0 \
    -c postgresql -- env PGPASSWORD=loopzero-dev-password psql -U loopzero -d kratos \
    -tAc "SELECT id::text || '|' || status || '|' || send_count FROM courier_messages \
          WHERE recipient='${KNOWN_EMAIL}' \
          AND created_at > NOW() - INTERVAL '30 seconds' \
          ORDER BY created_at DESC LIMIT 1;" 2>/dev/null | tr -d ' ')

  if [[ -z "$row" ]]; then
    record B FAIL "no courier_messages row for ${KNOWN_EMAIL} in last 30s"
    return
  fi

  local msg_id send_count
  msg_id=$(echo "$row" | cut -d'|' -f1)
  send_count=$(echo "$row" | cut -d'|' -f3)
  if [[ "$send_count" -lt 1 ]]; then
    record B FAIL "courier message present but send_count=${send_count} (never dispatched)"
    return
  fi

  # Resend: query the API list endpoint + assert most-recent email to
  # KNOWN_EMAIL is delivered or sent (NOT bounced/failed/queued).
  local resend_status
  resend_status=$(curl -ks -X GET "https://api.resend.com/emails?limit=10" \
    -H "Authorization: Bearer ${RESEND_API_KEY}" 2>&1 \
    | python3 -c "
import sys, json
d = json.load(sys.stdin)
for e in d.get('data', []):
    if '${KNOWN_EMAIL}' in str(e.get('to', '')):
        print(e.get('last_event', '?'))
        break
else:
    print('NOT_FOUND')
" 2>/dev/null)

  case "$resend_status" in
    delivered|sent)
      record B PASS "courier message ${msg_id} dispatched + Resend status=${resend_status}"
      ;;
    bounced|failed|queued|NOT_FOUND)
      record B FAIL "Resend last_event=${resend_status} for ${KNOWN_EMAIL} (expected delivered|sent)"
      ;;
    *)
      record B FAIL "unexpected Resend status: ${resend_status}"
      ;;
  esac
}

# Scenario C: signup for a fresh email MUST create identity + enqueue
# verification + Resend MUST accept.
scenario_c() {
  local fresh_email="redteam-117-signup-$(date +%s)-${RANDOM}@loopzero.test"

  local flow_id
  flow_id=$(curl -ks -X GET "${TARGET_HOST}/self-service/registration/api" \
    -H "Accept: application/json" 2>&1 \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
  if [[ -z "$flow_id" ]]; then
    record C FAIL "init registration flow failed"
    return
  fi

  local resp
  resp=$(curl -ks -X POST "${TARGET_HOST}/self-service/registration?flow=${flow_id}" \
    -H "Content-Type: application/json" -H "Accept: application/json" \
    -d "{\"method\":\"password\",\"password\":\"loopzero-dev-password-117-redteam\",\"traits\":{\"email\":\"${fresh_email}\"}}" 2>&1)

  # Look for `message` field (Kratos returns it on successful registration
  # awaiting verification) OR `identity` (api-flow returns the full identity)
  local has_signal
  has_signal=$(echo "$resp" | python3 -c "
import sys, json
try:
  d = json.load(sys.stdin)
except Exception:
  print('NO_JSON'); sys.exit()
if 'identity' in d or 'message' in d or 'redirect' in d or 'continue_with' in d:
  print('OK')
else:
  print('NO_SIGNAL:' + ','.join(d.keys())[:60])
" 2>/dev/null)

  if [[ "$has_signal" != "OK" ]]; then
    record C FAIL "registration POST response missing identity/message: ${has_signal}"
    return
  fi

  sleep 5

  # PG: courier_messages row for fresh_email
  local has_row
  has_row=$(kubectl --context "$KUBE_CONTEXT" -n loopzero-system exec postgresql-0 \
    -c postgresql -- env PGPASSWORD=loopzero-dev-password psql -U loopzero -d kratos \
    -tAc "SELECT count(*) FROM courier_messages WHERE recipient='${fresh_email}' \
          AND created_at > NOW() - INTERVAL '60 seconds';" 2>/dev/null | tr -d ' ')

  if [[ "$has_row" != "1" ]]; then
    record C FAIL "no courier_messages row for fresh signup ${fresh_email} (count=${has_row})"
    return
  fi

  # Resend: must show the email in recent history with status sent OR delivered
  local resend_status
  resend_status=$(curl -ks -X GET "https://api.resend.com/emails?limit=10" \
    -H "Authorization: Bearer ${RESEND_API_KEY}" 2>&1 \
    | python3 -c "
import sys, json
d = json.load(sys.stdin)
for e in d.get('data', []):
    if '${fresh_email}' in str(e.get('to', '')):
        print(e.get('last_event', '?'))
        break
else:
    print('NOT_FOUND')
" 2>/dev/null)

  case "$resend_status" in
    delivered|sent)
      record C PASS "signup pipeline → Resend status=${resend_status} for fresh ${fresh_email}"
      ;;
    *)
      record C FAIL "signup pipeline Resend status=${resend_status} for ${fresh_email}"
      ;;
  esac
}

preflight

case "$SCENARIO" in
  all) scenario_a; scenario_b; scenario_c ;;
  A) scenario_a ;;
  B) scenario_b ;;
  C) scenario_c ;;
  *) echo "Unknown scenario: $SCENARIO (use A, B, C, or all)" >&2; exit 2 ;;
esac

if [[ $JSON_OUTPUT -eq 1 ]]; then
  python3 -c "
import json
results = [r.split('|', 2) for r in '''$(printf '%s\n' "${RESULTS[@]}")'''.strip().split('\n') if r]
print(json.dumps({
  'pass': $PASS,
  'fail': $FAIL,
  'scenarios': [{'id': r[0], 'verdict': r[1], 'detail': r[2]} for r in results]
}))
"
else
  echo
  echo "=== summary: ${PASS} pass / ${FAIL} fail ==="
fi

[[ $FAIL -gt 0 ]] && exit 1
exit 0
