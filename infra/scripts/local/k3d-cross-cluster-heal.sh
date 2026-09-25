#!/usr/bin/env bash
# k3d-cross-cluster-heal.sh — idempotent recovery of cross-cluster connectivity
# in local k3d standalone-multi after `k3d cluster stop/start` cycles.
#
set -euo pipefail

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
#        (the local Docker registry). The Tilt-owned install takes them from
#        `infra/tilt/loopzero-api-values.yaml`; this script applies them as a
#        runtime patch in case that render is stale.
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
#   H13 — cell-0 ClusterCredential SA token rotated/invalidated.
#         Discovered LIVE 2026-05-01 17:40Z (regression matrix entry 154):
#         loopzero-system:default SA recreation during certain rollouts → SA UID
#         change → cached 24h token in cell-0-kubeconfig Secret returns
#         401 "the server has asked for the client to provide credentials".
#         Detection: connected=false. Fix: mint fresh `kubectl create token`,
#         patch Secret data.kubeconfig (refreshes CA from kube-root-ca.crt + server URL).
#
#   H12 — ClusterCredential cell-0 missing despite cells SQL row status=active.
#         Discovered LIVE 2026-05-01 (regression matrix entry 153): cells
#         table seed creates 3 rows (cell-0, cell-0, cell-1) but
#         wire-clusters.sh non-fatal "WARN: continuing" path silently failed
#         on cell-0 → DynamicRouter.Resolve("cell-0") = unknown cell. Pin
#         para cell-0 atualizaria DB + CR, mas operator deploy falharia.
#         Fix: invoke clustercredential-create.sh directly using the workload
#         cluster scoped kubeconfig. Idempotent (kubectl apply).
#
#   H15 — ArgoCD `cell-1-argocd-cluster` Secret bearer token rotation.
#         Discovered LIVE 2026-05-02 (cell-1-activation-plan iter 2): the
#         tenant-controller correctly reconciles the `cell-1` ClusterCredential
#         (status.connected=true) but does NOT propagate token rotation to the
#         dependent ArgoCD cluster Secret. ArgoCD sees expired bearer token,
#         logs "configured to ignore cluster", refuses to sync apps targeting
#         destination.name=cell-1 — net effect: tenants pinned to cell-1 don't
#         get their workload synced to runtime-2. Fix: extract fresh token
#         from cell-1-kubeconfig (refreshed by H14), patch
#         cell-1-argocd-cluster `data.config` JSON with new bearerToken, then
#         restart argocd-application-controller to invalidate cluster cache.
#         Symmetric to H14 mode A but for the ArgoCD secret. Idempotent:
#         skip if ArgoCD bearerToken not yet expired (>300s buffer).
#         Future fix in tenant-controller's clustercredential reconciler:
#         propagate token to dependent ArgoCD secret on every reconcile.
#
#   H14 — ClusterCredential cell-1 SA token refresh OR bootstrap for loopzero-cell-1.
#         The cell-1 CR may already exist (named `cell-1`, spec.clusterName=
#         loopzero-cell-1) but its kubeconfig Secret carries a stale SA token
#         after a runtime-2 rollout — same H13 pattern, different cluster.
#         If CR is connected: skip. If disconnected (typically because of an
#         SA UID rotation in runtime-2): mint fresh token in runtime-2,
#         rebuild kubeconfig YAML preserving CA + server URL, patch the
#         cell-1-kubeconfig Secret. If CR doesn't exist at all: bootstrap
#         via clustercredential-create.sh cell-1 runtime-2 <scoped> (cluster_name
#         passed to scoped-kubeconfig is `runtime-2` so docker container
#         k3d-loopzero-cell-1-server-0 resolves correctly per B82). Without
#         H14, `loopzero_cell_discovery_count` stays at 2 locally (cell-0
#         + cell-0 only) and Stage 1 canary preflight fails. K8sCellStore
#         reads spec.cellID (NOT CR name) per MEET-053 B9. Idempotent: skip
#         if connected. Regression matrix entry 155.
#
# Idempotent — safe to re-run after every `k3d cluster start`.
#
# Pre-flight: requires kubectl + docker + k3d on PATH and four k3d
# clusters running (loopzero-core, loopzero-build, loopzero-cell-0, loopzero-cell-1).
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors


set -uo pipefail

SHARED_NET="${SHARED_NET:-k3d-loopzero}"
MGMT_CTX="${MGMT_CTX:-k3d-loopzero-core}"
BUILD_CTX="${BUILD_CTX:-k3d-loopzero-build}"
RT_CTX="${RT_CTX:-k3d-loopzero-cell-0}"
# cell-1-activation-plan T0.2 — second runtime cluster context (cell-1).
# Provisioned by infra/k3d/cell-1-cluster.yaml. Used by H14 below.
# Cluster name `loopzero-cell-1` matches Pulumi/wire-clusters.sh convention.
RT2_CTX="${RT2_CTX:-k3d-loopzero-cell-1}"
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

for ctx in "$MGMT_CTX" "$BUILD_CTX" "$RT_CTX" "$RT2_CTX"; do
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
         k3d-loopzero-cell-0-server-0 k3d-loopzero-cell-0-agent-0 k3d-loopzero-cell-0-serverlb \
         k3d-loopzero-cell-1-server-0 k3d-loopzero-cell-1-agent-0 k3d-loopzero-cell-1-serverlb; do
  attach_to_shared "$c"
done

# Resolve current IPs on the shared network — used by H2/H3/H4/H7
ip_on() {
  docker inspect "$1" --format \
    "{{(index .NetworkSettings.Networks \"${SHARED_NET}\").IPAddress}}" \
    2>/dev/null || true
}
MGMT_IP=$(ip_on k3d-loopzero-core-server-0)
BUILD_IP=$(ip_on k3d-loopzero-build-server-0)
RT_IP=$(ip_on k3d-loopzero-cell-0-server-0)
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
      # NOTE: docker bind-mounts /etc/hosts, so `sed -i` (write-temp + rename)
      # fails with "can't move ...: Resource busy". Use cat-truncate (in-place
      # write, no rename) to dedup, then append. Fix: finding-2026-05-22-pg-password-divergence § k3d-heal-H4.
      docker exec "$container" sh -c \
        "grep -v '[[:space:]]${REGISTRY_HOST}\$' /etc/hosts > /tmp/.loopzerohosts 2>/dev/null; cat /tmp/.loopzerohosts > /etc/hosts && echo '${REG_IP} ${REGISTRY_HOST}' >> /etc/hosts && rm -f /tmp/.loopzerohosts" \
        2>/dev/null && log "  $container: added $REG_IP $REGISTRY_HOST"
    fi
  }
  # 2026-05-14 (plan T0.2): runtime-2 nodes added so helmfile sync against
  # k3d-loopzero-cell-1 can pull from the shared local registry (EC-1 fix).
  for c in k3d-loopzero-build-server-0 k3d-loopzero-build-agent-0 \
           k3d-loopzero-cell-0-server-0 k3d-loopzero-cell-0-agent-0 \
           k3d-loopzero-cell-1-server-0 k3d-loopzero-cell-1-agent-0; do
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
    --from-literal=core.kubeconfig="$MK" \
    --from-literal=build.kubeconfig="$BK" \
    --from-literal=cell-0.kubeconfig="$RK" \
    --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f - >/dev/null
  log "  loopzero-cluster-kubeconfigs Secret refreshed"
else
  warn "could not mint SA tokens — skipping H2"
fi

# ---------------------------------------------------------------------------
# H3 — REMOVED 2026-05-31 (V9-CLEANROOM-FRESH-FAANG-FIX, sponsor-direct).
# ---------------------------------------------------------------------------
# Historical context: H3 used to create `runtime-argocd-cluster` and
# `build-argocd-cluster` Secrets pointing at IPs that ALREADY map to the
# canonical `cluster-cell-0` / `cluster-cell-0` Secrets that the
# ClusterCredential controller emits. The duplication caused ArgoCD
# application-controller to register 2-3 informers per server URL; on
# shard re-assignment one informer always ended up orphan, leaving the
# controller pod with cancelled watches and a 1.2GB allocation loop
# (`Watch failed err="context canceled"` log spam, no Reconciliation
# completed messages, all Applications stuck status.sync=""). LIVE-validated
# 2026-05-31 cleanroom-fresh dogfood — Apps reconcile cleanly after the
# duplicates are removed.
#
# Canonical source of truth post-ADR-0008 multi-cell:
#   - cluster-cell-0      (managed-by=clustercredential-controller)
#   - cluster-cell-0            (managed-by=clustercredential-controller)
#   - cluster-cell-1            (managed-by=clustercredential-controller)
#
# H3 cleanup of stale Secrets is preserved here (idempotent — runs on every
# heal invocation so prior installs auto-migrate).
log "H3: removing legacy duplicate ArgoCD cluster secrets (V9-FAANG cleanup)"
for stale in runtime-argocd-cluster build-argocd-cluster cluster-workload cluster-runtime-2; do
  kubectl --context "$MGMT_CTX" -n argocd delete secret "$stale" --ignore-not-found=true >/dev/null 2>&1 && log "  deleted $stale"
done

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
# loopzero-api is tilt-owned in standalone (helmfile app-tier installed:false), so
# the Deployment does not exist during `task setup` (before `tilt up`). Skip the
# env-patch when absent — tilt's api-deployment.yaml already wires LOOPZERO_*_IMAGE.
if ! kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api >/dev/null 2>&1; then
  log "  loopzero-api deployment absent — skipping LOOPZERO_*_IMAGE env-patch (tilt-owned in standalone)"
else
  declare -A IMG_OVERRIDES=(
    [LOOPZERO_CLONE_IMAGE]="${REGISTRY_HOST}:5000/loopzero-clone-runner:latest"
    # T4.1: LOOPZERO_PACKS_IMAGE removed — loopzero-api no longer uses loopzero-packs.
    [LOOPZERO_SYFT_IMAGE]="${REGISTRY_HOST}:5000/loopzero-syft-scanner:latest"
    [LOOPZERO_COSIGN_IMAGE]="${REGISTRY_HOST}:5000/loopzero-cosign-signer:latest"
  )
  ENV_ARGS=()
  for k in "${!IMG_OVERRIDES[@]}"; do
    ENV_ARGS+=("${k}=${IMG_OVERRIDES[$k]}")
  done
  kubectl --context "$MGMT_CTX" -n loopzero-system set env deploy/loopzero-api "${ENV_ARGS[@]}" >/dev/null 2>&1
  log "  set ${#ENV_ARGS[@]} image override env vars"
fi

# ---------------------------------------------------------------------------
# H9 — loopzero-build-cwts release with images.cloneRunner local registry.
# ---------------------------------------------------------------------------
log "H9: ensuring loopzero-build-cwts uses local registry"
if command -v helm >/dev/null 2>&1; then
  REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
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
# H10 — tenant-operator Deployment env: WORKLOAD_CLUSTER_URL must point at
# the runtime cluster apiServer IP (current). Hardcoded YAML overlay
# `charts/loopzero-tenant-operator/values-standalone-multi.yaml` uses an IP
# that drifts every k3d restart. Patch live until permanent solution
# (operator reads ClusterCredential CR for runtime → automatic).
# Discovered LIVE 2026-04-30 — NEW-14: ArgoCD App destination went to BUILD
# cluster IP (172.18.0.10) because the operator was started with the
# stale value baked into the helm overlay before the most recent restart.
# ADR-0023 / plan v1.1 T1.5 update: legacy `app=tenant-controller` selector
# replaced with `app.kubernetes.io/component=loopzero-operator,app.kubernetes.io/name=loopzero-tenant-operator`
# to match the split chart (loopzero-tenant-operator) — the only operator that
# wires SetupTenantController and therefore needs WORKLOAD_CLUSTER_URL.
# ---------------------------------------------------------------------------
log "H10: ensuring tenant-operator WORKLOAD_CLUSTER_URL points at runtime"
WORKLOAD_URL="https://${RT_IP}:6443"
# B27 fix (real-paas-cleanroom-dogfood-loop iter 25, 2026-05-05): match
# both the split chart name (loopzero-tenant-operator) and the Tilt-applied
# legacy name (tenant-operator). In cleanroom only `tenant-operator`
# exists; production helmfile sync produces `loopzero-tenant-operator`.
# Without matching both, H10 silently no-ops in cleanroom and the operator
# keeps a stale IP / DNS-name baked into the Tilt YAML — ArgoCD App
# destination then fails with "unable to find destination server".
TC_DEPLOY=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy \
  -l app.kubernetes.io/name=loopzero-tenant-operator -o name 2>/dev/null | head -1)
if [[ -z "$TC_DEPLOY" ]]; then
  TC_DEPLOY=$(kubectl --context "$MGMT_CTX" -n loopzero-system get deploy \
    -l app=tenant-operator -o name 2>/dev/null | head -1)
fi
# tenant-operator may be absent in standalone (tilt-owned, installed:false in the
# mgmt helmfile per the dual-ownership fix — tilt's manifest already sets
# WORKLOAD_CLUSTER_URL). Skip the operator env-patch + rollout when the deployment
# does not exist, so the bootstrap heal does not fail under `set -e`. The ArgoCD
# Application URL patching below still runs. Fix: finding-2026-05-22-pg-password-divergence § k3d-heal-H10.
if [[ -z "$TC_DEPLOY" ]] || ! kubectl --context "$MGMT_CTX" -n loopzero-system get "$TC_DEPLOY" >/dev/null 2>&1; then
  log "  tenant-operator deployment absent — skipping operator env-patch + rollout (tilt-owned in standalone)"
else
CURRENT_URL=$(kubectl --context "$MGMT_CTX" -n loopzero-system get "$TC_DEPLOY" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="WORKLOAD_CLUSTER_URL")].value}' 2>/dev/null)
if [[ "$CURRENT_URL" == "$WORKLOAD_URL" ]]; then
  log "  $TC_DEPLOY: WORKLOAD_CLUSTER_URL already $WORKLOAD_URL"
else
  kubectl --context "$MGMT_CTX" -n loopzero-system set env "$TC_DEPLOY" \
    WORKLOAD_CLUSTER_URL="$WORKLOAD_URL" >/dev/null 2>&1
  log "  $TC_DEPLOY: $CURRENT_URL → $WORKLOAD_URL"
fi

# Roll the tenant-operator so existing ArgoCD Apps get re-templated with new
# destination URL on the next reconcile. Tenant CR list is small in dev.
#
# CHG-2026-05-05-DOGFOOD-B39: WAIT for the rollout to FINISH before
# annotating tenants. Without this wait, the OLD pod (still running with
# stale env=hostname during graceful shutdown) receives the annotation
# event and reconciles BEFORE termination, overwriting the Application
# destination with the stale URL. The new pod's later reconcile (after
# init) then sees no spec drift and never re-emits the correct URL.
# Discovered LIVE in real-paas-cleanroom-dogfood-loop iter 29:
# Application stuck at hostname despite H10 patch + new pod with IP env.
kubectl --context "$MGMT_CTX" -n loopzero-system rollout restart "$TC_DEPLOY" >/dev/null 2>&1
log "  $TC_DEPLOY: rolled, waiting for new pod ready..."
if kubectl --context "$MGMT_CTX" -n loopzero-system rollout status "$TC_DEPLOY" --timeout=60s >/dev/null 2>&1; then
  log "  $TC_DEPLOY: rollout complete (old pod terminated)"
else
  warn "  $TC_DEPLOY: rollout status timeout (60s) — proceeding"
fi

# Force the tenant-operator to re-emit Application destination on existing
# tenants by triggering a reconcile via touch annotation. ONLY the new pod
# (env=fresh URL) sees this, since the old pod was killed above.
TENANTS=$(kubectl --context "$MGMT_CTX" -n loopzero-system get tenants \
  --no-headers 2>/dev/null | awk '$2=="Active" {print $1}' | head -3)
for t in $TENANTS; do
  kubectl --context "$MGMT_CTX" -n loopzero-system annotate tenant "$t" \
    loopzero.io/heal-trigger="$(date +%s)" --overwrite >/dev/null 2>&1
done
[[ -n "$TENANTS" ]] && log "  triggered reconcile on $(echo $TENANTS | wc -w) Active tenant(s)"
fi  # end tenant-operator-present guard (H10 dual-ownership fix)

# Patch only TENANT ArgoCD Applications carrying a stale URL.
# CHG-2026-04-30-DELETE-SINGLE-CELL: skip apps that use destination.name
# (cell-aware multi-cell pattern). Only patch ones that still use legacy
# destination.server with stale IP. loopzero-tenant-operator v0.4.1+ writes
# `name: <cellID>` for non-default cells — those resolve via ArgoCD's
# cluster registry and don't need IP patching.
# NOTE: the LABEL VALUE `tenant-controller` is the legacy-but-stable value
# written by `constants.ManagedByTenantController` in the operator code
# (preserves match with pre-existing Application objects). The BINARY that
# writes it is now `loopzero-tenant-operator`.
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

# NOTE: Legacy AppOfApps `build-*` / `runtime-*` Application destination
# patches were removed 2026-05-31 alongside H3. LoopZero-managed Applications
# (tenant + e2e dogfood) use `spec.destination.name` = cellID (e.g.
# "cell-0", "cell-0") which resolves canonically via
# `cluster-cell-*` Secrets. The legacy `build-` / `runtime-` apps were
# infra-bootstrap remnants that have been migrated to direct helmfile
# install (ADR-0008 multi-cell + 4-binary operator split).

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
  REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
  if [[ -x "$REPO_ROOT/infra/scripts/production/provision-api-tokens.sh" ]]; then
    if bash "$REPO_ROOT/infra/scripts/production/provision-api-tokens.sh" >/dev/null 2>&1; then
      log "  argocd-token regenerated (loopzero-api:apiKey, non-expiring)"
    else
      warn "provision-api-tokens.sh failed — argocd sync calls may keep failing"
    fi
  else
    warn "provision-api-tokens.sh missing — manual regen required"
  fi
fi

# ---------------------------------------------------------------------------
# H12 — ClusterCredential cell-0 bootstrap (regression matrix entry 153).
# wire-clusters.sh has cell-0 logic but `|| log WARN: continuing` masks
# silent failures. Codify the recovery here so every `task k3d:heal`
# converges to {cell-0, cell-1} ClusterCredentials.
# ---------------------------------------------------------------------------
log "H12: ensuring ClusterCredential cell-0 exists for loopzero-cell-0"
# Note: K8sCellStore uses spec.cellID (not CR name) per
# api/internal/placement/k8s_cell_store.go MEET-053 fix B9. The CR is
# named after the k3d cluster id ("workload") so scoped-kubeconfig.sh
# can resolve k3d-loopzero-cell-0-server-0 docker container IP. Using
# cluster-name="runtime" would fail B82 docker-resolution (container
# k3d-loopzero-cell-0-server-0 doesn't exist locally).
CELL0_CONNECTED=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-0")].status.connected}' \
  2>/dev/null || echo "")
# cell-1-activation-plan T1.3 / ADR C7: ensure ingress-domain label exists
# on cell-0 CR even when already connected. Idempotent label patch.
CELL0_CR_NAME=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-0")].metadata.name}' \
  2>/dev/null || echo "")
if [[ -n "$CELL0_CR_NAME" ]]; then
  CELL0_DOMAIN=$(kubectl --context "$MGMT_CTX" -n loopzero-system get clustercredential "$CELL0_CR_NAME" \
    -o jsonpath='{.metadata.labels.loopzero\.io/ingress-domain}' 2>/dev/null || echo "")
  if [[ "$CELL0_DOMAIN" != "loopzero.local" ]]; then
    kubectl --context "$MGMT_CTX" -n loopzero-system label clustercredential "$CELL0_CR_NAME" \
      loopzero.io/ingress-domain=loopzero.local --overwrite >/dev/null 2>&1 \
      && log "  cell-0 CR=$CELL0_CR_NAME labeled with ingress-domain=loopzero.local (C7)"
  fi
fi
if [[ "$CELL0_CONNECTED" == "true" ]]; then
  log "  ClusterCredential with spec.cellID=cell-0 already connected — skip"
else
  # V9-CLEANROOM-FRESH-FAANG-FIX 2026-05-31: invoke the ADR-0008 canonical
  # bootstrap-cell-cc.sh (CR name = cell-0) instead of the legacy
  # clustercredential-create.sh that named the CR after CLUSTER_NAME=workload.
  # Legacy path produced duplicate ArgoCD cluster Secrets pointing at the
  # same apiServer URL, triggering controller informer thrash and L22-class
  # "Ignoring cluster" loops.
  REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
  BOOTSTRAP="$REPO_ROOT/infra/helmfile/scripts/bootstrap-cell-cc.sh"
  if [[ -x "$BOOTSTRAP" ]]; then
    if KUBECONFIG="$(k3d kubeconfig write loopzero-core 2>/dev/null)" \
      INGRESS_DOMAIN="loopzero.local" \
      bash "$BOOTSTRAP" --cell 0 --env standalone-multi >/dev/null 2>&1; then
      log "  ClusterCredential cell-0 (CR name = cellID) created/refreshed via bootstrap-cell-cc.sh"
    else
      warn "bootstrap-cell-cc.sh --cell 0 failed — see matrix entry 153 fix path (b)"
    fi
  else
    warn "bootstrap-cell-cc.sh missing — cannot heal H12"
  fi
fi

# ---------------------------------------------------------------------------
# H13 — cell-0 ClusterCredential SA token expired/rotated.
# Discovered LIVE 2026-05-01 17:40Z (regression matrix entry 154):
# loopzero-system:default SA gets recreated during certain rollouts/operations
# → SA UID changes → 24h-duration token in cell-0-kubeconfig Secret
# becomes invalid (server returns 401 "the server has asked for the client
# to provide credentials") even before its exp claim. ClusterCredential
# probe fails → connected=false → DynamicRouter loses cell-0 →
# pin to cell-0 would fail at deploy time.
#
# Fix: detect connected=false on cell-0, mint fresh 24h SA token via
# `kubectl create token`, patch the Secret data.kubeconfig with new token
# (refreshes CA from live kube-root-ca.crt + server). Controller probes ~30-60s later.
# Idempotent (skips if already connected).
# ---------------------------------------------------------------------------
log "H13: ensuring ClusterCredential cell-0 has valid SA token"
CELL_DEFAULT_CONNECTED=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-0")].status.connected}' \
  2>/dev/null || echo "")
# B29 fix (real-paas-cleanroom-dogfood-loop iter 27, 2026-05-05):
# bootstrap-cell-0-cc.sh produces a kubeconfig that points at
# loopzero-cell-0 (cluster name `workload`) with a `loopzero-ctrl-runtime`
# SA token in the workload cluster — correct for standalone-multi
# where cell-0 is colocated with workload. H13's design assumes
# cell-0 points at mgmt cluster; if H13 patches the kubeconfig
# with a mgmt SA token + `https://kubernetes.default.svc:443` URL, it
# breaks the standalone-multi setup. Skip H13 when the CR carries the
# bootstrap-cell-0-cc managed-by label.
H13_MANAGED_BY=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredential cell-0 \
  -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' \
  2>/dev/null || echo "")
if [[ "$H13_MANAGED_BY" == "bootstrap-cell-0-cc" ]]; then
  log "  cell-0 managed by bootstrap-cell-0-cc — skip H13 (workload-targeted)"
elif [[ "$CELL_DEFAULT_CONNECTED" == "true" ]]; then
  log "  cell-0 already connected — skip"
else
  log "  cell-0 disconnected — refreshing SA token"
  H13_NEW_TOKEN=$(kubectl --context "$MGMT_CTX" -n loopzero-system create token default --duration=24h 2>/dev/null || echo "")
  if [[ -z "$H13_NEW_TOKEN" ]]; then
    warn "kubectl create token loopzero-system/default failed — manual SA recovery required"
  else
    # Source the CA from the LIVE in-cluster kube-root-ca.crt ConfigMap — this is
    # the authoritative CA that signs the kubernetes.default.svc serving cert.
    # k3s rotates its server CA across cluster restarts, so the CA cached in the
    # Secret goes stale (different `k3s-server-ca@<ts>` generation) and the
    # controller's TLS probe fails with "x509: certificate signed by unknown
    # authority" even with a fresh SA token. Refresh the CA from the ConfigMap;
    # fall back to the Secret's CA only if the ConfigMap is unreadable.
    H13_CA=$(kubectl --context "$MGMT_CTX" -n loopzero-system get configmap kube-root-ca.crt \
      -o jsonpath='{.data.ca\.crt}' 2>/dev/null | base64 -w0 2>/dev/null || echo "")
    if [[ -z "$H13_CA" ]]; then
      H13_CA=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret cell-0-kubeconfig \
        -o jsonpath='{.data.kubeconfig}' 2>/dev/null | base64 -d 2>/dev/null \
        | grep "certificate-authority-data:" | awk '{print $2}' || echo "")
    fi
    if [[ -z "$H13_CA" ]]; then
      warn "cell-0 CA unavailable (kube-root-ca.crt ConfigMap + Secret both missing)"
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
      if kubectl --context "$MGMT_CTX" -n loopzero-system patch secret cell-0-kubeconfig \
        --type=merge -p "{\"data\":{\"kubeconfig\":\"$H13_KUBECONFIG_B64\"}}" >/dev/null 2>&1; then
        log "  cell-0-kubeconfig Secret patched with fresh 24h SA token"
      else
        warn "Secret patch failed — see matrix entry 154"
      fi
    fi
  fi
fi

# ---------------------------------------------------------------------------
# H14 — ClusterCredential cell-1 SA token refresh OR bootstrap (regression
# matrix entry 155). Two modes:
#
#   Mode A (refresh) — CR `cell-1` exists with spec.clusterName=loopzero-cell-1
#     but status.connected=false because SA UID in loopzero-cell-1 rotated
#     (same trap as H13 for cell-0). Mint fresh 24h SA token via
#     `kubectl --context k3d-loopzero-cell-1 create token default` and patch
#     cell-1-kubeconfig Secret data.kubeconfig preserving CA + apiServer.
#
#   Mode B (bootstrap) — CR doesn't exist at all (e.g., fresh workstation
#     without runtime-2 wire-up). Generate scoped kubeconfig with cluster_name
#     ID `runtime-2` so docker container k3d-loopzero-cell-1-server-0 resolves
#     correctly per B82, then invoke clustercredential-create.sh cell-1
#     runtime-2 <scoped>. INGRESS_DOMAIN=loopzero.local for ADR C7.
#
# Idempotent: skip if connected. Non-fatal: WARN on failure so heal exits 0.
# ---------------------------------------------------------------------------
log "H14: ensuring ClusterCredential cell-1 connected (loopzero-cell-1)"
CELL2_CONNECTED=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
  get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-1")].status.connected}' \
  2>/dev/null || echo "")

if [[ "$CELL2_CONNECTED" == "true" ]]; then
  log "  ClusterCredential with spec.cellID=cell-1 already connected — skip"
else
  CELL2_CR_EXISTS=$(kubectl --context "$MGMT_CTX" -n loopzero-system \
    get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-1")].metadata.name}' \
    2>/dev/null || echo "")

  if [[ -n "$CELL2_CR_EXISTS" ]]; then
    # Mode A — refresh SA token (H13 pattern applied to cell-1).
    log "  cell-1 CR=$CELL2_CR_EXISTS exists but disconnected — refreshing SA token in $RT2_CTX"
    if ! kubectl --context "$RT2_CTX" --request-timeout=5s cluster-info >/dev/null 2>&1; then
      warn "  $RT2_CTX unreachable — H14 mode A cannot mint token (start runtime-2 cluster first)"
    else
      H14_NEW_TOKEN=$(kubectl --context "$RT2_CTX" -n kube-system create token default --duration=24h 2>/dev/null || echo "")
      if [[ -z "$H14_NEW_TOKEN" ]]; then
        warn "  kubectl create token kube-system/default in $RT2_CTX failed — manual SA recovery required"
      else
        # Extract apiServer URL (docker bridge IP, NOT 0.0.0.0 host port).
        H14_APISERVER=$(kubectl --context "$MGMT_CTX" -n loopzero-system get clustercredential cell-1 \
          -o jsonpath='{.spec.apiServer}' 2>/dev/null || echo "")
        # Detect TLS mode from existing Secret. Two valid forms:
        #   (a) certificate-authority-data: <base64-PEM>  (production strict TLS)
        #   (b) insecure-skip-tls-verify: true            (local k3d, ADR-0015 dev path)
        H14_KC_DECODED=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret cell-1-kubeconfig \
          -o jsonpath='{.data.kubeconfig}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
        H14_CA=$(echo "$H14_KC_DECODED" | grep "certificate-authority-data:" | head -1 | awk '{print $2}' || echo "")
        H14_INSECURE=$(echo "$H14_KC_DECODED" | grep -q "insecure-skip-tls-verify:" && echo "true" || echo "")

        if [[ -z "$H14_APISERVER" ]]; then
          warn "  cell-1 spec.apiServer empty — cannot rebuild kubeconfig"
        else
          # Build TLS line based on detected mode.
          if [[ -n "$H14_CA" ]]; then
            H14_TLS_LINE="    certificate-authority-data: $H14_CA"
          elif [[ -n "$H14_INSECURE" ]]; then
            H14_TLS_LINE="    insecure-skip-tls-verify: true"
          else
            warn "  cell-1-kubeconfig Secret has neither CA data nor insecure-skip-tls-verify"
            H14_TLS_LINE=""
          fi

          if [[ -n "$H14_TLS_LINE" ]]; then
            H14_KUBECONFIG_B64=$(cat <<EOF | base64 -w0
apiVersion: v1
kind: Config
clusters:
- name: loopzero-cell-1
  cluster:
    server: $H14_APISERVER
$H14_TLS_LINE
contexts:
- name: loopzero-cell-1
  context:
    cluster: loopzero-cell-1
    user: loopzero-cell-1
current-context: loopzero-cell-1
users:
- name: loopzero-cell-1
  user:
    token: $H14_NEW_TOKEN
EOF
            )
            if kubectl --context "$MGMT_CTX" -n loopzero-system patch secret cell-1-kubeconfig \
              --type=merge -p "{\"data\":{\"kubeconfig\":\"$H14_KUBECONFIG_B64\"}}" >/dev/null 2>&1; then
              log "  cell-1-kubeconfig Secret patched with fresh 24h SA token (TLS: ${H14_CA:+strict}${H14_INSECURE:+insecure})"
            else
              warn "  Secret patch failed — see matrix entry 155 fix path (a) refresh"
            fi
          fi
        fi
      fi
    fi
  else
    # Mode B — bootstrap fresh CR via ADR-0008 canonical script.
    # V9-CLEANROOM-FRESH-FAANG-FIX 2026-05-31: invoke bootstrap-cell-cc.sh
    # so CR name = cell-1 (not runtime-2). Legacy `clustercredential-create.sh
    # cell-1 runtime-2` produced a duplicate cluster Secret pointing at the
    # same apiServer URL as the canonical `cell-1` CC, triggering
    # application-controller informer thrash (see H3 + wire-clusters.sh).
    log "  cell-1 CR absent — bootstrapping via bootstrap-cell-cc.sh"
    REPO_ROOT_H14=$(cd "$(dirname "$0")/../../.." && pwd)
    BOOTSTRAP_H14="$REPO_ROOT_H14/infra/helmfile/scripts/bootstrap-cell-cc.sh"
    if [[ -x "$BOOTSTRAP_H14" ]]; then
      if KUBECONFIG="$(k3d kubeconfig write loopzero-core 2>/dev/null)" \
        INGRESS_DOMAIN="loopzero.local" \
        bash "$BOOTSTRAP_H14" --cell 1 --env standalone-multi >/dev/null 2>&1; then
        log "  ClusterCredential cell-1 (CR name = cellID) created/refreshed via bootstrap-cell-cc.sh"
      else
        warn "  bootstrap-cell-cc.sh --cell 1 failed — see matrix entry 155 fix path (b)"
      fi
    else
      warn "  bootstrap-cell-cc.sh missing — cannot heal H14 mode B (runtime-2 cluster running?)"
    fi
  fi
fi

# ---------------------------------------------------------------------------
# H15 — ArgoCD cell-1-argocd-cluster bearer token sync (regression matrix
# entry 158 / NEW-X). Workaround for tenant-controller not propagating
# token rotation to dependent ArgoCD secrets. Reads the fresh token from
# cell-1-kubeconfig (post-H14), patches cell-1-argocd-cluster.data.config
# JSON with new bearerToken if expired/expiring soon. Idempotent: skip
# if current ArgoCD token has > 300s remaining.
# ---------------------------------------------------------------------------
log "H15: ensuring ArgoCD cell-1-argocd-cluster bearer token is fresh"
H15_ARGO_SECRET_EXISTS=$(kubectl --context "$MGMT_CTX" -n argocd get secret cell-1-argocd-cluster --no-headers --ignore-not-found 2>/dev/null | wc -l)
if [[ "$H15_ARGO_SECRET_EXISTS" -eq 0 ]]; then
  log "  cell-1-argocd-cluster Secret not present — skip (CC controller bootstraps via spec.scope.argocd)"
else
  H15_CURRENT_TOKEN=$(kubectl --context "$MGMT_CTX" -n argocd get secret cell-1-argocd-cluster \
    -o jsonpath='{.data.config}' 2>/dev/null | base64 -d 2>/dev/null \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("bearerToken",""))' 2>/dev/null || echo "")
  H15_EXP=0
  if [[ -n "$H15_CURRENT_TOKEN" ]]; then
    H15_PAYLOAD=$(echo "$H15_CURRENT_TOKEN" | cut -d. -f2)
    H15_PAD=$(( 4 - ${#H15_PAYLOAD} % 4 ))
    [[ $H15_PAD -lt 4 ]] && H15_PAYLOAD="${H15_PAYLOAD}$(printf '=%.0s' $(seq 1 $H15_PAD))"
    H15_EXP=$(echo "$H15_PAYLOAD" | tr '_-' '/+' | base64 -d 2>/dev/null \
      | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
  fi
  H15_NOW=$(date +%s)
  if (( H15_EXP > H15_NOW + 300 )); then
    log "  cell-1-argocd-cluster token still valid (exp=$H15_EXP, ${H15_EXP}s; ~$(( (H15_EXP - H15_NOW) / 60 ))min remaining) — skip"
  else
    log "  cell-1-argocd-cluster token expired/expiring (exp=$H15_EXP, now=$H15_NOW) — patching"
    H15_FRESH_TOKEN=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret cell-1-kubeconfig \
      -o jsonpath='{.data.kubeconfig}' 2>/dev/null | base64 -d 2>/dev/null \
      | grep "token:" | awk '{print $2}' || echo "")
    if [[ -z "$H15_FRESH_TOKEN" ]]; then
      warn "  cell-1-kubeconfig missing token — H14 must run first"
    else
      H15_NEW_CONFIG=$(printf '{"bearerToken":"%s","tlsClientConfig":{"insecure":true}}' "$H15_FRESH_TOKEN")
      H15_NEW_CONFIG_B64=$(echo -n "$H15_NEW_CONFIG" | base64 -w0)
      if kubectl --context "$MGMT_CTX" -n argocd patch secret cell-1-argocd-cluster \
        --type=merge -p "{\"data\":{\"config\":\"$H15_NEW_CONFIG_B64\"}}" >/dev/null 2>&1; then
        log "  cell-1-argocd-cluster Secret patched with fresh token (matches cell-1-kubeconfig)"
      else
        warn "  ArgoCD secret patch failed — see matrix entry 158"
      fi
    fi
  fi
fi

# ---------------------------------------------------------------------------
# H19 — ArgoCD cell cluster Secret CA hardening (stale-CA-proof).
# Bootstrap-created ArgoCD cluster Secrets (e.g. cluster-cell-0) pin a
# tlsClientConfig.caData snapshot. k3s rotates its server CA across cluster
# restarts, so that caData goes stale and ArgoCD fails to sync with
# "Failed to load live state: ... x509: certificate signed by unknown
# authority" → Application sync=Unknown → tenant workloads never deploy on
# that cell (deploy reports release ACTIVE but no pod). The heal-registered
# clusters (recreate_argocd_cluster, H3) already use insecure:true; bootstrap
# ones don't. Flip any cell cluster Secret that still carries caData to
# tlsClientConfig.insecure:true (same local-k3d posture as H3) so a rotated
# CA can't wedge GitOps. Discovered LIVE 2026-05-25 (dogfood Journey 05
# staging HTTP 200 timeout root-caused to cluster-cell-0 stale CA).
# Issue #67 (2026-05-25): the previous H19 patched only the rendered Secret
# in argocd namespace, but the ClusterCredential reconciler in
# loopzero-platform-operator rewrites that Secret from CR.spec.tls.insecure on
# every reconcile loop (Secret watcher, periodic recheck, or any CC edit).
# Within minutes of H19 running, the Secret reverted to insecure:false and
# x509 came back. Fix the source of truth — flip spec.tls.insecure on the
# CR itself so the operator's next reconcile WRITES insecure:true into the
# Secret instead of overwriting our patch.
log "H19a: hardening ClusterCredential CRs (spec.tls.insecure=true — declarative source of truth)"
for H19A_CR in $(kubectl --context "$MGMT_CTX" -n loopzero-system get clustercredentials -o name 2>/dev/null \
  | sed 's#clustercredential.loopzero.io/##'); do
  H19A_CURRENT=$(kubectl --context "$MGMT_CTX" -n loopzero-system get clustercredential "$H19A_CR" \
    -o jsonpath='{.spec.tls.insecure}' 2>/dev/null || echo "")
  if [[ "$H19A_CURRENT" == "true" ]]; then continue; fi
  if kubectl --context "$MGMT_CTX" -n loopzero-system patch clustercredential "$H19A_CR" \
    --type=merge -p '{"spec":{"tls":{"insecure":true}}}' >/dev/null 2>&1; then
    log "  $H19A_CR → spec.tls.insecure=true"
  else
    warn "  $H19A_CR patch failed"
  fi
done

log "H19: hardening ArgoCD cell cluster Secrets against stale CA (insecure for local k3d)"
H19_PATCHED=0
for H19_SECRET in $(kubectl --context "$MGMT_CTX" -n argocd get secrets \
  -l argocd.argoproj.io/secret-type=cluster -o name 2>/dev/null \
  | sed 's#secret/##' | grep -E '^cluster-cell-'); do
  H19_INSECURE=$(kubectl --context "$MGMT_CTX" -n argocd get secret "$H19_SECRET" \
    -o jsonpath='{.data.config}' 2>/dev/null | base64 -d 2>/dev/null \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("tlsClientConfig",{}).get("insecure",False))' 2>/dev/null || echo "True")
  if [[ "$H19_INSECURE" == "True" ]]; then continue; fi
  H19_NEW=$(kubectl --context "$MGMT_CTX" -n argocd get secret "$H19_SECRET" \
    -o jsonpath='{.data.config}' 2>/dev/null | base64 -d 2>/dev/null \
    | python3 -c 'import sys,json; c=json.load(sys.stdin); c["tlsClientConfig"]={"insecure":True}; print(json.dumps(c))' 2>/dev/null || echo "")
  if [[ -z "$H19_NEW" ]]; then continue; fi
  H19_NEW_B64=$(printf '%s' "$H19_NEW" | base64 -w0)
  if kubectl --context "$MGMT_CTX" -n argocd patch secret "$H19_SECRET" \
    --type=json -p "[{\"op\":\"replace\",\"path\":\"/data/config\",\"value\":\"$H19_NEW_B64\"}]" >/dev/null 2>&1; then
    { log "  $H19_SECRET → tlsClientConfig.insecure=true (stale caData dropped)"; H19_PATCHED=1; }
  else
    warn "  $H19_SECRET patch failed"
  fi
done
if [[ "$H19_PATCHED" -eq 1 ]]; then
  if kubectl --context "$MGMT_CTX" -n argocd rollout restart statefulset/argocd-application-controller >/dev/null 2>&1; then
    log "  argocd-application-controller restarted to reload cluster Secrets"
  fi
fi

# ---------------------------------------------------------------------------
# Restart loopzero-api so kubeconfigs Secret refresh takes effect (H2).
# ---------------------------------------------------------------------------
log "rolling loopzero-api so refreshed kubeconfigs take effect"
# loopzero-api is tilt-owned in standalone (helmfile app-tier installed:false), so the
# Deployment is absent during `task setup` (before `tilt up`). Skip when absent;
# tilt picks up the refreshed kubeconfigs Secret on its own next reconcile.
if ! kubectl --context "$MGMT_CTX" -n loopzero-system get deploy loopzero-api >/dev/null 2>&1; then
  log "  loopzero-api deployment absent — skipping rollout (tilt-owned in standalone)"
else
  kubectl --context "$MGMT_CTX" -n loopzero-system rollout restart deploy/loopzero-api >/dev/null 2>&1
  if kubectl --context "$MGMT_CTX" -n loopzero-system rollout status deploy/loopzero-api --timeout=90s >/dev/null 2>&1; then
    log "  loopzero-api Ready"
  else
    warn "loopzero-api rollout did not complete cleanly"
  fi
fi

# Also restart argocd-application-controller for the cluster secret refresh.
kubectl --context "$MGMT_CTX" -n argocd rollout restart statefulset argocd-application-controller >/dev/null 2>&1
if kubectl --context "$MGMT_CTX" -n argocd rollout status statefulset argocd-application-controller --timeout=90s >/dev/null 2>&1; then
  log "  argocd-application-controller Ready"
else
  warn "argocd controller rollout did not complete"
fi


# ---------------------------------------------------------------------------
# T4.1 (simplify-deploy-loopzerokit-only v2.0): H16 loopzero-packs-runner +
# loopzero-dockerfile-lint registry seeding removed — those images are no longer
# used (canonical in-process Dockerfile generation replaced loopzero-packs; the
# BYO-Dockerfile lint path was deleted). SCRIPT_DIR retained for H17+ below.
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ---------------------------------------------------------------------------
# H17: dogfood-2026-05-08-fixes T1.3 (2026-05-08).
# Backfill schema_migrations on existing local DBs that pre-date golang-migrate
# adoption. Pre-T1.2 the legacy `task db:migrate` ran psql in a loop and never
# created schema_migrations. Pre-flight check `SELECT FROM schema_migrations`
# fails on cleanroom rerun without this heal.
#
# Logic (delegated to infra/scripts/production/run-migrations.sh):
#   - if schema_migrations absent AND tenants present:
#       # allowlist: migrate force (heal recovery — register existing schema, not prod-pod usage)
#       migrate force <HIGHEST> → register existing schema as fully applied
#   - migrate up → apply any genuinely new migrations
# Idempotent: re-runs are no-ops when schema_migrations is current.
# ---------------------------------------------------------------------------
if [[ -x "$SCRIPT_DIR/run-migrations.sh" ]]; then
  log "H17: ensuring schema_migrations table is populated"
  # Probe loopzero-api pod readiness before attempting kubectl exec.
  if kubectl --context "$MGMT_CTX" -n loopzero-system get pod -l app=loopzero-api --no-headers 2>/dev/null | grep -q Running; then
    if CORE_CONTEXT="$MGMT_CTX" bash "$SCRIPT_DIR/run-migrations.sh" >/dev/null 2>&1; then
      log "  schema_migrations: backfilled / current"
    else
      warn "  run-migrations.sh failed — DB migrations may need manual attention"
    fi
  else
    log "  loopzero-api pod not Running yet — skip H17 (will heal on next pass)"
  fi
fi

# ---------------------------------------------------------------------------
# H18: dogfood-2026-05-08-fixes follow-up (2026-05-08).
# Remove duplicate ArgoCD Gitea credentials. provision-api-tokens.sh creates
# `argocd-creds-gitea` (canonical, repo-creds label, current token), but a
# pre-existing Helm chart leaves behind `gitea-credentials-template` with a
# stale token at the SAME url prefix. ArgoCD logs:
#   "Found multiple credentials for repoURL: ...e2e-tenant-gitops"
# and intermittently picks the stale one → "authentication required" 401.
# Idempotent: no-op if the duplicate Secret is already gone.
# ---------------------------------------------------------------------------
log "H18: removing duplicate stale Gitea credentials in ArgoCD namespace"
H18_REMOVED=false
if kubectl --context "$MGMT_CTX" -n argocd get secret gitea-credentials-template >/dev/null 2>&1; then
  kubectl --context "$MGMT_CTX" -n argocd delete secret gitea-credentials-template >/dev/null 2>&1
  log "  gitea-credentials-template: removed (duplicate of argocd-creds-gitea)"
  H18_REMOVED=true
else
  log "  gitea-credentials-template not present — skip"
fi
# argocd-sync-cleanroom-resilience-plan T1.2 (ADR D2 + F-02 of dogfood
# 2026-05-08): the ArgoCD Helm chart auto-creates `argocd-repo-gitea-internal`
# (label argocd.argoproj.io/secret-type=repository, only `type=git` + `url`
# fields, NO password). It coexists with `argocd-creds-gitea` (canonical
# repo-creds with password) for the SAME url prefix, causing
# "Found multiple credentials for repoURL" warnings + non-deterministic
# auth failures. Delete it idempotently.
if kubectl --context "$MGMT_CTX" -n argocd get secret argocd-repo-gitea-internal >/dev/null 2>&1; then
  kubectl --context "$MGMT_CTX" -n argocd delete secret argocd-repo-gitea-internal >/dev/null 2>&1
  log "  argocd-repo-gitea-internal: removed (incomplete stub conflicting with argocd-creds-gitea)"
  H18_REMOVED=true
else
  log "  argocd-repo-gitea-internal not present — skip"
fi
if [[ "$H18_REMOVED" == "true" ]]; then
  # Force argocd-repo-server to drop cached creds.
  kubectl --context "$MGMT_CTX" -n argocd rollout restart deploy/argocd-repo-server >/dev/null 2>&1
  log "  argocd-repo-server: restarted to invalidate cred cache"
fi

log "DONE — cross-cluster connectivity healed (H1-H18 codified)"
