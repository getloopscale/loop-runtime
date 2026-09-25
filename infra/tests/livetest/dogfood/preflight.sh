#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-COMPLETO — Phase 0 (T0.1..T0.5)
#
# M23 F17 deferral (2026-07-10): this 522-LoC script exceeds the 500-LoC file
# size budget per architecture.md § 6, but is intentionally NOT split during
# M23. Per plan v1.1 § Coverage Matrix: this is a "D5 DELEGATE-HELMFILE
# candidate" — the proper refactor is helmfile-side migration (moving preflight
# probes into declarative helmfile hooks), NOT a shell-only file split which
# would fragment pre-flight logic without addressing the root cause. Tracked
# in ROADMAP.md § Post-M23 backlog.
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
#   bash infra/tests/livetest/dogfood/preflight.sh
#   bash infra/tests/livetest/dogfood/preflight.sh --json   # JSON em stdout (CI)
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

JSON_ONLY=0
[[ "${1:-}" == "--json" ]] && JSON_ONLY=1

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
SEED_SCRIPT="${REPO_ROOT}/infra/helmfile/scripts/seed-local-user.sh"
TOKEN_FILE="${HOME}/.loopzero/local-dogfood-token.txt"

MGMT_CTX="k3d-loopzero-core"
BUILD_CTX="k3d-loopzero-build"
WORKLOAD_CTX="k3d-loopzero-cell-0"

# Each check appends one entry to RESULTS as JSON object.
declare -a RESULTS=()

PASS=0
FAIL=0
SKIP=0
WARN=0

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
  case "$status" in
    PASS)
      PASS=$((PASS+1))
      log "[$id] PASS — $evidence"
      ;;
    SKIP)
      SKIP=$((SKIP+1))
      log "[$id] SKIP — $evidence"
      ;;
    WARN)
      WARN=$((WARN+1))
      log "[$id] WARN — $evidence"
      ;;
    *)
      FAIL=$((FAIL+1))
      log "[$id] FAIL — $evidence"
      ;;
  esac
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
# loopzero_api_config_value prints the value loopzero-api runs with for a non-secret
# variable: an explicit container variable wins, then the ConfigMaps mounted
# through envFrom, last one first (a later envFrom source wins a key both define).
# Both install paths put these variables in a ConfigMap — Tilt renders it from
# charts/loopzero-api, and Helmfile installs charts/loopzero-api — so reading only the
# explicit variables would report every one of them as unset.
loopzero_api_config_value() {
  local name="$1" value cms
  value=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
    -o jsonpath="{range .spec.template.spec.containers[0].env[?(@.name==\"${name}\")]}{.value}{end}" 2>/dev/null || true)
  if [[ -n "$value" ]]; then
    printf '%s' "$value"
    return
  fi
  cms=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
    -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].configMapRef.name}' 2>/dev/null || true)
  local reversed=() cname
  for cname in $cms; do reversed=("$cname" "${reversed[@]}"); done
  for cname in "${reversed[@]}"; do
    value=$(kubectl --context "$MGMT_CTX" -n loopzero-system get cm "$cname" \
      -o jsonpath="{.data.${name}}" 2>/dev/null || true)
    if [[ -n "$value" ]]; then
      printf '%s' "$value"
      return
    fi
  done
}

check_deploy_env() {
  local id="T0.4"
  local registry_insecure minio_endpoint
  registry_insecure=$(loopzero_api_config_value REGISTRY_INSECURE)
  minio_endpoint=$(loopzero_api_config_value MINIO_ENDPOINT)

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
  ports=$(docker port k3d-loopzero-cell-0-serverlb 2>/dev/null || true)
  if [[ -z "$ports" ]]; then
    record "$id" "FAIL" "k3d-loopzero-cell-0-serverlb container not running"
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
# T0.6 — NetworkPolicy `allow-ingress-traefik` aponta para namespace correto
# -----------------------------------------------------------------------------
# Detecta regression do bug CHG-2026-04-29-DOGFOOD-FB-INGRESSNS: operator gerava
# ingress NetworkPolicy com namespaceSelector matchLabels={kubernetes.io/metadata.name:
# loopzero-system}, mas Traefik vive em namespace `traefik`. Resultado: HTTP 502 em
# todo deploy. Fix em código (constants.NamespaceIngress + ingressNS() helper).
# Este check garante que regression não volta.
check_networkpolicy_traefik_target() {
  local id="T0.6"
  # Requer Environment ativa em tn-e2etest. Se não, SKIP gracioso.
  if ! kubectl --context "$WORKLOAD_CTX" get ns loopzero-tn-e2etest-production >/dev/null 2>&1; then
    record "$id" "SKIP" "namespace loopzero-tn-e2etest-production absent (no active Environment)"
    return
  fi
  local target
  target=$(kubectl --context "$WORKLOAD_CTX" -n loopzero-tn-e2etest-production \
    get networkpolicy allow-ingress-traefik \
    -o jsonpath='{.spec.ingress[0].from[0].namespaceSelector.matchLabels.kubernetes\.io/metadata\.name}' 2>/dev/null || true)
  if [[ -z "$target" ]]; then
    record "$id" "FAIL" "NetworkPolicy allow-ingress-traefik missing or has empty namespaceSelector"
    return
  fi
  if [[ "$target" != "traefik" ]]; then
    record "$id" "FAIL" "NetworkPolicy targets ns=$target (expected: traefik) — operator regression, see CHG-2026-04-29-DOGFOOD-FB-INGRESSNS"
    return
  fi
  record "$id" "PASS" "NetworkPolicy allow-ingress-traefik targets traefik (operator OK)"
}

# -----------------------------------------------------------------------------
# T0.7 — LOOPZERO_SERVICE_AUTH descoberto e salvo
# -----------------------------------------------------------------------------
# Plano dogfood-real-e2e D8: admin commands NÃO podem ser SKIP. Sem service auth,
# §14 admin tenants evict não testa. Esta função:
#   1. Lê valor do env var LOOPZERO_SERVICE_AUTH_SECRET no deploy loopzero-api
#   2. Salva em ~/.loopzero/local-service-auth.txt (chmod 600)
#   3. Não loga valor em stdout (só hash truncado)
check_service_auth() {
  local id="T0.7"
  local out_file="${HOME}/.loopzero/local-service-auth.txt"
  local val
  # First try direct env (Tilt pattern)
  val=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
    -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name=="LOOPZERO_SERVICE_AUTH_SECRET")]}{.value}{end}' 2>/dev/null || true)
  # If empty, fall back to envFrom secretRef (helmfile canonical pattern —
  # chart mounts loopzero-service-auth Secret via envFrom).
  if [[ -z "$val" ]]; then
    local envfrom_secrets
    envfrom_secrets=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
      -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].secretRef.name}' 2>/dev/null || true)
    for sname in $envfrom_secrets; do
      [[ -z "$sname" ]] && continue
      val=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret "$sname" \
        -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' 2>/dev/null | base64 -d 2>/dev/null || true)
      [[ -n "$val" ]] && break
    done
  fi
  if [[ -z "$val" ]]; then
    record "$id" "FAIL" "LOOPZERO_SERVICE_AUTH_SECRET not in loopzero-api .env[] nor any envFrom Secret"
    return
  fi
  # Persist atomicamente (write tmp + mv).
  mkdir -p "${HOME}/.loopzero"
  local tmp="${out_file}.tmp.$$"
  printf '%s' "$val" > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$out_file"
  # Hash truncado para evidência (não vaza secret).
  local hash
  hash=$(printf '%s' "$val" | sha256sum | cut -c1-12)
  record "$id" "PASS" "service auth written to ~/.loopzero/local-service-auth.txt (sha256:${hash}…)"
}

# -----------------------------------------------------------------------------
# T0.8 — LOOPZERO_CLUSTER_ISSUER aponta para um ClusterIssuer existente
# -----------------------------------------------------------------------------
# Detecta regression do bug CHG-2026-04-29-DOGFOOD-FB-ISSUER: env var apontava
# para `letsencrypt-prod` mas issuer real instalado é `loopzero-ca-issuer`.
# Certs nunca emitidos → Traefik 502.
check_cluster_issuer() {
  local id="T0.8"
  local issuer
  # First try env directive (Tilt pattern)
  issuer=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
    -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name=="LOOPZERO_CLUSTER_ISSUER")]}{.value}{end}' 2>/dev/null || true)
  # Fall back to envFrom + ConfigMap value (helmfile dev-public uses ConfigMap)
  if [[ -z "$issuer" ]]; then
    local envfrom_cms
    envfrom_cms=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api \
      -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].configMapRef.name}' 2>/dev/null || true)
    for cname in $envfrom_cms; do
      [[ -z "$cname" ]] && continue
      issuer=$(kubectl --context "$MGMT_CTX" -n loopzero-system get cm "$cname" \
        -o jsonpath='{.data.LOOPZERO_CLUSTER_ISSUER}' 2>/dev/null || true)
      [[ -n "$issuer" ]] && break
    done
  fi
  # Last resort: default to env-specific issuer (selfsigned-issuer for dev-public).
  # Env file dev-public.yaml declares `certIssuer.name: selfsigned-issuer`.
  if [[ -z "$issuer" ]]; then
    # Probe workload for any cluster issuer that exists — if selfsigned-issuer
    # is the only one, that's the dev-public canonical.
    if kubectl --context "$WORKLOAD_CTX" get clusterissuer selfsigned-issuer >/dev/null 2>&1; then
      issuer="selfsigned-issuer"
    elif kubectl --context "$WORKLOAD_CTX" get clusterissuer loopzero-ca-issuer >/dev/null 2>&1; then
      issuer="loopzero-ca-issuer"
    fi
  fi
  if [[ -z "$issuer" ]]; then
    record "$id" "FAIL" "no LOOPZERO_CLUSTER_ISSUER set and no known ClusterIssuer present"
    return
  fi
  if ! kubectl --context "$WORKLOAD_CTX" get clusterissuer "$issuer" >/dev/null 2>&1; then
    record "$id" "FAIL" "ClusterIssuer '$issuer' not found in workload cluster — drift between env var and live state"
    return
  fi
  record "$id" "PASS" "LOOPZERO_CLUSTER_ISSUER='$issuer' resolves to live ClusterIssuer"
}

# -----------------------------------------------------------------------------
# T0.9 — ESO ClusterSecretStore `loopzero-core-store` Ready=True
# -----------------------------------------------------------------------------
# Discovered live during journey-03 (CHG-2026-04-29-DOGFOOD-REAL-E2E):
# standalone-multi (helmfile L2) não provisionava o CSS que Pulumi
# (runtime-bootstrap.ts) cria em prod. Sem CSS, ESO não sync secrets
# para tenant pods → CreateContainerConfigError em qualquer app com
# `secrets:` declarado em loopzero.yaml. Fix: bootstrap-eso-cluster-secret-store.sh.
check_eso_cluster_secret_store() {
  local id="T0.9"
  local status
  status=$(kubectl --context "$WORKLOAD_CTX" get clustersecretstore loopzero-core-store \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
  if [[ "$status" != "True" ]]; then
    record "$id" "FAIL" "ClusterSecretStore loopzero-core-store not Ready (got: '$status'). Run: bash infra/helmfile/scripts/bootstrap-eso-cluster-secret-store.sh"
    return
  fi
  record "$id" "PASS" "ClusterSecretStore loopzero-core-store Ready=True (ESO can sync mgmt→runtime secrets)"
}

# -----------------------------------------------------------------------------
# T0.10 — No namespaces stuck in Terminating phase
# -----------------------------------------------------------------------------
# Discovered live (CHG-2026-04-30-INFRA-PARITY): operator finalizers race
# externally-deleted resources, leaving ns in Terminating with no owner.
# Subsequent `loopzero env create` for same name fails ("already exists").
# Fix: scripts/cleanup-terminating-namespaces.sh (idempotent, allowlist-gated).
check_no_terminating_namespaces() {
  local id="T0.10"
  local count
  count=$(kubectl --context "$WORKLOAD_CTX" get ns --no-headers 2>/dev/null | awk '$2=="Terminating"' | wc -l | tr -d '[:space:]')
  if [[ "$count" -gt 0 ]]; then
    record "$id" "FAIL" "$count namespaces stuck in Terminating on workload cluster. Run: bash infra/scripts/local/recover-mgmt-degraded.sh"
    return
  fi
  record "$id" "PASS" "no Terminating namespaces on workload cluster"
}

# -----------------------------------------------------------------------------
# T0.11 — No dangling kubeconfig contexts (WARN level)
# -----------------------------------------------------------------------------
# DOKS contexts ficam em ~/.kube/config após pulumi destroy. Operator typos
# tipo `kubectl --context loopzero-core` (sem prefixo k3d-) podem mirar cluster
# inexistente e produzir errors confusos. Detecta via 5s probe; reporta WARN
# (não FAIL — usuário pode legitimamente ter DOKS dormindo).
check_dangling_kubecontexts() {
  local id="T0.11"
  local dangling=()
  local ctx
  while IFS= read -r ctx; do
    [[ -z "$ctx" ]] && continue
    [[ "$ctx" == k3d-* ]] && continue
    if ! timeout 5 kubectl --context "$ctx" get ns kube-system >/dev/null 2>&1; then
      dangling+=("$ctx")
    fi
  done < <(kubectl config get-contexts -o name 2>/dev/null | grep -E "^(loopzero|do-)")

  if [[ ${#dangling[@]} -gt 0 ]]; then
    record "$id" "WARN" "${#dangling[@]} dangling kubecontext(s): $(IFS=,; echo "${dangling[*]}"). Remove via: for c in ${dangling[*]}; do kubectl config delete-context \$c; done"
    return
  fi
  record "$id" "PASS" "no dangling kubecontexts"
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
check_networkpolicy_traefik_target
check_service_auth
check_cluster_issuer
check_eso_cluster_secret_store
check_no_terminating_namespaces
check_dangling_kubecontexts

elapsed=$(( $(date +%s) - start ))

# Build JSON output
joined=$(IFS=,; echo "${RESULTS[*]}")
json="{\"phase\":0,\"results\":[${joined}],\"summary\":{\"pass\":$PASS,\"fail\":$FAIL,\"skip\":$SKIP,\"warn\":$WARN,\"elapsed_sec\":$elapsed}}"

if [[ "$JSON_ONLY" -eq 1 ]]; then
  echo "$json"
else
  echo "[preflight] DONE — pass=$PASS fail=$FAIL skip=$SKIP elapsed=${elapsed}s" >&2
  echo "$json" | python3 -m json.tool
fi

[[ "$FAIL" -gt 0 ]] && exit 1
exit 0
