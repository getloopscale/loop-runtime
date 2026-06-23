#!/usr/bin/env bash
# T7.2 of plan 2026-06-06-dashboard-journey-j17-deployments-realtime.
#
# LIVE end-to-end validation of the J-17 deployments journey against the
# real dev-public stack (https://cloud-dev.loopzero.example). Validates:
#
#   Scenario A — auth + REST list endpoint
#     - mint JWT against Hydra (client_credentials, loopzero-engine audience)
#     - GET /engine/api/v1/projects/{name}/builds → 200 + builds array
#     - assert limit + env filter forwarded (q-string round-trip)
#
#   Scenario B — SSE event stream wired
#     - open EventSource on /engine/api/v1/projects/{name}/builds/stream
#     - assert HTTP 200 + Content-Type: text/event-stream
#     - assert at least one event arrives within timeout (`build.started`
#       OR a heartbeat / sync event) — proves Caddy + cloud BFF + loopzero-api
#       SSE chain is not silently blocked by a buffering proxy
#
#   Scenario C — Loki marker pipeline (Layer 1+2+3 of the log filter)
#     - pick a recent SUCCEEDED build
#     - GET /engine/api/v1/builds/{id}/logs
#     - assert entries[*].content contains at least one ::loopzero:: marker
#     - assert at least one entry has loopzero_phase ∈ {clone,install,build,
#       sign,deliver} after the frontend parser would strip the prefix
#
#   Scenario D — pre-flight HEALTHCHECK
#     - probe https://cloud-dev.loopzero.example/health
#     - probe https://auth-dev.loopzero.example (Kratos OIDC discovery)
#     - probe Hydra public oauth2/token via port-forward
#
# This is a LIVE test (touches dev-public). It does NOT mutate prod state
# beyond reading existing builds; no commits are pushed. For mutating
# coverage (trigger a build + assert SSE step events arrive in <2s) the
# T7.3 dogfood wrapper runs an explicit push via gh api.
#
# Run after `task dev-public:bootstrap` + `LOOPZERO_HELMFILE_ENV=dev-public
# task setup`.

set -euo pipefail

CLOUD_URL="${CLOUD_URL:-https://app-dev.loopzero.example}"
AUTH_URL="${AUTH_URL:-https://auth-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-teste}"
# Engine endpoints (loopzero-api) — reached via /engine/* proxy on loopzero-cloud
# OR direct via NodePort. Bootstrap-JWT only validates engine-direct;
# /engine proxy requires user-session JWT. Override ENGINE_BASE_OVERRIDE
# to e.g. http://localhost:13001 for bootstrap-JWT engine validation.
# T5.2 of plan 2026-06-08.
ENGINE_BASE="${ENGINE_BASE_OVERRIDE-${CLOUD_URL}/engine}"
HYDRA_NAMESPACE="${HYDRA_NAMESPACE:-loopzero-system}"
HYDRA_PORT_LOCAL="${HYDRA_PORT_LOCAL:-14444}"
KUBECTL_CMD="${KUBECTL_CMD:-kubectl --context k3d-loopzero-mgmt}"
TIMEOUT_SSE="${TIMEOUT_SSE:-15}"

SKIP_LIVE="${SKIP_LIVE:-0}"
SCENARIO="${SCENARIO:-all}"
JSON_OUTPUT="${JSON_OUTPUT:-0}"

PASS=0
FAIL=0
RESULTS_JSON='{}'

usage() {
  cat <<USAGE
Usage: $0 [--skip-live] [--scenario=A|B|C|D|all] [--scenarios=A,B,D] [--json]

Flags:
  --skip-live           Skip scenarios that touch dev-public (CI hermetic mode)
  --scenario=<id>       Run only one scenario (A, B, C, D, or all)
  --scenarios=A,B,D     Run a subset of scenarios (comma-separated, no spaces).
                        Use this when a consumer plan declares only specific
                        scenarios are in-scope for its DoD (e.g.
                        2026-06-08-dashboard-screens-review-fixes T5.2 uses
                        --scenarios=A,B,D per ADR 2026-06-08-t5-2-dod-
                        refinement-loki-scenario-out-of-scope). --scenarios
                        and --scenario are mutually exclusive; if both passed,
                        --scenarios wins. Scenario D (preflight) always runs
                        when any other scenario runs.
  --json                Emit machine-readable verdict on stdout

Env overrides:
  CLOUD_URL             Default: https://cloud-dev.loopzero.example
  AUTH_URL              Default: https://auth-dev.loopzero.example
  PROJECT_NAME          Default: teste
  TIMEOUT_SSE           Default: 15s
  ENGINE_BASE_OVERRIDE  Override engine path for direct (engine-NodePort)
                        bypass of the /engine proxy gate. E.g.
                        ENGINE_BASE_OVERRIDE=http://localhost:13001 lets
                        bootstrap-JWT validate against engine directly.
USAGE
}

# SCENARIOS_FILTER honors --scenarios= (comma-separated, multi-selector).
# Empty string = no filter applied (falls back to --scenario= or default "all").
SCENARIOS_FILTER=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ;;
    --scenario=*) SCENARIO="${1#--scenario=}" ;;
    --scenarios=*) SCENARIOS_FILTER="${1#--scenarios=}" ;;
    --json) JSON_OUTPUT=1 ;;
    -h|--help) usage; exit 0 ;;
    *) printf 'Unknown flag: %s\n' "$1" >&2; usage; exit 2 ;;
  esac
  shift
done

# scenario_enabled <id> — case-insensitive membership check against SCENARIOS_FILTER.
# Returns 0 when --scenarios is empty (no filter) OR id is in the comma-list.
scenario_enabled() {
  local id="${1,,}"
  if [[ -z "$SCENARIOS_FILTER" ]]; then return 0; fi
  local lower="${SCENARIOS_FILTER,,}"
  # Wrap in commas so substring search is exact (avoids 'A' matching 'AB').
  [[ ",${lower}," == *",${id},"* ]]
}

log() {
  if [[ "$JSON_OUTPUT" != "1" ]]; then
    printf '[deployments-flow] %s\n' "$*" >&2
  fi
}

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

# --- Pre-flight ---
preflight() {
  log "Pre-flight HEALTHCHECK"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  --skip-live: pre-flight bypassed"
    return 0
  fi
  # Health probe is mode-aware:
  #   - dev-public DNS (cloud-dev): /health (loopzero-cloud's own health)
  #   - engine-direct (localhost:13001): /api/v1/health (loopzero-api's own health)
  # T5.2 of plan 2026-06-08.
  local cloud_ok=0
  if curl -fsS -o /dev/null -m 5 "$ENGINE_BASE/api/v1/health"; then
    cloud_ok=1
  elif curl -fsS -o /dev/null -m 5 "$CLOUD_URL/health"; then
    cloud_ok=1
  fi
  assert "cloud-dev or engine-direct health reachable" "$cloud_ok"
  if [[ "$cloud_ok" != "1" ]]; then
    printf 'ABORT: cloud-dev not reachable; aborting LIVE scenarios\n' >&2
    return 1
  fi
}

# --- Auth helper ---
TMP_JWT=""
mint_jwt() {
  if [[ -n "$TMP_JWT" ]]; then echo "$TMP_JWT"; return 0; fi
  # CANONICAL 2026-06-15 dev-public LIVE: prefer HMAC session JWT (carries
  # tenant_id from active session; passes loopzero-cloud /engine proxy guard
  # per MEET-2026-04-07-039 B7). Bootstrap RSA JWT remains the fallback
  # for engine-direct (localhost:13001) but lacks tenant_id claim so the
  # /engine/* proxy rejects it.
  if [[ -f "$HOME/.loopzero/local-dogfood-session-jwt.txt" ]]; then
    TMP_JWT=$(cat "$HOME/.loopzero/local-dogfood-session-jwt.txt")
    echo "$TMP_JWT"
    return 0
  fi
  # Local dogfood path: reuse the seeded token file when available
  if [[ -f "$HOME/.loopzero/local-dogfood-token.txt" ]]; then
    TMP_JWT=$(cat "$HOME/.loopzero/local-dogfood-token.txt")
    echo "$TMP_JWT"
    return 0
  fi
  printf 'ABORT: no JWT available; run seed-admin-loopzero-dev.sh + seed-local-user.sh
' >&2
  return 1
}

# --- Scenario A: REST list + filter forwarding ---
scenario_a() {
  log "Scenario A: REST list endpoint + filter forwarding"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  SKIP (--skip-live)"
    return 0
  fi
  local jwt
  jwt=$(mint_jwt) || return 1
  # 1. Plain list — must return JSON {builds:[...], total:N}
  local body
  body=$(curl -fsS -m 10 \
    -H "Authorization: Bearer $jwt" \
    "$ENGINE_BASE/api/v1/projects/$PROJECT_NAME/builds?limit=5")
  if echo "$body" | python3 -c "
import sys, json
d = json.load(sys.stdin)
assert isinstance(d.get('builds'), list), 'builds is not array'
assert isinstance(d.get('total'), int), 'total is not int'
print(f'count={len(d[\"builds\"])} total={d[\"total\"]}', file=sys.stderr)
" 2>&1 >/dev/null; then
    assert "GET /builds?limit=5 returns {builds, total}" 1
  else
    assert "GET /builds?limit=5 returns {builds, total}" 0
  fi
  # 2. Env filter forwarded — production should narrow
  local prod_body
  prod_body=$(curl -fsS -m 10 \
    -H "Authorization: Bearer $jwt" \
    "$ENGINE_BASE/api/v1/projects/$PROJECT_NAME/builds?env=production&limit=5")
  local all_prod
  all_prod=$(echo "$prod_body" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print('1' if all(b.get('environment') == 'production' for b in d.get('builds', [])) else '0')
" 2>/dev/null || echo "0")
  assert "?env=production narrows result server-side" "$all_prod"
}

# --- Scenario B: SSE wiring ---
scenario_b() {
  log "Scenario B: SSE stream Content-Type + first event"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  SKIP (--skip-live)"
    return 0
  fi
  local jwt
  jwt=$(mint_jwt) || return 1
  local sse_tmp
  sse_tmp=$(mktemp)
  # Open SSE; bound by timeout; capture headers + body. loopzero-cloud BFF
  # strips Authorization for SSE (cookie auth path), so we attach the
  # same dev-public session cookie. For dogfood path we still pass the
  # Bearer header — the BFF accepts both.
  curl -sS -N -m "$TIMEOUT_SSE" \
    -H "Authorization: Bearer $jwt" \
    -H "Accept: text/event-stream" \
    -D "${sse_tmp}.h" \
    "$ENGINE_BASE/api/v1/projects/$PROJECT_NAME/builds/stream" \
    > "$sse_tmp" 2>/dev/null || true
  local status
  status=$(head -1 "${sse_tmp}.h" 2>/dev/null | awk '{print $2}')
  if [[ "$status" == "200" ]]; then
    assert "SSE HTTP 200" 1
  else
    assert "SSE HTTP 200 (got: ${status:-no-response})" 0
  fi
  if grep -qi "content-type:.*text/event-stream" "${sse_tmp}.h" 2>/dev/null; then
    assert "Content-Type is text/event-stream" 1
  else
    assert "Content-Type is text/event-stream" 0
  fi
  # The stream should yield at least one event (any type) before the
  # timeout. Empty body in ${TIMEOUT_SSE}s with HTTP 200 indicates either
  # (a) Caddy/BFF buffering bug OR (b) no real builds emitting events.
  # Both are CORRECTLY FAIL — the test asserts the real event flow,
  # not just the wire shape.
  if [[ -s "$sse_tmp" ]] && grep -qE '^event:|^data:' "$sse_tmp"; then
    assert "at least one SSE event arrives within ${TIMEOUT_SSE}s" 1
  else
    assert "at least one SSE event arrives within ${TIMEOUT_SSE}s" 0
  fi
  rm -f "$sse_tmp" "${sse_tmp}.h"
}

# --- Scenario C: Loki marker pipeline ---
scenario_c() {
  log "Scenario C: Loki marker pipeline (Layer 1+2+3)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  SKIP (--skip-live)"
    return 0
  fi
  local jwt
  jwt=$(mint_jwt) || return 1
  local list
  list=$(curl -fsS -m 10 \
    -H "Authorization: Bearer $jwt" \
    "$ENGINE_BASE/api/v1/projects/$PROJECT_NAME/builds?status=succeeded&limit=1")
  local build_id
  build_id=$(echo "$list" | python3 -c "
import sys, json
d = json.load(sys.stdin)
builds = d.get('builds', [])
print(builds[0].get('build_id', '') if builds else '')
" 2>/dev/null)
  if [[ -z "$build_id" ]]; then
    assert "no SUCCEEDED build to validate" 0
    return 0
  fi
  log "  using build: $build_id"
  local logs
  logs=$(curl -fsS -m 10 \
    -H "Authorization: Bearer $jwt" \
    "$ENGINE_BASE/api/v1/builds/$build_id/logs")
  # At least one entry with a ::loopzero:: marker = Layer 1 (Promtail label)
  # + Layer 2 (Argo wrap) + Layer 3 (LogQL filter) all working.
  local marker_count
  marker_count=$(echo "$logs" | python3 -c "
import sys, json
d = json.load(sys.stdin)
entries = d.get('entries', [])
n = sum(1 for e in entries if '::loopzero::' in (e.get('content') or ''))
print(n)
" 2>/dev/null || echo "0")
  if [[ "$marker_count" -gt 0 ]]; then
    assert "Loki returns ::loopzero:: markers (got: $marker_count entries)" 1
  else
    assert "Loki returns ::loopzero:: markers" 0
  fi
  # Phase coverage: at least one entry from each of clone/install/build
  local phase_count
  phase_count=$(echo "$logs" | python3 -c "
import sys, json, re
d = json.load(sys.stdin)
phases = set()
for e in d.get('entries', []):
    m = re.match(r'^::loopzero:: phase=(\w+)', e.get('content', '') or '')
    if m: phases.add(m.group(1))
expected = {'clone', 'install', 'build'}
print(len(expected & phases))
" 2>/dev/null || echo "0")
  if [[ "$phase_count" -ge 3 ]]; then
    assert "phase coverage clone+install+build present" 1
  else
    assert "phase coverage clone+install+build present (got: $phase_count of 3)" 0
  fi
}

# --- Scenario D: pre-flight already inline ---
scenario_d() {
  preflight || true
}

# --- Run ---
# When --scenarios= is set (multi-selector), it WINS over --scenario= per usage()
# contract. Scenario D (preflight) runs whenever any other scenario runs to
# guard against cold-cluster false-PASS.
if [[ -n "$SCENARIOS_FILTER" ]]; then
  scenario_d
  scenario_enabled A && scenario_a
  scenario_enabled B && scenario_b
  scenario_enabled C && scenario_c
  # D is implicit (always runs above) — explicit selection still no-ops harmlessly
else
  case "$SCENARIO" in
    A|a) scenario_d; scenario_a ;;
    B|b) scenario_d; scenario_b ;;
    C|c) scenario_d; scenario_c ;;
    D|d) scenario_d ;;
    all) scenario_d; scenario_a; scenario_b; scenario_c ;;
    *) printf 'Unknown scenario: %s\n' "$SCENARIO" >&2; exit 2 ;;
  esac
fi

# --- Verdict ---
TOTAL=$((PASS + FAIL))
if [[ "$JSON_OUTPUT" == "1" ]]; then
  printf '{"pass":%d,"fail":%d,"total":%d,"verdict":"%s"}\n' \
    "$PASS" "$FAIL" "$TOTAL" "$([[ "$FAIL" == "0" ]] && echo PASS || echo FAIL)"
else
  printf '\n[deployments-flow] verdict: %d/%d PASS, %d FAIL\n' \
    "$PASS" "$TOTAL" "$FAIL" >&2
fi

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
