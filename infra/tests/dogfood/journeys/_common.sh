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

# Wait for HTTP 200 on a LoopZero IngressRoute host.
# Args: host max_seconds [path]
wait_http_200() {
  local host=$1 max_seconds=${2:-300} path=${3:-/}
  local elapsed=0 code
  journey_log "waiting for HTTP 200 at https://${host}${path} (max ${max_seconds}s)"
  while (( elapsed < max_seconds )); do
    code=$(curl -sS -o /dev/null -w '%{http_code}' \
      --connect-to "${host}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" \
      -k --max-time 5 \
      "https://${host}${path}" 2>/dev/null || echo 000)
    if [[ "$code" == "200" ]]; then
      journey_log "HTTP 200 OK at ${host}${path} (after ${elapsed}s)"
      return 0
    fi
    sleep 5
    elapsed=$(( elapsed + 5 ))
  done
  journey_log "timeout (${max_seconds}s); last code=${code}"
  return 1
}

# Poll a build to terminal (SUCCEEDED/FAILED/CANCELLED).
# Args: build_id max_seconds. Echoes final status.
wait_build_terminal() {
  local build_id=$1 max_seconds=${2:-1200}
  local elapsed=0 status
  while (( elapsed < max_seconds )); do
    status=$(curl -sS "${LOOPZERO_URL}/api/v1/builds/${build_id}" \
      -H "Authorization: Bearer ${LOOPZERO_TOKEN}" 2>/dev/null \
      | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "?")
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

# Assert audit log has an entry matching action filter.
# Args: action substring [project]
assert_audit_entry() {
  local action=$1 project=${2:-}
  local entries
  entries=$("$LOOPZERO_BIN" audit --json 2>/dev/null \
    | python3 -c "
import sys, json
d = json.load(sys.stdin)
matched = [e for e in d.get('entries', d.get('items', []))
           if '$action' in e.get('action', '') ]
print(len(matched))
" 2>/dev/null || echo 0)
  if [[ "$entries" -lt 1 ]]; then
    journey_log "audit assertion FAILED — no entry with action='$action'"
    return 1
  fi
  journey_log "audit OK: $entries entries with action='$action'"
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
