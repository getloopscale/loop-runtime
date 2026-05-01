#!/usr/bin/env bash
# k3d-cross-cluster-heal.sh — idempotent recovery of cross-cluster connectivity
# in local k3d standalone-multi after `k3d cluster stop/start` cycles.
#
# CHG-2026-04-30-DOGFOOD-LOCAL-HEAL — codifies the 9 manual workarounds (H1-H9)
# discovered LIVE during dogfood E2E unblock 2026-04-30:
#
#   H1 — k3d cluster restart drops build/runtime nodes from the shared
#        `k3d-loopzero` docker network. Reattach all server/agent/serverlb
#        containers via `docker network connect`.
#
#   H2 — `loopzero-cluster-kubeconfigs` Secret stores in-cluster kubeconfigs
#        with a fixed CA cert + apiServer IP. After restart, k3s rotates
#        the CA AND the IP changes — Secret references stale data.
#        Regenerate kubeconfigs with current IPs + insecure-skip-tls-verify
#        (local-only — production keeps strict TLS via cert-manager).
#
#   H3 — ArgoCD `runtime-argocd-cluster` and `build-argocd-cluster` cluster
#        secrets carry the same stale apiServer URL. Recreate from scratch
#        (delete + apply) so ArgoCD discovery cache invalidates fully.
#
#   H4 — k3d-loopzero-registry hostname is not in coredns of the build cluster
#        once it joined `k3d-loopzero-build` private net first; even after H1
#        re-attach, /etc/hosts is needed because the agent's containerd
#        does ALL its image pulls before joining the cluster network. Add
#        an explicit hostAlias entry to all node /etc/hosts.
#
#   H5 — RBAC: build & runtime clusters have `kube-system:default` SA
#        without permission to create Workflow / LoopZeroApplication CRs. In
#        local dev, bind that SA to cluster-admin. Production uses
#        scoped per-cluster SAs (loopzero-ctrl-build, loopzero-ctrl-runtime).
#
#   H6 — `buildkit-registry-config` Secret in build cluster: when no DOCR
#        creds are available, the merge logic chokes on missing fields.
#        Set an empty `{"auths":{}}` so cosign --allow-insecure-registry
#        can short-circuit auth.
#
#   H7 — `loopzero-minio-credentials` MINIO_ENDPOINT must point to the mgmt
#        cluster NodePort 32000 reachable on the shared k3d-loopzero network.
#
#   H8 — loopzero-api Deployment env: LOOPZERO_PACKS_IMAGE, LOOPZERO_CLONE_IMAGE,
#        LOOPZERO_SYFT_IMAGE, LOOPZERO_COSIGN_IMAGE need to override the
#        registry.digitalocean.com defaults to point at k3d-loopzero-registry
#        (the local Docker registry). Permanent fix is helmfile values
#        in `infra/helm/loopzero-api/values-standalone-multi.yaml`; this
#        script applies as runtime patch in case helmfile sync is stale.
#
#   H9 — loopzero-build-cwts release: chart values default `images.cloneRunner`
#        to DOCR. Override at helm-upgrade time when env != "prod".
#
#   H10 — tenant-controller Deployment env WORKLOAD_CLUSTER_URL drifts every
#         k3d restart (current values overlay hardcodes a stale IP). Patch
#         live + roll the controller; existing ArgoCD Apps with stale
#         destination get a one-shot patch too.
#
#   H11 — ArgoCD token in loopzero-api-tokens Secret expired (24h-session token
#         instead of loopzero-api:apiKey non-expiring). loopzero-api can't trigger
#         post-deploy / post-delete sync → resources orphaned in workload.
#         Detect via JWT exp claim, regenerate via provision-api-tokens.sh.
#
#   H13 — cell-default ClusterCredential SA token rotated/invalidated.
#         Discovered LIVE 2026-05-01 17:40Z (regression matrix entry 154):
#         loopzero-system:default SA recreation during certain rollouts → SA UID
#         change → cached 24h token in cell-default-kubeconfig Secret returns
#         401 "the server has asked for the client to provide credentials".
#         Detection: connected=false. Fix: mint fresh `kubectl create token`,
#         patch Secret data.kubeconfig (preserves CA + in-cluster server URL).
#
#   H12 — ClusterCredential cell-1 missing despite cells SQL row status=active.
#         Discovered LIVE 2026-05-01 (regression matrix entry 153): cells
#         table seed creates 3 rows (cell-default, cell-1, cell-2) but
#         wire-clusters.sh non-fatal "WARN: continuing" path silently failed
#         on cell-1 → DynamicRouter.Resolve("cell-1") = unknown cell. Pin
#         para cell-1 atualizaria DB + CR, mas operator deploy falharia.
#         Fix: invoke clustercredential-create.sh directly using the workload
#         cluster scoped kubeconfig. Idempotent (kubectl apply).
#
# Idempotent — safe to re-run after every `k3d cluster start`.
#
# Pre-flight: requires kubectl + docker + k3d on PATH and three k3d
# clusters running (loopzero-mgmt, loopzero-build, loopzero-workload).
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

SHARED_NET="${SHARED_NET:-k3d-loopzero}"
MGMT_CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
BUILD_CTX="${BUILD_CTX:-k3d-loopzero-build}"
RT_CTX="${RT_CTX:-k3d-loopzero-workload}"
REGISTRY_HOST="${REGISTRY_HOST:-k3d-loopzero-registry}"

log() { printf '[k3d-heal] %s\n' "$*" >&2; }
warn() { printf '[k3d-heal] WARN: %s\n' "$*" >&2; }

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
require() {
  command -v "$1" >/dev/null 2>&1 || { warn "$1 not found in PATH"; exit 1; }
}
require kubectl
require docker
require k3d

for ctx in "$MGMT_CTX" "$BUILD_CTX" "$RT_CTX"; do
  if ! kubectl --context "$ctx" --request-timeout=5s cluster-info >/dev/null 2>&1; then
    warn "context $ctx not reachable — k3d cluster start required first"
    # not fatal — H1 attempts to fix
  fi
done

# ---------------------------------------------------------------------------
# H1 — Reattach build + runtime nodes to shared k3d-loopzero network.
# ---------------------------------------------------------------------------
log "H1: ensuring all k3d nodes are on shared network $SHARED_NET"
attach_to_shared() {
  local container="$1"
  if ! docker inspect "$container" >/dev/null 2>&1; then
    warn "container $container not running — skipping"
    return
  fi
  if docker inspect "$container" \
    --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' \
    2>/dev/null | grep -qw "$SHARED_NET"; then
    log "  $container: already on $SHARED_NET"
  else
    if docker network connect "$SHARED_NET" "$container" 2>/dev/null; then
      log "  $container: attached to $SHARED_NET"
    else
      warn "  $container: docker network connect failed"
    fi
  fi
}
for c in k3d-loopzero-build-server-0 k3d-loopzero-build-agent-0 k3d-loopzero-build-serverlb \
         k3d-loopzero-workload-server-0 k3d-loopzero-workload-agent-0 k3d-loopzero-workload-serverlb; do
  attach_to_shared "$c"
done

# Resolve current IPs on the shared network — used by H2/H3/H4/H7
ip_on() {
  docker inspect "$1" --format \
    "{{(index .NetworkSettings.Networks \"${SHARED_NET}\").IPAddress}}" \
    2>/dev/null || true
}
MGMT_IP=$(ip_on k3d-loopzero-mgmt-server-0)
BUILD_IP=$(ip_on k3d-loopzero-build-server-0)
RT_IP=$(ip_on k3d-loopzero-workload-server-0)
REG_IP=$(docker network inspect "$SHARED_NET" \
  --format '{{range .Containers}}{{if eq .Name "'"$REGISTRY_HOST"'"}}{{.IPv4Address}}{{end}}{{end}}' \
  2>/dev/null | sed 's|/.*||')
[[ -z "$MGMT_IP" || -z "$BUILD_IP" || -z "$RT_IP" ]] && {
  warn "cannot resolve IPs (MGMT=$MGMT_IP BUILD=$BUILD_IP RT=$RT_IP) — abort"
  exit 1
}
log "  resolved: MGMT=$MGMT_IP BUILD=$BUILD_IP RT=$RT_IP REG=$REG_IP"

# ---------------------------------------------------------------------------
# H4 — Add k3d-loopzero-registry to /etc/hosts on build + runtime nodes.
# Idempotent (grep before append). containerd reads /etc/hosts at pull time.
# ---------------------------------------------------------------------------
if [[ -n "$REG_IP" ]]; then
  log "H4: ensuring $REGISTRY_HOST hostAlias on build + runtime nodes"
  ensure_host() {
    local container="$1"
    docker inspect "$container" >/dev/null 2>&1 || return
    if docker exec "$container" grep -qE "^${REG_IP}[[:space:]]+${REGISTRY_HOST}\b" /etc/hosts 2>/dev/null; then
      log "  $container: $REGISTRY_HOST → $REG_IP already present"
    else
      docker exec "$container" sh -c \
        "sed -i '/[[:space:]]${REGISTRY_HOST}\$/d' /etc/hosts && echo '${REG_IP} ${REGISTRY_HOST}' >> /etc/hosts" \
        2>/dev/null && log "  $container: added $REG_IP $REGISTRY_HOST"
    fi
  }
  for c in k3d-loopzero-build-server-0 k3d-loopzero-build-agent-0 \
           k3d-loopzero-workload-server-0 k3d-loopzero-workload-agent-0; do
    ensure_host "$c"
  done
fi

# ---------------------------------------------------------------------------
# H5 — Bind kube-system:default SA to cluster-admin in build + runtime.
# Local k3d only — production has scoped SAs (loopzero-ctrl-*).
# ---------------------------------------------------------------------------
log "H5: ensuring kube-system:default cluster-admin in build + runtime"
ensure_admin() {
  local ctx="$1" name="$2"
  if kubectl --context "$ctx" get clusterrolebinding "$name" >/dev/null 2>&1; then
    log "  $ctx: $name already exists"
  else
    kubectl --context "$ctx" create clusterrolebinding "$name" \
      --clusterrole=cluster-admin --serviceaccount=kube-system:default 2>&1 | head -1
  fi
}
ensure_admin "$BUILD_CTX" loopzero-api-build-admin
ensure_admin "$RT_CTX" loopzero-api-runtime-admin

# ---------------------------------------------------------------------------
# H2 — Refresh loopzero-cluster-kubeconfigs Secret with current IPs.
# Uses insecure-skip-tls-verify in local k3d (cert SAN drift is expected).
# ---------------------------------------------------------------------------
log "H2: refreshing loopzero-cluster-kubeconfigs with current IPs"
mk_kcfg() {
  local server="$1" tok="$2"
  cat <<EOF
apiVersion: v1
kind: Config
clusters:
- name: in-cluster
  cluster:
    server: $server
    insecure-skip-tls-verify: true
contexts:
- name: in-cluster
  context: {cluster: in-cluster, user: in-cluster}
current-context: in-cluster
users:
- name: in-cluster
  user: {token: $tok}
EOF
}

mint_token() {
  local ctx="$1" ns="$2" sa="$3"
  kubectl --context "$ctx" -n "$ns" create token "$sa" --duration=24h 2>/dev/null
}

MGMT_TOK=$(mint_token "$MGMT_CTX" loopzero-system default)
BUILD_TOK=$(mint_token "$BUILD_CTX" kube-system default)
RT_TOK=$(mint_token "$RT_CTX" kube-system default)

if [[ -n "$MGMT_TOK" && -n "$BUILD_TOK" && -n "$RT_TOK" ]]; then
  MK=$(mk_kcfg "https://${MGMT_IP}:6443" "$MGMT_TOK")
  BK=$(mk_kcfg "https://${BUILD_IP}:6443" "$BUILD_TOK")
  RK=$(mk_kcfg "https://${RT_IP}:6443" "$RT_TOK")
  kubectl --context "$MGMT_CTX" -n loopzero-system create secret generic loopzero-cluster-kubeconfigs \
    --from-literal=mgmt.kubeconfig="$MK" \
    --from-literal=build.kubeconfig="$BK" \
    --from-literal=runtime.kubeconfig="$RK" \
    --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f - >/dev/null
  log "  loopzero-cluster-kubeconfigs Secret refreshed"
else
  warn "could not mint SA tokens — skipping H2"
fi

# ---------------------------------------------------------------------------
# H3 — Recreate ArgoCD cluster Secrets so the controller discovery cache
# fully invalidates (a `kubectl patch` is not enough — the cache key is the
# old apiServer URL; only delete+apply forces re-discovery).
# ---------------------------------------------------------------------------
log "H3: recreating ArgoCD cluster secrets (force discovery refresh)"
recreate_argocd_cluster() {
  local secret_name="$1" cluster_name="$2" server="$3" token="$4"
  local config="{\"bearerToken\":\"${token}\",\"tlsClientConfig\":{\"insecure\":true}}"
  kubectl --context "$MGMT_CTX" -n argocd delete secret "$secret_name" --ignore-not-found=true >/dev/null 2>&1
  cat <<EOF | kubectl --context "$MGMT_CTX" apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: $secret_name
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: cluster
type: Opaque
stringData:
  name: $cluster_name
  server: $server
  config: '$config'
EOF
  log "  $secret_name → $server"
}
[[ -n "$RT_TOK" ]] && recreate_argocd_cluster runtime-argocd-cluster runtime "https://${RT_IP}:6443" "$RT_TOK"
[[ -n "$BUILD_TOK" ]] && recreate_argocd_cluster build-argocd-cluster build "https://${BUILD_IP}:6443" "$BUILD_TOK"

# ---------------------------------------------------------------------------
# H6 — empty buildkit-registry-config when DOCR creds absent.
# ---------------------------------------------------------------------------
log "H6: ensuring buildkit-registry-config valid in build cluster"
existing=$(kubectl --context "$BUILD_CTX" -n argo get secret buildkit-registry-config \
  -o jsonpath='{.data.config\.json}' 2>/dev/null | base64 -d 2>/dev/null || echo '')
if [[ -z "$existing" || ! "$existing" =~ \"auths\" ]]; then
  kubectl --context "$BUILD_CTX" -n argo create secret generic buildkit-registry-config \
    --from-literal='config.json={"auths":{}}' \
    --dry-run=client -o yaml | kubectl --context "$BUILD_CTX" apply -f - >/dev/null
  log "  buildkit-registry-config → empty {\"auths\":{}}"
else
  log "  buildkit-registry-config already valid"
fi

# ---------------------------------------------------------------------------
# H7 — loopzero-minio-credentials with mgmt NodePort URL reachable from build.
# ---------------------------------------------------------------------------
log "H7: ensuring loopzero-minio-credentials in build cluster"
MINIO_AK=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret minio \
  -o jsonpath='{.data.rootUser}' 2>/dev/null | base64 -d 2>/dev/null || true)
MINIO_SK=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret minio \
  -o jsonpath='{.data.rootPassword}' 2>/dev/null | base64 -d 2>/dev/null || true)
MINIO_EP="http://${MGMT_IP}:32000"
if [[ -n "$MINIO_AK" && -n "$MINIO_SK" ]]; then
  kubectl --context "$BUILD_CTX" get ns argo >/dev/null 2>&1 || \
    kubectl --context "$BUILD_CTX" create ns argo >/dev/null
  kubectl --context "$BUILD_CTX" -n argo create secret generic loopzero-minio-credentials \
    --from-literal=AWS_ACCESS_KEY_ID="$MINIO_AK" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$MINIO_SK" \
    --from-literal=MINIO_ENDPOINT="$MINIO_EP" \
    --from-literal=AWS_DEFAULT_REGION=us-east-1 \
    --from-literal=AWS_ENDPOINT_URL="$MINIO_EP" \
    --dry-run=client -o yaml | kubectl --context "$BUILD_CTX" apply -f - >/dev/null
  log "  loopzero-minio-credentials → $MINIO_EP"
else
  warn "MinIO root creds not found in mgmt — H7 skipped"
fi

# ---------------------------------------------------------------------------
# H8 — loopzero-api Deployment env: LOOPZERO_*_IMAGE local registry overrides.
# ---------------------------------------------------------------------------
log "H8: ensuring LOOPZERO_*_IMAGE env on loopzero-api"
declare -A IMG_OVERRIDES=(
  [LOOPZERO_CLONE_IMAGE]="${REGISTRY_HOST}:5000/loopzero-clone-runner:latest"
  [LOOPZERO_PACKS_IMAGE]="${REGISTRY_HOST}:5000/loopzero-packs-runner:latest"
  [LOOPZERO_SYFT_IMAGE]="${REGISTRY_HOST}:5000/loopzero-syft-scanner:latest"
  [LOOPZERO_COSIGN_IMAGE]="${REGISTRY_HOST}:5000/loopzero-cosign-signer:latest"
)
ENV_ARGS=()
for k in "${!IMG_OVERRIDES[@]}"; do
  ENV_ARGS+=("${k}=${IMG_OVERRIDES[$k]}")
done
kubectl --context "$MGMT_CTX" -n loopzero-system set env deploy/loopzero-api "${ENV_ARGS[@]}" >/dev/null 2>&1
log "  set ${#ENV_ARGS[@]} image override env vars"

# ---------------------------------------------------------------------------
# H9 — loopzero-build-cwts release with images.cloneRunner local registry.
# ---------------------------------------------------------------------------
log "H9: ensuring loopzero-build-cwts uses local registry"
if command -v helm >/dev/null 2>&1; then
  REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
  if [[ -d "$REPO_ROOT/charts/loopzero-build-cwts" ]]; then
    helm upgrade --kube-context "$BUILD_CTX" -n argo loopzero-build-cwts \
      "$REPO_ROOT/charts/loopzero-build-cwts" \
      --install \
      --create-namespace \
      --set workflow.serviceAccount.create=false \
      --set "images.cloneRunner=${REGISTRY_HOST}:5000/loopzero-clone-runner:latest" \
      >/dev/null 2>&1 && log "  loopzero-build-cwts re-templated with local registry"
  else
    warn "charts/loopzero-build-cwts dir not found — H9 skipped"
  fi
else
  warn "helm not on PATH — H9 skipped"
fi

# ---------------------------------------------------------------------------
# H10 — tenant-controller Deployment env: WORKLOAD_CLUSTER_URL must point at
# the runtime cluster apiServer IP (current). Hardcoded YAML overlay
# `infra/helm/tenant-controller/values-standalone-multi.yaml:28` uses an IP
# that drifts every k3d restart. Patch live until permanent solution
# (operator reads ClusterCredential CR for runtime → automatic).
# Discovered LIVE 2026-04-30 — NEW-14: ArgoCD App destination went to BUILD
# cluster IP (172.18.0.10) because tenant-controller was started with the
# stale value baked into the helm overlay before the most recent restart.
# ---------------------------------------------------------------------------
log "H10: ensuring tenant-controller WORKLOAD_CLUSTER_URL points at runtime"
WORKLOAD_URL="https://${RT_IP}:6443"
TC_DEPLOY=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy -l app=tenant-controller -o name 2>/dev/null | head -1)
[[ -z "$TC_DEPLOY" ]] && TC_DEPLOY="deploy/tenant-controller"
CURRENT_URL=$(kubectl --context "$MGMT_CTX" -n loopzero-system get "$TC_DEPLOY" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="WORKLOAD_CLUSTER_URL")].value}' 2>/dev/null)
if [[ "$CURRENT_URL" == "$WORKLOAD_URL" ]]; then
  log "  $TC_DEPLOY: WORKLOAD_CLUSTER_URL already $WORKLOAD_URL"
else
  kubectl --context "$MGMT_CTX" -n loopzero-system set env "$TC_DEPLOY" \
    WORKLOAD_CLUSTER_URL="$WORKLOAD_URL" >/dev/null 2>&1
  log "  $TC_DEPLOY: $CURRENT_URL → $WORKLOAD_URL"
fi

# Roll tenant-controller so existing ArgoCD Apps get re-templated with new
# destination URL on the next reconcile. Tenant CR list is small in dev.
kubectl --context "$MGMT_CTX" -n loopzero-system rollout restart "$TC_DEPLOY" >/dev/null 2>&1
log "  $TC_DEPLOY: rolled to refresh existing Apps"

# Force tenant-controller to re-emit Application destination on existing
# tenants by triggering a reconcile via touch annotation. Use first tenant
# that has Active phase as smoke proof.
TENANTS=$(kubectl --context "$MGMT_CTX" -n loopzero-system get tenants \
  --no-headers 2>/dev/null | awk '$2=="Active" {print $1}' | head -3)
for t in $TENANTS; do
  kubectl --context "$MGMT_CTX" -n loopzero-system annotate tenant "$t" \
    loopzero.io/heal-trigger="$(date +%s)" --overwrite >/dev/null 2>&1
done
[[ -n "$TENANTS" ]] && log "  triggered reconcile on $(echo $TENANTS | wc -w) Active tenant(s)"

# Patch only TENANT ArgoCD Applications carrying a stale URL.
# CHG-2026-04-30-DELETE-SINGLE-CELL: skip apps that use destination.name
# (cell-aware multi-cell pattern). Only patch ones that still use legacy
# destination.server with stale IP. tenant-controller v0.4.1+ writes
# `name: <cellID>` for non-default cells — those resolve via ArgoCD's
# cluster registry and don't need IP patching.
TENANT_APPS=$(kubectl --context "$MGMT_CTX" -n argocd get applications \
  -l loopzero.io/managed-by=tenant-controller \
  -o jsonpath='{range .items[*]}{.metadata.name}|{.spec.destination.server}|{.spec.destination.name}{"\n"}{end}' \
  2>/dev/null)
if [[ -n "$TENANT_APPS" ]]; then
  while IFS='|' read -r app stale_url dest_name; do
    [[ -z "$app" ]] && continue
    if [[ -n "$dest_name" ]]; then
      log "  tenant app $app: uses name=$dest_name (cell-aware), skipping URL patch"
      continue
    fi
    [[ "$stale_url" == "$WORKLOAD_URL" ]] && continue
    kubectl --context "$MGMT_CTX" -n argocd patch application "$app" \
      --type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/destination/server\",\"value\":\"$WORKLOAD_URL\"}]" \
      >/dev/null 2>&1 && log "  tenant app $app: $stale_url → $WORKLOAD_URL"
  done <<< "$TENANT_APPS"
fi

# Restore build-* and runtime-* AppOfApps destination URLs from cluster
# secret server URLs (these are the in-ArgoCD secrets healed by H3).
ARGO_APPS=$(kubectl --context "$MGMT_CTX" -n argocd get applications \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null)
BUILD_URL_FROM_SECRET=$(kubectl --context "$MGMT_CTX" -n argocd get secret \
  build-argocd-cluster -o jsonpath='{.data.server}' 2>/dev/null | base64 -d 2>/dev/null)
RUNTIME_URL_FROM_SECRET=$(kubectl --context "$MGMT_CTX" -n argocd get secret \
  runtime-argocd-cluster -o jsonpath='{.data.server}' 2>/dev/null | base64 -d 2>/dev/null)
for app in $ARGO_APPS; do
  case "$app" in
    build-*)
      [[ -z "$BUILD_URL_FROM_SECRET" ]] && continue
      cur=$(kubectl --context "$MGMT_CTX" -n argocd get application "$app" \
        -o jsonpath='{.spec.destination.server}' 2>/dev/null)
      [[ "$cur" == "$BUILD_URL_FROM_SECRET" ]] && continue
      kubectl --context "$MGMT_CTX" -n argocd patch application "$app" \
        --type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/destination/server\",\"value\":\"$BUILD_URL_FROM_SECRET\"}]" \
        >/dev/null 2>&1 && log "  build app $app: $cur → $BUILD_URL_FROM_SECRET"
      ;;
    runtime-*)
      [[ -z "$RUNTIME_URL_FROM_SECRET" ]] && continue
      cur=$(kubectl --context "$MGMT_CTX" -n argocd get application "$app" \
        -o jsonpath='{.spec.destination.server}' 2>/dev/null)
      [[ "$cur" == "$RUNTIME_URL_FROM_SECRET" ]] && continue
      kubectl --context "$MGMT_CTX" -n argocd patch application "$app" \
        --type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/destination/server\",\"value\":\"$RUNTIME_URL_FROM_SECRET\"}]" \
        >/dev/null 2>&1 && log "  runtime app $app: $cur → $RUNTIME_URL_FROM_SECRET"
      ;;
  esac
done

# ---------------------------------------------------------------------------
# H11 — ArgoCD token expired. loopzero-api-tokens Secret may have expired (24h
# admin session token if provisioned with the wrong path). Detect via JWT
# exp claim and regenerate via provision-api-tokens.sh which mints from the
# `loopzero-api:apiKey` ArgoCD account (non-expiring).
# Discovered LIVE 2026-04-30 — NEW-15: project delete failed to trigger
# ArgoCD prune sync because [DEPLOY_SERVICE_AUTH_FAILED] — token had
# expired 13.8h before. Cleanup of solo-server resources never happened,
# Prova de Morte timed out 300s.
# ---------------------------------------------------------------------------
log "H11: ensuring ArgoCD token in loopzero-api-tokens Secret is valid"
TOKEN_RAW=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret loopzero-api-tokens \
  -o jsonpath='{.data.argocd-token}' 2>/dev/null | base64 -d 2>/dev/null || true)
TOKEN_VALID="false"
if [[ -n "$TOKEN_RAW" ]]; then
  PAYLOAD=$(echo "$TOKEN_RAW" | cut -d. -f2)
  PAD=$(( 4 - ${#PAYLOAD} % 4 ))
  [[ $PAD -lt 4 ]] && PAYLOAD="${PAYLOAD}$(printf '%.0s=' $(seq 1 $PAD))"
  EXP=$(echo "$PAYLOAD" | tr '_-' '/+' | base64 -d 2>/dev/null \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
  NOW=$(date +%s)
  # exp=0 means non-expiring (loopzero-api:apiKey style). Anything >300s in the
  # future is fine; under that or already past = regenerate.
  if [[ "$EXP" == "0" ]] || (( EXP > NOW + 300 )); then
    TOKEN_VALID="true"
  fi
fi
if [[ "$TOKEN_VALID" == "true" ]]; then
  log "  argocd-token already valid (exp=$EXP, now=$NOW)"
else
  log "  argocd-token invalid/expired (exp=${EXP:-missing}); regenerating via provision-api-tokens.sh"
  REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
  if [[ -x "$REPO_ROOT/infra/scripts/provision-api-tokens.sh" ]]; then
    bash "$REPO_ROOT/infra/scripts/provision-api-tokens.sh" >/dev/null 2>&1 \
      && log "  argocd-token regenerated (loopzero-api:apiKey, non-expiring)" \
      || warn "provision-api-tokens.sh failed — argocd sync calls may keep failing"
  else
    warn "provision-api-tokens.sh missing — manual regen required"
  fi
fi

# ---------------------------------------------------------------------------
# H12 — ClusterCredential cell-1 bootstrap (regression matrix entry 153).
# wire-clusters.sh has cell-1 logic but `|| log WARN: continuing` masks
# silent failures. Codify the recovery here so every `task k3d:heal`
# converges to {cell-default, cell-1, cell-2} ClusterCredentials.
# ---------------------------------------------------------------------------
log "H12: ensuring ClusterCredential cell-1 exists for loopzero-workload"
# Note: K8sCellStore uses spec.cellID (not CR name) per
# api/internal/placement/k8s_cell_store.go MEET-053 fix B9. The CR is
# named after the k3d cluster id ("workload") so scoped-kubeconfig.sh
# can resolve k3d-loopzero-workload-server-0 docker container IP. Using
# cluster-name="runtime" would fail B82 docker-resolution (container
# k3d-loopzero-runtime-server-0 doesn't exist locally).
CELL1_CONNECTED=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-1")].status.connected}' \
  2>/dev/null || echo "")
if [[ "$CELL1_CONNECTED" == "true" ]]; then
  log "  ClusterCredential with spec.cellID=cell-1 already connected — skip"
else
  REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
  CC_LIB="$REPO_ROOT/infra/helmfile/scripts/lib/clustercredential-create.sh"
  SCOPED_LIB="$REPO_ROOT/infra/helmfile/scripts/lib/scoped-kubeconfig.sh"
  WORKLOAD_KUBECONFIG=$(mktemp)
  H12_TMP="$WORKLOAD_KUBECONFIG"
  trap 'rm -f "$H12_TMP"' EXIT
  k3d kubeconfig get loopzero-workload > "$WORKLOAD_KUBECONFIG" 2>/dev/null
  if [[ -x "$CC_LIB" ]] && [[ -x "$SCOPED_LIB" ]] && [[ -s "$WORKLOAD_KUBECONFIG" ]]; then
    # CLUSTER_NAME=workload matches k3d-loopzero-workload-server-0 container
    # so scoped-kubeconfig can rewrite SERVER to docker bridge IP.
    SCOPED=$(KUBECONFIG="$WORKLOAD_KUBECONFIG" "$SCOPED_LIB" workload "$WORKLOAD_KUBECONFIG" 2>/dev/null | grep -oE '/[^[:space:]]+kube[^[:space:]]*' | head -1 || echo "")
    if [[ -n "$SCOPED" ]] && [[ -s "$SCOPED" ]]; then
      KUBECONFIG="" "$CC_LIB" cell-1 workload "$SCOPED" >/dev/null 2>&1 \
        && log "  ClusterCredential 'workload' (cellID=cell-1) created/refreshed" \
        || warn "clustercredential-create.sh cell-1 failed — see matrix entry 153 fix path (b)"
    else
      warn "scoped-kubeconfig.sh produced empty output for workload cluster"
    fi
  else
    warn "wire-clusters lib scripts missing or workload kubeconfig empty"
  fi
fi

# ---------------------------------------------------------------------------
# H13 — cell-default ClusterCredential SA token expired/rotated.
# Discovered LIVE 2026-05-01 17:40Z (regression matrix entry 154):
# loopzero-system:default SA gets recreated during certain rollouts/operations
# → SA UID changes → 24h-duration token in cell-default-kubeconfig Secret
# becomes invalid (server returns 401 "the server has asked for the client
# to provide credentials") even before its exp claim. ClusterCredential
# probe fails → connected=false → DynamicRouter loses cell-default →
# pin to cell-default would fail at deploy time.
#
# Fix: detect connected=false on cell-default, mint fresh 24h SA token via
# `kubectl create token`, patch the Secret data.kubeconfig with new token
# (preserves CA + server fields). Controller probes ~30-60s later.
# Idempotent (skips if already connected).
# ---------------------------------------------------------------------------
log "H13: ensuring ClusterCredential cell-default has valid SA token"
CELL_DEFAULT_CONNECTED=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-default")].status.connected}' \
  2>/dev/null || echo "")
if [[ "$CELL_DEFAULT_CONNECTED" == "true" ]]; then
  log "  cell-default already connected — skip"
else
  log "  cell-default disconnected — refreshing SA token"
  H13_NEW_TOKEN=$(kubectl --context "$MGMT_CTX" -n loopzero-system create token default --duration=24h 2>/dev/null || echo "")
  if [[ -z "$H13_NEW_TOKEN" ]]; then
    warn "kubectl create token loopzero-system/default failed — manual SA recovery required"
  else
    H13_CA=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret cell-default-kubeconfig \
      -o jsonpath='{.data.kubeconfig}' 2>/dev/null | base64 -d 2>/dev/null \
      | grep "certificate-authority-data:" | awk '{print $2}' || echo "")
    if [[ -z "$H13_CA" ]]; then
      warn "cell-default-kubeconfig Secret missing or malformed — cannot extract CA"
    else
      H13_KUBECONFIG_B64=$(cat <<EOF | base64 -w0
apiVersion: v1
kind: Config
clusters:
- name: in-cluster
  cluster:
    server: https://kubernetes.default.svc:443
    certificate-authority-data: $H13_CA
contexts:
- name: in-cluster
  context:
    cluster: in-cluster
    user: in-cluster
current-context: in-cluster
users:
- name: in-cluster
  user:
    token: $H13_NEW_TOKEN
EOF
      )
      kubectl --context "$MGMT_CTX" -n loopzero-system patch secret cell-default-kubeconfig \
        --type=merge -p "{\"data\":{\"kubeconfig\":\"$H13_KUBECONFIG_B64\"}}" >/dev/null 2>&1 \
        && log "  cell-default-kubeconfig Secret patched with fresh 24h SA token" \
        || warn "Secret patch failed — see matrix entry 154"
    fi
  fi
fi

# ---------------------------------------------------------------------------
# Restart loopzero-api so kubeconfigs Secret refresh takes effect (H2).
# ---------------------------------------------------------------------------
log "rolling loopzero-api so refreshed kubeconfigs take effect"
kubectl --context "$MGMT_CTX" -n loopzero-system rollout restart deploy/loopzero-api >/dev/null 2>&1
kubectl --context "$MGMT_CTX" -n loopzero-system rollout status deploy/loopzero-api --timeout=90s >/dev/null 2>&1 \
  && log "  loopzero-api Ready" \
  || warn "loopzero-api rollout did not complete cleanly"

# Also restart argocd-application-controller for the cluster secret refresh.
kubectl --context "$MGMT_CTX" -n argocd rollout restart statefulset argocd-application-controller >/dev/null 2>&1
kubectl --context "$MGMT_CTX" -n argocd rollout status statefulset argocd-application-controller --timeout=90s >/dev/null 2>&1 \
  && log "  argocd-application-controller Ready" \
  || warn "argocd controller rollout did not complete"

log "DONE — cross-cluster connectivity healed (9 hacks codified)"
