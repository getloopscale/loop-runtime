#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — shared helpers for journey-NN-*.sh scripts.
#
# Exposes:
#   - Token & service auth resolution
#   - kubectl context helpers
#   - HTTP-200 polling with TLS via Traefik :8443 (--connect-to)
#   - Audit log assertions
#   - Cleanup verification (Prova de Morte)
#   - JSON record helpers (idempotent across subshells via tmpfile)
#
# Each journey sources this file:
#   source "$(dirname "$0")/_common.sh"
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

# shellcheck disable=SC2034  # exported helpers are referenced by other scripts.

set -uo pipefail  # NOT -e — journeys handle their own error flow.

LOOPZERO_BIN="${LOOPZERO_BIN:-loopzero}"
LOOPZERO_URL="${LOOPZERO_URL:-http://localhost:13001}"
export LOOPZERO_URL
TOKEN_FILE="${HOME}/.loopzero/local-dogfood-token.txt"
SERVICE_AUTH_FILE="${HOME}/.loopzero/local-service-auth.txt"
SEED_SCRIPT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}/infra/helmfile/scripts/seed-local-user.sh"

MGMT_CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
BUILD_CTX="${BUILD_CTX:-k3d-loopzero-build}"
WORKLOAD_CTX="${WORKLOAD_CTX:-k3d-loopzero-workload}"
TRAEFIK_HOST_PORT="${TRAEFIK_HOST_PORT:-8443}"

# Standalone-multi tenant from seed-local-user.sh
TENANT_ID="${TENANT_ID:-tn-e2etest}"

# log writes to stderr.
journey_log() { echo "[$(basename "${BASH_SOURCE[1]:-journey}")] $*" >&2; }
journey_step() { echo -e "\n[$(basename "${BASH_SOURCE[1]:-journey}")] === $* ===" >&2; }
journey_fail() { echo "[$(basename "${BASH_SOURCE[1]:-journey}")] FAIL: $*" >&2; exit 1; }

# Make sure LOOPZERO_TOKEN is valid; regenerate via seed if needed.
ensure_token() {
  if [[ -z "${LOOPZERO_TOKEN:-}" ]]; then
    if [[ ! -f "$TOKEN_FILE" ]]; then
      journey_log "no token file; running seed"
      bash "$SEED_SCRIPT" >/dev/null 2>&1 || journey_fail "seed-local-user.sh failed"
    fi
    LOOPZERO_TOKEN=$(cat "$TOKEN_FILE")
    export LOOPZERO_TOKEN
  fi

  # Quick liveness check.
  local code
  code=$(curl -sS -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${LOOPZERO_TOKEN}" \
    "${LOOPZERO_URL}/api/v1/auth/whoami" 2>/dev/null || echo 000)
  if [[ "$code" != "200" ]]; then
    journey_log "token invalid (whoami=$code); regenerating"
    bash "$SEED_SCRIPT" >/dev/null 2>&1 || journey_fail "seed-local-user.sh failed during regen"
    LOOPZERO_TOKEN=$(cat "$TOKEN_FILE")
    export LOOPZERO_TOKEN
  fi
}

# Resolve service auth secret (admin commands).
ensure_service_auth() {
  if [[ -z "${LOOPZERO_SERVICE_AUTH:-}" ]]; then
    [[ -f "$SERVICE_AUTH_FILE" ]] || journey_fail "service auth file missing — run preflight.sh first (T0.7)"
    LOOPZERO_SERVICE_AUTH=$(cat "$SERVICE_AUTH_FILE")
    export LOOPZERO_SERVICE_AUTH
  fi
}

# Plan T5.3 — resolve Traefik host-port for a given tenant (cell-aware).
# Tenants pinned to cell-2 land on loopzero-runtime-2 cluster which exposes Traefik
# on hostPort 8444 in k3d (vs loopzero-workload's 8443). Without resolution, probes
# always hit cell-default ports → 404 even when deploy succeeded on cell-2.
# Matrix entry LP-002 documents the divergence.
#
# Implementation: queries Tenant CR on mgmt cluster (`kubectl get tenant <id>`)
# for spec.cellID. Falls back to TRAEFIK_HOST_PORT when kubectl unavailable OR
# tenant CR absent.
resolve_traefik_port_for_tenant() {
  local tenant=$1
  local cell
  cell=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenant "$tenant" \
    -o jsonpath='{.spec.cellID}' 2>/dev/null || echo "")
  case "$cell" in
    cell-2) echo "8444" ;;
    *)      echo "${TRAEFIK_HOST_PORT}" ;;
  esac
}

# Wait for HTTP 200 on a LoopZero IngressRoute host.
# Args: host max_seconds [path] [tenant_id_for_cell_lookup]
wait_http_200() {
  local host=$1 max_seconds=${2:-300} path=${3:-/} tenant=${4:-}
  local elapsed=0 code port
  port="${TRAEFIK_HOST_PORT}"
  if [[ -n "$tenant" ]]; then
    port=$(resolve_traefik_port_for_tenant "$tenant")
  fi
  journey_log "waiting for HTTP 200 at https://${host}${path} (port=${port}, max ${max_seconds}s)"
  while (( elapsed < max_seconds )); do
    code=$(curl -sS -o /dev/null -w '%{http_code}' \
      --connect-to "${host}:443:127.0.0.1:${port}" \
      -k --max-time 5 \
      "https://${host}${path}" 2>/dev/null || echo 000)
    if [[ "$code" == "200" ]]; then
      journey_log "HTTP 200 OK at ${host}${path} (after ${elapsed}s, port=${port})"
      return 0
    fi
    sleep 5
    elapsed=$(( elapsed + 5 ))
  done
  journey_log "timeout (${max_seconds}s); last code=${code} (port=${port})"
  return 1
}

# Poll a build to terminal (SUCCEEDED/FAILED/CANCELLED).
# Args: build_id max_seconds. Echoes final status.
# Note: refreshes auth on 401s — local seed token TTL (~30m) can expire
# mid-loop on slow builds, and silently 401-polling for 25min looked like
# a journey hang. Now we re-call ensure_token whenever the API rejects auth.
wait_build_terminal() {
  local build_id=$1 max_seconds=${2:-1200}
  local elapsed=0 status http_code
  while (( elapsed < max_seconds )); do
    http_code=$(curl -sS -o /tmp/.build-status-body -w '%{http_code}' \
      "${LOOPZERO_URL}/api/v1/builds/${build_id}" \
      -H "Authorization: Bearer ${LOOPZERO_TOKEN}" 2>/dev/null || echo 000)
    if [[ "$http_code" == "401" ]]; then
      journey_log "wait_build_terminal: auth expired mid-poll, regenerating"
      LOOPZERO_TOKEN=""
      ensure_token
      continue
    fi
    status=$(python3 -c "import sys,json; print(json.load(sys.stdin).get('status','?'))" < /tmp/.build-status-body 2>/dev/null || echo "?")
    case "$status" in
      SUCCEEDED|FAILED|CANCELLED)
        echo "$status"
        return 0
        ;;
    esac
    sleep 15
    elapsed=$(( elapsed + 15 ))
  done
  echo "TIMEOUT"
  return 1
}

# Wait for release tied to a SPECIFIC build to reach ACTIVE in DB.
# Critical: prior-run releases can linger as ACTIVE because `loopzero project delete`
# does not transition them (state machine forbids ACTIVE→CANCELLED — they're only
# superseded via NewReleaseActivated when the convergence loop activates the next
# release). A wait that just checks "any active release" can return immediately on
# stale rows. We must verify the build_id matches.
# Args: project env build_id max_seconds
wait_release_active() {
  local project=$1 env=$2 build_id=$3 max_seconds=${4:-180}
  local elapsed=0 status active_build http_code
  journey_log "waiting for release ACTIVE in DB for build=$build_id (project=$project env=$env max ${max_seconds}s)"
  while (( elapsed < max_seconds )); do
    http_code=$(curl -sS -o /tmp/.release-status-body -w '%{http_code}' \
      "${LOOPZERO_URL}/api/v1/projects/${project}/status?env=${env}" \
      -H "Authorization: Bearer ${LOOPZERO_TOKEN}" 2>/dev/null || echo 000)
    if [[ "$http_code" == "401" ]]; then
      journey_log "wait_release_active: auth expired mid-poll, regenerating"
      LOOPZERO_TOKEN=""
      ensure_token
      continue
    fi
    read -r status active_build <<<"$(python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    ar = d.get('active_release') or {}
    print(ar.get('status', '_'), ar.get('build_id', '_'))
except Exception:
    print('_ _')
" < /tmp/.release-status-body 2>/dev/null || echo '_ _')"
    if [[ "$status" == "ACTIVE" && "$active_build" == "$build_id" ]]; then
      journey_log "release ACTIVE confirmed for build=$build_id (after ${elapsed}s)"
      return 0
    fi
    sleep 5
    elapsed=$(( elapsed + 5 ))
  done
  journey_log "timeout — release for build=$build_id not ACTIVE after ${max_seconds}s (last status='${status}', active_build='${active_build}')"
  return 1
}

# Assert audit log has an entry matching action filter (substring match).
# Optional second arg filters further by project_name in metadata or resource_id.
# Queries via direct curl with limit=200 so journeys looking for recent
# entries don't miss them in the default 20-row page.
# Args: action_substring [project_name_filter]
assert_audit_entry() {
  local action=$1 project=${2:-}
  local body
  body=$(curl -sS "${LOOPZERO_URL}/api/v1/audit?limit=200" \
    -H "Authorization: Bearer ${LOOPZERO_TOKEN}" 2>/dev/null)
  local entries
  entries=$(python3 -c "
import sys, json
d = json.loads(sys.argv[1])
project = sys.argv[2]
matched = []
for e in d.get('entries', d.get('items', [])):
    if '$action' not in e.get('action', ''):
        continue
    if project:
        meta = e.get('metadata') or {}
        rid = e.get('resource_id', '')
        if project not in str(meta) and project not in rid:
            continue
    matched.append(e)
print(len(matched))
" "$body" "$project" 2>/dev/null || echo 0)
  if [[ "$entries" -lt 1 ]]; then
    journey_log "audit assertion FAILED — no entry with action='$action'${project:+ project='$project'}"
    return 1
  fi
  journey_log "audit OK: $entries entries with action='$action'${project:+ project='$project'}"
  return 0
}

# Prova de Morte — verify cleanup of a single PROJECT. The tenant namespace
# AND the ArgoCD App (`<tenant>-<env>`) are SHARED across projects in the
# same env. Cleanup verification per-project must therefore look at
# label-selected resources within the shared namespace, not at ns/app
# existence.
#
# Args: tenant project env max_seconds
prove_dead() {
  local tenant=$1 project=$2 env=$3 max_seconds=${4:-300}
  local ns="loopzero-${tenant}-${env}"
  local elapsed=0
  journey_log "Prova de Morte: ns=$ns project=$project (max ${max_seconds}s)"

  while (( elapsed < max_seconds )); do
    # If the tenant namespace itself is gone, the project is gone too.
    if ! kubectl --context "$WORKLOAD_CTX" get ns "$ns" >/dev/null 2>&1; then
      journey_log "Prova de Morte: tenant namespace gone (project guaranteed gone)"
      return 0
    fi

    # Per-project cleanup: zero resources with loopzero.io/project=<project> label.
    local res_count
    res_count=$(kubectl --context "$WORKLOAD_CTX" -n "$ns" \
      get deploy,svc,ingressroute.traefik.io,certificate.cert-manager.io \
      -l "loopzero.io/project=$project" \
      --no-headers 2>/dev/null | wc -l)
    if (( res_count == 0 )); then
      journey_log "Prova de Morte: cleanup complete after ${elapsed}s (zero resources for project=$project)"
      return 0
    fi

    sleep 10
    elapsed=$(( elapsed + 10 ))
  done
  journey_log "Prova de Morte: cleanup INCOMPLETE after ${max_seconds}s — resources still labeled with project=$project"
  kubectl --context "$WORKLOAD_CTX" -n "$ns" \
    get deploy,svc,ingressroute.traefik.io,certificate.cert-manager.io \
    -l "loopzero.io/project=$project" >&2 || true
  return 1
}

# State helpers — JSON I/O via jq-free python.
state_set() {
  local file=$1 key=$2 value=$3
  python3 - "$file" "$key" "$value" <<'PY'
import json, sys
path, key, value = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    with open(path) as f:
        data = json.load(f)
except Exception:
    data = {}
data[key] = value
with open(path, "w") as f:
    json.dump(data, f, indent=2)
PY
}

state_get() {
  local file=$1 key=$2
  python3 - "$file" "$key" <<'PY' 2>/dev/null
import json, sys
path, key = sys.argv[1], sys.argv[2]
try:
    with open(path) as f:
        print(json.load(f).get(key, ""))
except Exception:
    print("")
PY
}
