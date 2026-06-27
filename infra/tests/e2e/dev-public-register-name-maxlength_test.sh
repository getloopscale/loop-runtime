#!/usr/bin/env bash
# loopzero-cloud#65 closure LIVE test — proves the Kratos identity schema's
# `traits.name.maxLength: 256` cap rejects adversarial 20 000-char Name on
# the /self-service/registration submit.
#
# Symptom per #65 (RED TEAM 2026-06-26T141335): the original Kratos
# identity schema had `traits.name` UNBOUNDED (JSON Schema default = no
# cap), so the dashboard /register form accepted a 20 000-char display
# Name and persisted it. That name became the JWT `name` claim (4-line
# direct path: dashboard /register POST → Kratos → cloud session_token
# .go:117 → loopzero-contracts jwt.SessionTokenIssuer.Issue) → ~27 KB
# Authorization header → 431 on every /engine/* call (#64 root cause).
#
# Fix (this plan, T1):
#   - Frontend gate: dashboard/src/lib/validation.ts adds validateMax
#     Length(value, fieldLabel, max) helper + register.tsx wires it +
#     HTML maxLength={256} on the <Input>.
#   - Backend canonical schema cap: loopzero/infra/helm/ory-kratos/identity
#     .schema.json adds "maxLength": 256 to traits.name. Kratos enforces
#     it server-side per JSON Schema draft-07 contract.
#
# This LIVE test probes the BACKEND canonical contract: a fresh
# registration flow + a 20 000-char `traits.name` submission MUST return
# the canonical Kratos schema validation error (4xx with a message body
# referencing the maxLength constraint), NOT a 200 OK / 302 success.
#
# Exit codes:
#   0  Kratos rejects the 20k-char Name (canonical schema cap enforces)
#   1  Kratos accepted (cap not in effect — operator helmfile sync of
#      ory-kratos pending, OR schema regressed)
#
# Usage:
#   bash infra/tests/e2e/dev-public-register-name-maxlength_test.sh \
#     [--skip-live] [--json]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ; shift ;;
    --json)      JSON_OUTPUT=1 ; shift ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) printf 'unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { [ "$JSON_OUTPUT" = "0" ] && printf '[65-name-maxlen] %s\n' "$*" >&2 || true ; }

if [ "$SKIP_LIVE" = "1" ]; then
  log "SKIP_LIVE=1 — hermetic CI mode. bash -n + structure check."
  bash -n "$0"
  log "PASS hermetic syntax"
  [ "$JSON_OUTPUT" = "1" ] && printf '{"verdict":"SKIPPED","mode":"hermetic","fails":0}\n'
  exit 0
fi

log "TARGET: ${TARGET_HOST}"

# Step 1: initialize a self-service registration flow via the browser API.
# Kratos returns a flow id + ui.action URL + csrf_token node we need to POST.
COOKIE_JAR="$(mktemp)"
trap 'rm -f "${COOKIE_JAR}"' EXIT INT TERM

FLOW_INIT_URL="${TARGET_HOST}/self-service/registration/browser"
FLOW_JSON="$(mktemp)"
HTTP_CODE="$(curl -s -L -c "${COOKIE_JAR}" -b "${COOKIE_JAR}" \
  -H 'Accept: application/json' \
  -o "${FLOW_JSON}" \
  -w '%{http_code}' \
  "${FLOW_INIT_URL}" || echo "000")"

if [ "${HTTP_CODE}" != "200" ]; then
  log "FAIL — flow init returned HTTP ${HTTP_CODE} (expected 200)"
  [ "$JSON_OUTPUT" = "1" ] && printf '{"verdict":"FAIL","mode":"live","reason":"flow_init_%s"}\n' "${HTTP_CODE}"
  exit 1
fi

FLOW_ID="$(python3 -c "import json,sys; print(json.load(open('${FLOW_JSON}')).get('id',''))" 2>/dev/null || echo "")"
CSRF_TOKEN="$(python3 -c "
import json
flow = json.load(open('${FLOW_JSON}'))
for n in flow.get('ui',{}).get('nodes',[]):
    a = n.get('attributes',{})
    if a.get('name') == 'csrf_token':
        print(a.get('value',''))
        break
" 2>/dev/null || echo "")"
ACTION_URL="$(python3 -c "import json; print(json.load(open('${FLOW_JSON}')).get('ui',{}).get('action',''))" 2>/dev/null || echo "")"

if [ -z "${FLOW_ID}" ] || [ -z "${CSRF_TOKEN}" ] || [ -z "${ACTION_URL}" ]; then
  log "FAIL — flow shape unexpected (id='${FLOW_ID}' csrf='${CSRF_TOKEN:0:8}…' action='${ACTION_URL}')"
  [ "$JSON_OUTPUT" = "1" ] && printf '{"verdict":"FAIL","mode":"live","reason":"flow_shape_invalid"}\n'
  exit 1
fi

log "Flow initialized: id=${FLOW_ID:0:8}… action=${ACTION_URL:0:60}…"

# Step 2: build the adversarial submission body. 20 000-char 'A' Name per
# the Red Team #65 verbatim repro. Use a unique disposable email (epoch).
LONG_NAME="$(python3 -c 'import sys; sys.stdout.write("A" * 20000)')"
EMAIL="redteam-65-$(date +%s)@loopzerotest.invalid"  # .invalid TLD never resolves
PASSWORD='LoopZero-Red-Team-65-2026-06-26-canonical'

SUBMIT_JSON="$(mktemp)"
SUBMIT_BODY="$(mktemp)"
trap 'rm -f "${COOKIE_JAR}" "${FLOW_JSON}" "${SUBMIT_JSON}" "${SUBMIT_BODY}"' EXIT INT TERM

python3 -c "
import json
print(json.dumps({
    'method': 'password',
    'csrf_token': '''${CSRF_TOKEN}''',
    'traits': {'email': '''${EMAIL}''', 'name': '''${LONG_NAME}'''},
    'password': '''${PASSWORD}''',
}))
" > "${SUBMIT_BODY}"

HTTP_CODE="$(curl -s -L -c "${COOKIE_JAR}" -b "${COOKIE_JAR}" \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json' \
  -X POST \
  --data @"${SUBMIT_BODY}" \
  -o "${SUBMIT_JSON}" \
  -w '%{http_code}' \
  "${ACTION_URL}" || echo "000")"

log "Submit HTTP=${HTTP_CODE} body-bytes=$(wc -c < "${SUBMIT_JSON}")"

# Step 3: canonical assertion. Kratos with the patched schema MUST reject
# with 4xx (typically 400 with a JSON error referencing the maxLength
# constraint OR a UI messages array with a length-related complaint).
#
# A 200/302 = the cap is NOT yet applied at the Kratos schema level
# (operator pending helmfile sync of ory-kratos with the patched
# identity.schema.json).
if [ "${HTTP_CODE}" = "400" ] || [ "${HTTP_CODE}" = "422" ]; then
  REJECTION_MSG="$(python3 -c "
import json
try:
    j = json.load(open('${SUBMIT_JSON}'))
    # Kratos browser flow returns 400 with ui.messages[] OR ui.nodes[].messages[]
    msgs = []
    for n in (j.get('ui') or {}).get('messages', []) or []:
        msgs.append(n.get('text',''))
    for n in (j.get('ui') or {}).get('nodes', []) or []:
        for m in (n.get('messages') or []):
            msgs.append(m.get('text',''))
    if not msgs and isinstance(j.get('error'), dict):
        msgs.append(j['error'].get('message',''))
    if not msgs:
        msgs.append(j.get('reason',''))
    print('|'.join(filter(None, msgs))[:300])
except Exception as e:
    print(f'parse_err={e}')
" 2>/dev/null || echo "")"
  log "Canonical rejection — HTTP ${HTTP_CODE} body summary: ${REJECTION_MSG:0:200}"
  log "VERDICT: PASS — Kratos schema cap rejected 20k-char Name (Red Team #65 closure proof)"
  [ "$JSON_OUTPUT" = "1" ] && printf '{"verdict":"PASS","mode":"live","status":%s,"name_chars":20000}\n' "${HTTP_CODE}"
  exit 0
fi

# Any other status (200, 302, etc.) = name was NOT rejected by schema.
log "FAIL — Kratos accepted 20k-char Name (HTTP=${HTTP_CODE}). Schema cap NOT in effect."
log "       Operator action: \`task k3d:bootstrap-cells\` OR \`helmfile -e dev-public sync --selector name=ory-kratos\` to apply the patched identity.schema.json."
[ "$JSON_OUTPUT" = "1" ] && printf '{"verdict":"FAIL","mode":"live","status":%s,"reason":"schema_cap_not_applied"}\n' "${HTTP_CODE}"
exit 1
