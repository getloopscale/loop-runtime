#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# dev-public-secrets-write-path-subset_test.sh — iter 31 productive-idle
# Option A LIVE subset of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2.
#
# Validates the J-22 write-path canonically WITHOUT requiring ESO controller
# in mgmt cluster. Architectural finding (LIVE pre-flight 2026-06-10): ESO
# controller is deployed in workload + runtime-2 clusters only, NOT in mgmt.
# The full T4.1 6/6 scenarios require ESO in mgmt (Option B in runbook §
# Open Question) which is OUT-OF-SCOPE for the iter 1 dev-public LIVE test.
#
# This subset covers 3 of 6 T4.1 scenarios that test the write-path only
# (no ESO sync dependency):
#
#   A: PG encrypted_value column starts with vault:v1: (canonical zero-leak proof)
#   D: kubectl logs no sentinel leak (loopzero-api log redaction works)
#   E: kubectl describe pods no plaintext value leak
#
# The 3 deferred scenarios depend on the ESO read-path:
#   B: workload K8s Secret has plaintext (ESO ExternalSecret reconcile)
#   C: pod printenv KEY plaintext (envFrom of ESO-created Secret)
#   F: sentinel only in tenant workload ns Secret (ESO scope)
#
# When ESO is later deployed in mgmt (Option B execution), run the full
# `dev-public-secrets-zero-leak_test.sh` for all 6 scenarios.

set -uo pipefail
cd "$(dirname "$0")/../../.."

# === Flag parsing ===
SKIP_LIVE=0
JSON_MODE=0
TENANT_ID="${LOOPZERO_TEST_TENANT_ID:-admin-loopzero-dev}"
PROJECT_NAME="${LOOPZERO_TEST_PROJECT:-teste}"
ENV_NAME="${LOOPZERO_TEST_ENV:-production}"
DEV_PUBLIC_BASE="${LOOPZERO_DEV_PUBLIC_BASE:-https://app-dev.loopzero.example}"
KUBE_CONTEXT="${LOOPZERO_KUBE_CONTEXT:-k3d-loopzero-mgmt}"
PG_CONN="${LOOPZERO_PG_CONN:-postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--skip-live] [--json] [--help]

LIVE 3-scenario write-path subset (A+D+E) for the J-22 OpenBao transit chain.

This script validates the canonical "no plaintext at rest" mandate WITHOUT
requiring ESO controller in mgmt cluster. Deferred scenarios (B+C+F that
depend on ESO sync) require the full 6/6 test once ESO is deployed in mgmt.

Scenarios:
  A: PG encrypted_value column starts with vault:v1: (canonical proof)
  D: kubectl logs -A | grep sentinel → 0 matches (no log leak)
  E: kubectl describe pods -A shows KEY name but NOT plaintext value

Flags:
  --skip-live   Hermetic mode — skip LIVE probes (CI test mode)
  --json        Emit machine-readable JSON
  --help, -h    Show this help

Env overrides:
  LOOPZERO_TEST_TENANT_ID    Tenant ID (default admin-loopzero-dev)
  LOOPZERO_TEST_PROJECT      Project (default teste)
  LOOPZERO_TEST_ENV          Env (default production)
  LOOPZERO_DEV_PUBLIC_BASE   Cloud base URL (default https://cloud-dev.loopzero.example)
  LOOPZERO_KUBE_CONTEXT      Mgmt cluster context (default k3d-loopzero-mgmt)
  LOOPZERO_PG_CONN           Postgres connection string (default standalone dev)

Exit codes:
  0  All 3 scenarios PASS (write-path canonical)
  1  At least one scenario FAIL (zero-leak invariant violated)
  2  Usage error
EOF
}

for arg in "$@"; do
  case "$arg" in
    --skip-live) SKIP_LIVE=1 ;;
    --json) JSON_MODE=1 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown flag: $arg" >&2; usage; exit 2 ;;
  esac
done

# === Ephemeral sentinel ===
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

# === Cleanup trap ===
cleanup() {
  if [ $SKIP_LIVE -eq 1 ]; then return; fi
  # Best-effort: delete the probe secret via DELETE endpoint so failed
  # runs don't leave the sentinel orphaned in PG.
  if [ -n "${JWT:-}" ]; then
    curl -fsS -X DELETE \
      -H "Authorization: Bearer $JWT" \
      "$DEV_PUBLIC_BASE/api/v1/secrets/$SECRET_NAME?project=$PROJECT_NAME&env=$ENV_NAME" \
      >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

# === Hermetic mode short-circuit ===
if [ $SKIP_LIVE -eq 1 ]; then
  if [ $JSON_MODE -eq 1 ]; then
    printf '{"verdict":"SKIPPED","pass":0,"fail":0,"reason":"--skip-live (hermetic mode)","mode":"write-path-subset"}\n'
  else
    echo "verdict=SKIPPED pass=0 fail=0 reason=--skip-live mode=write-path-subset"
  fi
  exit 0
fi

# === LIVE invocation ===
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

# Brief wait for loopzero-api to fully commit the row (PG transaction may be
# in-flight when curl returns 200).
sleep 2

# === Scenario A: PG encrypted_value has vault:v1: prefix ===
# Canonical zero-leak proof: the PG row MUST contain ciphertext bytes
# starting with "vault:v1:" — NOT plaintext. This is THE signal that
# OpenBao transit Encrypt was invoked client-side BEFORE store.Set.
echo "[scenario A] psql query: encrypted_value prefix"
A_RESULT="$(PGPASSWORD=loopzero-dev-password psql -h 127.0.0.1 -p 15432 -U loopzero -d loopzero -tA \
  -c "SELECT substring(encode(encrypted_value, 'escape') for 9) FROM secrets WHERE name='$SECRET_NAME' LIMIT 1;" 2>&1 | tr -d '[:space:]')"
if [ "$A_RESULT" = "vault:v1:" ]; then
  echo "  PASS: PG encrypted_value starts with vault:v1:"
  record A PASS
else
  echo "  FAIL: PG encrypted_value prefix = '$A_RESULT' (expected 'vault:v1:')"
  echo "  This means the transit encrypt path was NOT exercised — the row"
  echo "  was written via the legacy AES path OR plaintext. The sponsor"
  echo "  walkthrough cannot proceed until this PASSes."
  record A FAIL
fi

# === Scenario D: loopzero-api logs no sentinel leak ===
echo "[scenario D] kubectl logs grep sentinel in loopzero-api (last 5min)"
D_HITS="$(kubectl --context="$KUBE_CONTEXT" -n loopzero-system logs deploy/loopzero-api --since=5m 2>&1 | grep -c "$SENTINEL_VALUE" || true)"
if [ "$D_HITS" -eq 0 ]; then
  echo "  PASS: 0 sentinel matches in loopzero-api logs"
  record D PASS
else
  echo "  FAIL: $D_HITS sentinel matches in loopzero-api logs (log redaction broken)"
  record D FAIL
fi

# === Scenario E: kubectl describe pods no value leak ===
# The probe secret name MAY appear in describe output (env var name is OK);
# the probe VALUE (sentinel) MUST NOT appear (defense vs accidental
# plaintext interpolation in pod spec).
echo "[scenario E] kubectl describe pods across all namespaces for value leak"
E_HITS="$(kubectl --context="$KUBE_CONTEXT" describe pods --all-namespaces 2>&1 | grep -c "$SENTINEL_VALUE" || true)"
if [ "$E_HITS" -eq 0 ]; then
  echo "  PASS: 0 value matches in describe pods output"
  record E PASS
else
  echo "  FAIL: $E_HITS value matches in describe pods (pod spec leaked plaintext)"
  record E FAIL
fi

# === Verdict ===
VERDICT="$([ $FAIL_COUNT -eq 0 ] && echo PASS || echo FAIL)"

if [ $JSON_MODE -eq 1 ]; then
  printf '{"verdict":"%s","pass":%d,"fail":%d,"mode":"write-path-subset","scenarios":{' \
    "$VERDICT" "$PASS_COUNT" "$FAIL_COUNT"
  printf '"A":"%s","D":"%s","E":"%s"' \
    "${SCENARIO_STATUS[A]:-N/A}" "${SCENARIO_STATUS[D]:-N/A}" "${SCENARIO_STATUS[E]:-N/A}"
  printf '},"deferred":["B","C","F"],"reason":"ESO controller not in mgmt cluster (Option A scope)"}\n'
else
  echo ""
  echo "=== Verdict ==="
  echo "verdict=$VERDICT pass=$PASS_COUNT fail=$FAIL_COUNT mode=write-path-subset"
  echo "scenarios: A=${SCENARIO_STATUS[A]:-N/A}  D=${SCENARIO_STATUS[D]:-N/A}  E=${SCENARIO_STATUS[E]:-N/A}"
  echo "deferred:  B C F (require ESO in mgmt cluster — see runbook § Open Question)"
fi

[ $FAIL_COUNT -eq 0 ] && exit 0 || exit 1
