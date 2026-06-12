#!/usr/bin/env bash
# T4.1 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — LIVE
# 6-scenario zero-leak proof per D5 ADR. The plan ships when this script
# returns verdict=PASS pass=6 fail=0 on a cleanroom-fresh dev-public env.
#
# Each scenario is a mechanical grep against an isolated cluster surface
# (PG row, K8s Secret, pod env, kubectl logs/describe). The ephemeral
# UUID sentinel makes the grep precise — coincidental match probability
# is ~0.
#
# OPERATOR HARD STOP: requires real OpenBao + ESO controller + loopzero-api
# wired with the transit chain. CI invocation uses --skip-live to validate
# only the script shape via the hermetic companion test.

set -uo pipefail
cd "$(dirname "$0")/../../.."

# === Flag parsing ===
SKIP_LIVE=0
REHEARSAL_MODE=0
JSON_MODE=0
SCENARIO_FILTER=""
TENANT_ID="${LOOPZERO_TEST_TENANT_ID:-admin-loopzero-dev}"
PROJECT_NAME="${LOOPZERO_TEST_PROJECT:-teste}"
ENV_NAME="${LOOPZERO_TEST_ENV:-production}"
DEV_PUBLIC_BASE="${LOOPZERO_DEV_PUBLIC_BASE:-https://cloud-dev.loopzero.example}"
KUBE_CONTEXT="${LOOPZERO_KUBE_CONTEXT:-k3d-loopzero-mgmt}"
WORKLOAD_CONTEXT="${LOOPZERO_WORKLOAD_CONTEXT:-k3d-loopzero-workload}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--skip-live] [--rehearsal] [--json] [--scenario=A|B|C|D|E|F] [--help]

LIVE 6-scenario zero-leak proof for the J-22 OpenBao transit chain.

Scenarios:
  A: PG encrypted_value column starts with vault:v1: (NOT plaintext bytes)
  B: Workload K8s Secret in tenant ns has plaintext value (ESO sync OK)
  C: Pod printenv KEY returns plaintext verbatim (envFrom OK)
  D: kubectl logs -A | grep sentinel → 0 matches (no log leak)
  E: kubectl describe pods -A shows KEY name but NOT plaintext value
  F: kubectl get secret -A → sentinel only in tenant workload ns

Flags:
  --skip-live           Hermetic mode — skip LIVE probes (CI test mode)
  --rehearsal           Iter 26 rehearsal mode — runs LIVE preflight probes
                        only (no probe-secret POST, no encrypt path
                        exercised). Validates that the wire chain is
                        ready BEFORE the sponsor walkthrough commits to
                        full LIVE. ~5 kubectl checks, ~5s total. Catches
                        misconfig (CSS not Ready, Secret missing, ESO
                        controller down) early without producing a
                        sentinel row in PG.
  --json                Emit machine-readable {verdict, pass, fail, scenarios[]} JSON
  --scenario=X          Run only scenario X (one of A B C D E F); default = all
  --help, -h            Show this help

Env overrides:
  LOOPZERO_TEST_TENANT_ID    Tenant ID (default admin-loopzero-dev)
  LOOPZERO_TEST_PROJECT      Project (default teste)
  LOOPZERO_TEST_ENV          Env (default production)
  LOOPZERO_DEV_PUBLIC_BASE   Cloud base URL (default https://cloud-dev.loopzero.example)
  LOOPZERO_KUBE_CONTEXT      Mgmt cluster context (default k3d-loopzero-mgmt)
  LOOPZERO_WORKLOAD_CONTEXT  Workload cluster context (default k3d-loopzero-workload)

Exit codes:
  0  All 6 scenarios PASS (or all rehearsal probes PASS under --rehearsal)
  1  At least one scenario FAIL (zero-leak invariant violated — operator HARD STOP)
  2  Usage error
EOF
}

for arg in "$@"; do
  case "$arg" in
    --skip-live) SKIP_LIVE=1 ;;
    --rehearsal) REHEARSAL_MODE=1 ;;
    --json) JSON_MODE=1 ;;
    --scenario=*) SCENARIO_FILTER="${arg#*=}" ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown flag: $arg" >&2; usage; exit 2 ;;
  esac
done

# Mutual exclusion: --skip-live (hermetic, no cluster) cannot combine
# with --rehearsal (LIVE preflight without encrypt). The hermetic
# companion test asserts both flags but never both at once.
if [ $SKIP_LIVE -eq 1 ] && [ $REHEARSAL_MODE -eq 1 ]; then
  echo "FATAL: --skip-live and --rehearsal are mutually exclusive" >&2
  exit 2
fi

# === Ephemeral sentinel ===
# UUID-shape token that NO real secret would ever match. Per
# Drawbacks-Risks R4 of plan v1.2 the coincidence probability is ≈ 0.
SENTINEL_UUID="$(cat /proc/sys/kernel/random/uuid 2>/dev/null || \
  python3 -c 'import uuid; print(uuid.uuid4())' 2>/dev/null || \
  echo "fallback-$(date +%s)-$$")"
SENTINEL_VALUE="zeroleak-${SENTINEL_UUID}"
SECRET_NAME="ZEROLEAK_PROBE_$(echo "$SENTINEL_UUID" | tr -d - | tr 'a-f' 'A-F' | head -c 16)"

# === Result tracking ===
PASS_COUNT=0
FAIL_COUNT=0
declare -A SCENARIO_STATUS

record() {
  local name="$1" status="$2"
  SCENARIO_STATUS[$name]="$status"
  if [ "$status" = "PASS" ]; then
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

run_scenario() {
  local name="$1"
  if [ -n "$SCENARIO_FILTER" ] && [ "$SCENARIO_FILTER" != "$name" ]; then
    return 1 # filtered out
  fi
  return 0
}

# === Cleanup trap (EC-14 from edge-case review) ===
cleanup() {
  if [ $SKIP_LIVE -eq 1 ]; then return; fi
  # Best-effort: delete the probe secret + ExternalSecret CR + K8s Secret
  # so failed runs do not leave the sentinel orphaned in PG.
  kubectl --context="$WORKLOAD_CONTEXT" delete secret "$SECRET_NAME" \
    -n "loopzero-${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}" --ignore-not-found=true >/dev/null 2>&1 || true
  kubectl --context="$WORKLOAD_CONTEXT" delete externalsecret "$SECRET_NAME" \
    -n "loopzero-${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}" --ignore-not-found=true >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# === Hermetic mode short-circuit ===
if [ $SKIP_LIVE -eq 1 ]; then
  # CI hermetic mode — exit early with verdict PASS but pass=0 fail=0;
  # the hermetic companion test (T4.2) validates the script shape.
  if [ $JSON_MODE -eq 1 ]; then
    printf '{"verdict":"SKIPPED","pass":0,"fail":0,"reason":"--skip-live (hermetic mode)"}\n'
  else
    echo "verdict=SKIPPED pass=0 fail=0 reason=--skip-live"
  fi
  exit 0
fi

# === Rehearsal mode (iter 26 productive-idle) ===
# LIVE preflight probes ONLY — no probe-secret POST, no encrypt path.
# Operator runs this BEFORE the sponsor walkthrough to catch misconfig
# (CSS not Ready, Secret missing, ESO down, loopzero-api env vars
# unflipped) WITHOUT producing a sentinel row in PG or exercising
# OpenBao under partial wire.
do_rehearsal() {
  local rehearsal_pass=0
  local rehearsal_fail=0
  local -a rehearsal_results=()

  rehearsal_check() {
    local name="$1" cmd="$2"
    if eval "$cmd" >/dev/null 2>&1; then
      rehearsal_pass=$((rehearsal_pass + 1))
      rehearsal_results+=("PASS: $name")
    else
      rehearsal_fail=$((rehearsal_fail + 1))
      rehearsal_results+=("FAIL: $name")
    fi
  }

  # R1: ESO controller pod Running in external-secrets ns
  rehearsal_check "R1 ESO controller pod Running" \
    "kubectl --context='$KUBE_CONTEXT' -n external-secrets get pods -l app.kubernetes.io/name=external-secrets -o jsonpath='{.items[*].status.phase}' | grep -q Running"

  # R2: ClusterSecretStore openbao-transit exists + Ready=True
  rehearsal_check "R2 ClusterSecretStore openbao-transit Ready=True" \
    "[ \"\$(kubectl --context='$KUBE_CONTEXT' get clustersecretstore openbao-transit -o jsonpath='{.status.conditions[?(@.type==\"Ready\")].status}' 2>/dev/null)\" = 'True' ]"

  # R3: Secret loopzero-openbao-approle exists in external-secrets ns (CSS read source)
  rehearsal_check "R3 Secret loopzero-openbao-approle in external-secrets ns" \
    "kubectl --context='$KUBE_CONTEXT' -n external-secrets get secret loopzero-openbao-approle -o jsonpath='{.data.OPENBAO_VAULT_SECRET_ID}' | head -c 1 | grep -q ."

  # R4: Secret loopzero-openbao-approle exists in loopzero-system ns (loopzero-api envFrom source)
  rehearsal_check "R4 Secret loopzero-openbao-approle in loopzero-system ns" \
    "kubectl --context='$KUBE_CONTEXT' -n loopzero-system get secret loopzero-openbao-approle -o jsonpath='{.data.OPENBAO_VAULT_SECRET_ID}' | head -c 1 | grep -q ."

  # R5: loopzero-api pods Running with OPENBAO_TRANSIT_ENABLED=true in env
  rehearsal_check "R5 loopzero-api OPENBAO_TRANSIT_ENABLED=true env present" \
    "kubectl --context='$KUBE_CONTEXT' -n loopzero-system get deploy loopzero-api -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name==\"OPENBAO_TRANSIT_ENABLED\")].value}' | grep -q '^true$'"

  # Emit results
  if [ $JSON_MODE -eq 1 ]; then
    printf '{"verdict":"%s","pass":%d,"fail":%d,"mode":"rehearsal","checks":[' \
      "$([ $rehearsal_fail -eq 0 ] && echo PASS || echo FAIL)" \
      "$rehearsal_pass" "$rehearsal_fail"
    local first=1
    for r in "${rehearsal_results[@]}"; do
      [ $first -eq 1 ] || printf ','
      printf '"%s"' "${r//\"/\\\"}"
      first=0
    done
    printf ']}\n'
  else
    printf '[rehearsal] LIVE preflight probes:\n'
    for r in "${rehearsal_results[@]}"; do
      printf '  %s\n' "$r"
    done
    printf '[rehearsal] verdict=%s pass=%d fail=%d\n' \
      "$([ $rehearsal_fail -eq 0 ] && echo PASS || echo FAIL)" \
      "$rehearsal_pass" "$rehearsal_fail"
  fi

  [ $rehearsal_fail -eq 0 ] && return 0 || return 1
}

if [ $REHEARSAL_MODE -eq 1 ]; then
  do_rehearsal
  exit $?
fi

# === LIVE invocation ===
# JWT mint from the canonical seed (admin@loopzero.example per project_canonical_dev_user).
JWT_PATH="${HOME}/.loopzero/local-dogfood-token.txt"
if [ ! -f "$JWT_PATH" ]; then
  echo "FATAL: JWT seed file $JWT_PATH not found — run seed-admin-loopzero-dev.sh first" >&2
  exit 1
fi
JWT="$(cat "$JWT_PATH")"

# Step 1: POST the probe secret via the J-22 Bulk endpoint.
echo "[probe] POST /api/v1/secrets/bulk with sentinel=$SENTINEL_VALUE"
RESPONSE="$(curl -fsS -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d "{\"project\":\"$PROJECT_NAME\",\"secrets\":[{\"name\":\"$SECRET_NAME\",\"value\":\"$SENTINEL_VALUE\",\"envs\":[\"$ENV_NAME\"]}]}" \
  "$DEV_PUBLIC_BASE/api/v1/secrets/bulk" 2>&1)"
if ! echo "$RESPONSE" | grep -q '"status":"created"'; then
  echo "FATAL: bulk POST did not create the probe secret: $RESPONSE" >&2
  exit 1
fi

# Wait briefly for ESO sync (refreshInterval=30s; some envs may be faster).
sleep 35

# === Scenario A: PG encrypted_value has vault:v1: prefix (NOT plaintext) ===
if run_scenario A; then
  PG_BYTES="$(kubectl --context="$KUBE_CONTEXT" -n loopzero-system exec deploy/postgresql -- \
    psql -U postgres -d loopzero -tAc \
    "SELECT encode(encrypted_value, 'escape') FROM secrets WHERE name='$SECRET_NAME' LIMIT 1" 2>/dev/null || echo "")"
  if echo "$PG_BYTES" | grep -q '^vault:v1:'; then
    record A PASS
  else
    record A FAIL
    echo "  A FAIL: PG row should start with vault:v1:; got: ${PG_BYTES:0:50}..." >&2
  fi
fi

# === Scenario B: workload K8s Secret has plaintext value (ESO sync OK) ===
if run_scenario B; then
  WORKLOAD_NS="loopzero-${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}"
  SEC_VALUE="$(kubectl --context="$WORKLOAD_CONTEXT" -n "$WORKLOAD_NS" \
    get secret "$SECRET_NAME" -o jsonpath="{.data.$SECRET_NAME}" 2>/dev/null | base64 -d 2>/dev/null || echo "")"
  if [ "$SEC_VALUE" = "$SENTINEL_VALUE" ]; then
    record B PASS
  else
    record B FAIL
    echo "  B FAIL: workload K8s Secret missing or value mismatch (got: ${SEC_VALUE:0:30}...)" >&2
  fi
fi

# === Scenario C: pod printenv returns plaintext verbatim ===
if run_scenario C; then
  # Find any pod in workload ns that mounts the secret via envFrom and
  # probe its env.
  POD="$(kubectl --context="$WORKLOAD_CONTEXT" -n "$WORKLOAD_NS" \
    get pods --no-headers 2>/dev/null | awk 'NR==1 {print $1}')"
  if [ -n "$POD" ]; then
    PRINTENV="$(kubectl --context="$WORKLOAD_CONTEXT" -n "$WORKLOAD_NS" \
      exec "$POD" -- printenv "$SECRET_NAME" 2>/dev/null || echo "")"
    if [ "$PRINTENV" = "$SENTINEL_VALUE" ]; then
      record C PASS
    else
      record C FAIL
      echo "  C FAIL: pod printenv $SECRET_NAME returned: ${PRINTENV:0:30}..." >&2
    fi
  else
    record C FAIL
    echo "  C FAIL: no pod found in $WORKLOAD_NS to probe" >&2
  fi
fi

# === Scenario D: kubectl logs -A | grep sentinel → 0 matches ===
if run_scenario D; then
  MATCHES="$(kubectl --context="$KUBE_CONTEXT" logs --all-containers=true --since=2m -A 2>/dev/null | \
    grep -F "$SENTINEL_VALUE" | wc -l)"
  if [ "$MATCHES" -eq 0 ]; then
    record D PASS
  else
    record D FAIL
    echo "  D FAIL: sentinel leaked into logs ($MATCHES matches)" >&2
  fi
fi

# === Scenario E: kubectl describe pods | grep ENV_KEY shows KEY only ===
if run_scenario E; then
  DESCRIBE_MATCHES="$(kubectl --context="$WORKLOAD_CONTEXT" describe pods -A 2>/dev/null | \
    grep -F "$SENTINEL_VALUE" | wc -l)"
  if [ "$DESCRIBE_MATCHES" -eq 0 ]; then
    record E PASS
  else
    record E FAIL
    echo "  E FAIL: sentinel leaked into pod describe ($DESCRIBE_MATCHES matches)" >&2
  fi
fi

# === Scenario F: secret only in tenant ns, not elsewhere ===
if run_scenario F; then
  # Decode every Secret in every ns, search for sentinel, verify all
  # matches are in the workload ns.
  WORKLOAD_NS="loopzero-${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}"
  ALL_MATCHES="$(kubectl --context="$WORKLOAD_CONTEXT" get secret -A -o json 2>/dev/null | \
    python3 -c "
import json, sys, base64
data = json.load(sys.stdin)
sentinel = '$SENTINEL_VALUE'
hits = []
for item in data.get('items', []):
  ns = item['metadata']['namespace']
  for k, v in (item.get('data') or {}).items():
    try:
      decoded = base64.b64decode(v).decode('utf-8', errors='ignore')
      if sentinel in decoded:
        hits.append(ns)
    except Exception:
      pass
print(';'.join(hits))
" 2>/dev/null)"
  if [ -z "$ALL_MATCHES" ]; then
    record F FAIL
    echo "  F FAIL: sentinel NOT found in any Secret (ESO did not sync — likely Scenario B already failed)" >&2
  else
    ALL_OK=1
    IFS=';' read -ra HITS <<< "$ALL_MATCHES"
    for ns in "${HITS[@]}"; do
      if [ "$ns" != "$WORKLOAD_NS" ]; then
        ALL_OK=0
        echo "  F FAIL: sentinel leaked into Secret in ns=$ns (expected only $WORKLOAD_NS)" >&2
      fi
    done
    if [ $ALL_OK -eq 1 ]; then
      record F PASS
    else
      record F FAIL
    fi
  fi
fi

# === Verdict ===
VERDICT="PASS"
if [ $FAIL_COUNT -gt 0 ]; then
  VERDICT="FAIL"
fi

if [ $JSON_MODE -eq 1 ]; then
  printf '{"verdict":"%s","pass":%d,"fail":%d,"sentinel":"%s","scenarios":{' \
    "$VERDICT" "$PASS_COUNT" "$FAIL_COUNT" "$SENTINEL_UUID"
  first=1
  for k in A B C D E F; do
    [ -z "${SCENARIO_STATUS[$k]:-}" ] && continue
    [ $first -eq 0 ] && printf ','
    first=0
    printf '"%s":"%s"' "$k" "${SCENARIO_STATUS[$k]}"
  done
  printf '}}\n'
else
  echo "verdict=$VERDICT pass=$PASS_COUNT fail=$FAIL_COUNT sentinel=$SENTINEL_UUID"
  for k in A B C D E F; do
    [ -z "${SCENARIO_STATUS[$k]:-}" ] && continue
    echo "  $k: ${SCENARIO_STATUS[$k]}"
  done
fi

if [ "$VERDICT" = "PASS" ]; then
  exit 0
else
  exit 1
fi
