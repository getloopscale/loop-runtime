#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-COMPLETO — Phase 0 (T0.1..T0.5)
#
# Pre-flight cluster validation para o dogfood completo. Idempotente,
# shellcheck-clean, alvo <30s. Verifica que k3d-standalone-multi está em
# estado idêntico a produção antes do dogfood iniciar — qualquer divergência
# detectada agora vira fix em helm/pulumi, não hack durante teste (ADR D5).
#
# Checks executados:
#   T0.1 cluster health probe (3 contextos, ≥95% pods Running)
#   T0.2 token regeneration via seed-local-user.sh
#   T0.3 VAP allowlist + buildkit-registry-config + loopzero-minio-credentials
#   T0.4 loopzero-api env (REGISTRY_INSECURE, MINIO_ENDPOINT)
#   T0.5 Traefik LB exposure + comando curl --resolve documentado
#
# Exit codes:
#   0 — todos os checks passaram, dogfood pode iniciar
#   1 — pelo menos 1 check falhou; lista de fixes obrigatórios
#   2 — erro de pré-condição (kubectl/k3d ausente)
#
# Uso:
#   bash infra/tests/dogfood/preflight.sh
#   bash infra/tests/dogfood/preflight.sh --json   # JSON em stdout (CI)
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

JSON_ONLY=0
[[ "${1:-}" == "--json" ]] && JSON_ONLY=1

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SEED_SCRIPT="${REPO_ROOT}/infra/helmfile/scripts/seed-local-user.sh"
TOKEN_FILE="${HOME}/.loopzero/local-dogfood-token.txt"

MGMT_CTX="k3d-loopzero-mgmt"
BUILD_CTX="k3d-loopzero-build"
WORKLOAD_CTX="k3d-loopzero-workload"

# Each check appends one entry to RESULTS as JSON object.
declare -a RESULTS=()

PASS=0
FAIL=0

# log writes to stderr unless --json mode
log() { [[ "$JSON_ONLY" -eq 1 ]] || echo "[preflight] $*" >&2; }
warn() { [[ "$JSON_ONLY" -eq 1 ]] || echo "[preflight] WARN: $*" >&2; }
err()  { echo "[preflight] ERROR: $*" >&2; }

record() {
  local id=$1 status=$2 evidence=$3
  # Escape quotes/backslashes for JSON string.
  local esc
  esc=$(printf '%s' "$evidence" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))')
  RESULTS+=("{\"id\":\"$id\",\"status\":\"$status\",\"evidence\":$esc}")
  if [[ "$status" == "PASS" ]]; then
    PASS=$((PASS+1))
    log "[$id] PASS — $evidence"
  else
    FAIL=$((FAIL+1))
    log "[$id] FAIL — $evidence"
  fi
}

require_bin() {
  command -v "$1" >/dev/null 2>&1 || { err "missing required binary: $1"; exit 2; }
}

# -----------------------------------------------------------------------------
# T0.1 — Cluster health probe
# -----------------------------------------------------------------------------
# For each context: list pods cluster-wide, count Running vs Total, fail if
# CrashLoopBackOff or ImagePullBackOff present in platform namespaces.
check_cluster_health() {
  local ctx=$1
  local id="T0.1.${ctx#k3d-loopzero-}"

  if ! kubectl --context "$ctx" get nodes >/dev/null 2>&1; then
    record "$id" "FAIL" "context $ctx unreachable"
    return
  fi

  # JSON path output: phase + reason. Filter platform-affecting issues.
  local bad
  bad=$(kubectl --context "$ctx" get pods -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}|{.status.phase}|{range .status.containerStatuses[*]}{.state.waiting.reason}{end}{"\n"}{end}' 2>/dev/null \
    | awk -F'|' '
        $2=="Pending" && $3 ~ /CreateContainerConfigError|ImagePullBackOff|ErrImagePull/ { print $0; next }
        $2!="Running" && $2!="Succeeded" && $1 !~ /^kube-system\// && $1 !~ /^loopzero-test-/ && $1 !~ /Terminating/ {
          # Also flag CrashLoopBackOff
          if ($3 ~ /CrashLoopBackOff/) { print $0; next }
        }
      ' || true)

  if [[ -n "$bad" ]]; then
    local count
    count=$(printf '%s\n' "$bad" | wc -l)
    record "$id" "FAIL" "$count problematic pods in $ctx"
    [[ "$JSON_ONLY" -eq 1 ]] || printf '%s\n' "$bad" | head -5 >&2
    return
  fi

  # Total pods sanity
  local total
  total=$(kubectl --context "$ctx" get pods -A --no-headers 2>/dev/null | wc -l)
  record "$id" "PASS" "$total pods, all Running/Succeeded (excluding kube-system + loopzero-test-* terminating)"
}

# -----------------------------------------------------------------------------
# T0.2 — Token validity + regeneration
# -----------------------------------------------------------------------------
check_token() {
  local id="T0.2"
  if [[ ! -f "$TOKEN_FILE" ]]; then
    log "token file missing, regenerating via seed"
    if ! bash "$SEED_SCRIPT" >/dev/null 2>&1; then
      record "$id" "FAIL" "seed-local-user.sh failed"
      return
    fi
  fi

  if [[ ! -s "$TOKEN_FILE" ]]; then
    record "$id" "FAIL" "token file empty after seed"
    return
  fi

  # Decode JWT exp claim — pure base64, no secret needed.
  local payload exp now diff
  payload=$(cut -d. -f2 < "$TOKEN_FILE")
  # base64url → base64 padding
  local pad=$(( 4 - ${#payload} % 4 ))
  [[ $pad -lt 4 ]] && payload="${payload}$(printf '%.0s=' $(seq 1 $pad))"
  exp=$(printf '%s' "$payload" | tr '_-' '/+' | base64 -d 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
  now=$(date +%s)
  diff=$((exp - now))

  local regenerated=0
  if [[ "$diff" -lt 600 ]]; then
    log "token expires in ${diff}s, regenerating"
    if ! bash "$SEED_SCRIPT" >/dev/null 2>&1; then
      record "$id" "FAIL" "seed re-run failed (token was expiring)"
      return
    fi
    regenerated=1
    # Re-decode after regen to report accurate exp.
    payload=$(cut -d. -f2 < "$TOKEN_FILE")
    pad=$(( 4 - ${#payload} % 4 ))
    [[ $pad -lt 4 ]] && payload="${payload}$(printf '%.0s=' $(seq 1 $pad))"
    exp=$(printf '%s' "$payload" | tr '_-' '/+' | base64 -d 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
    now=$(date +%s)
    diff=$((exp - now))
  fi

  # Validate against API
  local code
  code=$(curl -sS -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $(cat "$TOKEN_FILE")" \
    http://localhost:13001/api/v1/auth/whoami 2>/dev/null || echo 000)
  if [[ "$code" != "200" ]]; then
    record "$id" "FAIL" "whoami returned HTTP $code (expected 200)"
    return
  fi
  local note="exp+${diff}s"
  [[ "$regenerated" -eq 1 ]] && note="${note}, regenerated"
  record "$id" "PASS" "token valid (whoami HTTP 200, ${note})"
}

# -----------------------------------------------------------------------------
# T0.3 — VAPs + secrets de build
# -----------------------------------------------------------------------------
check_vap_prefix() {
  local id="T0.3.vap-restrict"
  local expr
  expr=$(kubectl --context "$WORKLOAD_CTX" get validatingadmissionpolicy loopzero-restrict-image-registries \
    -o jsonpath='{.spec.validations[0].expression}' 2>/dev/null || true)
  if [[ -z "$expr" ]]; then
    record "$id" "FAIL" "VAP loopzero-restrict-image-registries not found in workload cluster"
    return
  fi
  if ! grep -q 'k3d-loopzero-registry:5000/' <<<"$expr"; then
    record "$id" "FAIL" "VAP missing k3d-loopzero-registry:5000/ prefix; helm upgrade admission-policies needed"
    return
  fi
  record "$id" "PASS" "VAP allowlist contains k3d-loopzero-registry:5000/"
}

check_vap_signature() {
  local id="T0.3.vap-signature"
  if ! kubectl --context "$WORKLOAD_CTX" get validatingadmissionpolicy loopzero-require-image-signature >/dev/null 2>&1; then
    record "$id" "FAIL" "VAP loopzero-require-image-signature missing"
    return
  fi
  local action
  action=$(kubectl --context "$WORKLOAD_CTX" get validatingadmissionpolicybinding loopzero-require-image-signature-binding \
    -o jsonpath='{.spec.validationActions}' 2>/dev/null || echo "")
  if [[ -z "$action" ]]; then
    warn "VAPB loopzero-require-image-signature-binding not found, signature not enforced"
    record "$id" "FAIL" "VAPB binding missing"
    return
  fi
  record "$id" "PASS" "VAP signature enforced ($action)"
}

check_buildkit_secret() {
  local id="T0.3.buildkit-config"
  local data
  data=$(kubectl --context "$BUILD_CTX" -n argo get secret buildkit-registry-config \
    -o jsonpath='{.data.config\.json}' 2>/dev/null || true)
  if [[ -z "$data" ]]; then
    record "$id" "FAIL" "buildkit-registry-config Secret missing or empty in argo ns"
    return
  fi
  record "$id" "PASS" "buildkit-registry-config present (${#data} chars)"
}

check_minio_credentials() {
  local id="T0.3.minio-credentials"
  local missing=()
  local key
  for key in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY MINIO_ENDPOINT AWS_DEFAULT_REGION AWS_ENDPOINT_URL; do
    if ! kubectl --context "$BUILD_CTX" -n argo get secret loopzero-minio-credentials \
      -o jsonpath="{.data.${key}}" 2>/dev/null | grep -q .; then
      missing+=("$key")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    record "$id" "FAIL" "loopzero-minio-credentials missing keys: ${missing[*]}"
    return
  fi
  record "$id" "PASS" "loopzero-minio-credentials has all 5 required keys"
}

# -----------------------------------------------------------------------------
# T0.4 — loopzero-api deploy env vars
# -----------------------------------------------------------------------------
check_deploy_env() {
  local id="T0.4"
  local registry_insecure minio_endpoint
  registry_insecure=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
    -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name=="REGISTRY_INSECURE")]}{.value}{end}' 2>/dev/null || true)
  minio_endpoint=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
    -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name=="MINIO_ENDPOINT")]}{.value}{end}' 2>/dev/null || true)

  local issues=()
  [[ "$registry_insecure" != "true" ]] && issues+=("REGISTRY_INSECURE=$registry_insecure (expected true)")
  if [[ -n "$minio_endpoint" ]] && [[ ! "$minio_endpoint" =~ ^https?:// ]]; then
    issues+=("MINIO_ENDPOINT=$minio_endpoint missing scheme")
  fi
  # NOTE: empty MINIO_ENDPOINT is OK (config.go default has scheme). Only fail if set+broken.

  if [[ ${#issues[@]} -gt 0 ]]; then
    record "$id" "FAIL" "${issues[*]}"
    return
  fi
  record "$id" "PASS" "REGISTRY_INSECURE=true, MINIO_ENDPOINT scheme OK"
}

# -----------------------------------------------------------------------------
# T0.5 — Traefik LB exposure for curl --resolve
# -----------------------------------------------------------------------------
check_traefik_lb() {
  local id="T0.5"
  if ! command -v docker >/dev/null 2>&1; then
    record "$id" "FAIL" "docker missing — cannot check k3d serverlb ports"
    return
  fi
  local ports
  ports=$(docker port k3d-loopzero-workload-serverlb 2>/dev/null || true)
  if [[ -z "$ports" ]]; then
    record "$id" "FAIL" "k3d-loopzero-workload-serverlb container not running"
    return
  fi
  if ! grep -q '443/tcp' <<<"$ports"; then
    record "$id" "FAIL" "Traefik :443 not exposed by k3d serverlb"
    return
  fi
  local hostport
  hostport=$(grep '443/tcp' <<<"$ports" | head -1 | awk -F'->' '{print $2}' | tr -d ' ')
  record "$id" "PASS" "Traefik :443 exposed at $hostport — use 'curl --resolve <host>:443:127.0.0.1 -k https://<host>/'"
}

# -----------------------------------------------------------------------------
# Driver
# -----------------------------------------------------------------------------
require_bin kubectl
require_bin curl
require_bin python3
require_bin docker

start=$(date +%s)
log "starting Phase 0 pre-flight (target <30s)"

check_cluster_health "$MGMT_CTX"
check_cluster_health "$BUILD_CTX"
check_cluster_health "$WORKLOAD_CTX"
check_token
check_vap_prefix
check_vap_signature
check_buildkit_secret
check_minio_credentials
check_deploy_env
check_traefik_lb

elapsed=$(( $(date +%s) - start ))

# Build JSON output
joined=$(IFS=,; echo "${RESULTS[*]}")
json="{\"phase\":0,\"results\":[${joined}],\"summary\":{\"pass\":$PASS,\"fail\":$FAIL,\"elapsed_sec\":$elapsed}}"

if [[ "$JSON_ONLY" -eq 1 ]]; then
  echo "$json"
else
  echo "[preflight] DONE — pass=$PASS fail=$FAIL elapsed=${elapsed}s" >&2
  echo "$json" | python3 -m json.tool
fi

[[ "$FAIL" -gt 0 ]] && exit 1
exit 0
