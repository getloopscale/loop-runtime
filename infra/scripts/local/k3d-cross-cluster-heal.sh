#!/usr/bin/env bash
# k3d-cross-cluster-heal.sh — idempotent recovery of cross-cluster connectivity
# in local k3d standalone-multi after `k3d cluster stop/start` cycles.
#
set -euo pipefail

# CHG-2026-04-30-DOGFOOD-LOCAL-HEAL — codifies the 9 manual workarounds (H1-H9)
# discovered LIVE during dogfood E2E unblock 2026-04-30:
#
#   H1 — k3d cluster restart drops build/runtime nodes from the shared
#        `k3d-loop-runtime` docker network. Reattach all server/agent/serverlb
#        containers via `docker network connect`.
#
#   H2 — `loop-runtime-cluster-kubeconfigs` Secret stores the peer kubeconfigs
#        loop-runtime-api uses (build, cell-0) with a CA cert + apiServer IP. After
#        restart, k3s rotates the CA AND the IP changes — Secret references
#        stale data. `loop-runtime-ops seed cluster-kubeconfigs` re-mints them with
#        the current IPs and each peer's live CA.
#
#   H3 — ArgoCD `runtime-argocd-cluster` and `build-argocd-cluster` cluster
#        secrets carry the same stale apiServer URL. Recreate from scratch
#        (delete + apply) so ArgoCD discovery cache invalidates fully.
#
#   H4 — k3d-loop-runtime-registry hostname is not in coredns of the build cluster
#        once it joined `k3d-loop-runtime-build` private net first; even after H1
#        re-attach, /etc/hosts is needed because the agent's containerd
#        does ALL its image pulls before joining the cluster network. Add
#        an explicit hostAlias entry to all node /etc/hosts.
#
#   H5 — RBAC: build & runtime clusters have `kube-system:default` SA
#        without permission to create Workflow / LoopRuntimeApplication CRs. In
#        local dev, bind that SA to cluster-admin. Production uses
#        scoped per-cluster SAs (loop-runtime-ctrl-build, loop-runtime-ctrl-runtime).
#
#   H6 — removed: the build install renders buildkit-registry-config.
#
#   H7 — `loop-runtime-minio-credentials` MINIO_ENDPOINT must point to the mgmt
#        cluster NodePort 32000 reachable on the shared k3d-loop-runtime network.
#
#   H8, H9 — removed: the install renders loop-runtime-api's attestation images
#        and loop-runtime-build-cwts' local clone runner.
#
#   H10 — tenant-controller Deployment env WORKLOAD_CLUSTER_URL drifts every
#         k3d restart (current values overlay hardcodes a stale IP). Patch
#         live + roll the controller; existing ArgoCD Apps with stale
#         destination get a one-shot patch too.
#
#   H11 — ArgoCD token in loop-runtime-api-tokens Secret expired (24h-session token
#         instead of loop-runtime-api:apiKey non-expiring). loop-runtime-api can't trigger
#         post-deploy / post-delete sync → resources orphaned in workload.
#         Detect via JWT exp claim, regenerate via provision-api-tokens.sh.
#
#   H12 — a cell (cell-0, cell-1) whose ClusterCredential is missing or
#         disconnected is re-bootstrapped through bootstrap-cell-cc.sh, the one
#         writer of a cell's scoped kubeconfig (regression matrix 153-155).
#         H13 and H14 used to rewrite the kubeconfig by hand; see H12 below.
#
#   H15 — ArgoCD `cell-1-argocd-cluster` Secret bearer token rotation.
#         Discovered LIVE 2026-05-02 (cell-1-activation-plan iter 2): the
#         tenant-controller correctly reconciles the `cell-1` ClusterCredential
#         (status.connected=true) but does NOT propagate token rotation to the
#         dependent ArgoCD cluster Secret. ArgoCD sees expired bearer token,
#         logs "configured to ignore cluster", refuses to sync apps targeting
#         destination.name=cell-1 — net effect: tenants pinned to cell-1 don't
#         get their workload synced to runtime-2. Fix: extract fresh token
#         from cell-1-kubeconfig (refreshed by H12), patch
#         cell-1-argocd-cluster `data.config` JSON with new bearerToken, then
#         restart argocd-application-controller to invalidate cluster cache.
#         Idempotent:
#         skip if ArgoCD bearerToken not yet expired (>300s buffer).
#         Future fix in tenant-controller's clustercredential reconciler:
#         propagate token to dependent ArgoCD secret on every reconcile.
#
# Idempotent — safe to re-run after every `k3d cluster start`.
#
# Pre-flight: requires kubectl + docker + k3d on PATH and four k3d
# clusters running (loop-runtime-core, loop-runtime-build, loop-runtime-cell-0, loop-runtime-cell-1).
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors


set -uo pipefail

SHARED_NET="${SHARED_NET:-k3d-loop-runtime}"
MGMT_CTX="${MGMT_CTX:-k3d-loop-runtime-core}"
BUILD_CTX="${BUILD_CTX:-k3d-loop-runtime-build}"
RT_CTX="${RT_CTX:-k3d-loop-runtime-cell-0}"
# cell-1-activation-plan T0.2 — second runtime cluster context (cell-1).
# Provisioned by infra/k3d/cell-1-cluster.yaml.
# Cluster name `loop-runtime-cell-1` matches Pulumi/wire-clusters.sh convention.
RT2_CTX="${RT2_CTX:-k3d-loop-runtime-cell-1}"
REGISTRY_HOST="${REGISTRY_HOST:-k3d-loop-runtime-registry}"

log() { printf '[k3d-heal] %s\n' "$*" >&2; }
warn() { printf '[k3d-heal] WARN: %s\n' "$*" >&2; }

# Steps whose failure leaves the platform broken. The heal keeps going so the
# other steps still run, then exits non-zero naming them.
HEAL_FAILED=()

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
# H1 — Reattach build + runtime nodes to shared k3d-loop-runtime network.
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
for c in k3d-loop-runtime-build-server-0 k3d-loop-runtime-build-agent-0 k3d-loop-runtime-build-serverlb \
         k3d-loop-runtime-cell-0-server-0 k3d-loop-runtime-cell-0-agent-0 k3d-loop-runtime-cell-0-serverlb \
         k3d-loop-runtime-cell-1-server-0 k3d-loop-runtime-cell-1-agent-0 k3d-loop-runtime-cell-1-serverlb; do
  attach_to_shared "$c"
done

# Resolve current IPs on the shared network — used by H2/H3/H4/H7
ip_on() {
  docker inspect "$1" --format \
    "{{(index .NetworkSettings.Networks \"${SHARED_NET}\").IPAddress}}" \
    2>/dev/null || true
}
MGMT_IP=$(ip_on k3d-loop-runtime-core-server-0)
BUILD_IP=$(ip_on k3d-loop-runtime-build-server-0)
RT_IP=$(ip_on k3d-loop-runtime-cell-0-server-0)
REG_IP=$(docker network inspect "$SHARED_NET" \
  --format '{{range .Containers}}{{if eq .Name "'"$REGISTRY_HOST"'"}}{{.IPv4Address}}{{end}}{{end}}' \
  2>/dev/null | sed 's|/.*||')
[[ -z "$MGMT_IP" || -z "$BUILD_IP" || -z "$RT_IP" ]] && {
  warn "cannot resolve IPs (MGMT=$MGMT_IP BUILD=$BUILD_IP RT=$RT_IP) — abort"
  exit 1
}
log "  resolved: MGMT=$MGMT_IP BUILD=$BUILD_IP RT=$RT_IP REG=$REG_IP"

# ---------------------------------------------------------------------------
# H4 — Add k3d-loop-runtime-registry to /etc/hosts on build + runtime nodes.
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
        "grep -v '[[:space:]]${REGISTRY_HOST}\$' /etc/hosts > /tmp/.loopruntimehosts 2>/dev/null; cat /tmp/.loopruntimehosts > /etc/hosts && echo '${REG_IP} ${REGISTRY_HOST}' >> /etc/hosts && rm -f /tmp/.loopruntimehosts" \
        2>/dev/null && log "  $container: added $REG_IP $REGISTRY_HOST"
    fi
  }
  # 2026-05-14 (plan T0.2): runtime-2 nodes added so helmfile sync against
  # k3d-loop-runtime-cell-1 can pull from the shared local registry (EC-1 fix).
  for c in k3d-loop-runtime-build-server-0 k3d-loop-runtime-build-agent-0 \
           k3d-loop-runtime-cell-0-server-0 k3d-loop-runtime-cell-0-agent-0 \
           k3d-loop-runtime-cell-1-server-0 k3d-loop-runtime-cell-1-agent-0; do
    ensure_host "$c"
  done
fi

# ---------------------------------------------------------------------------
# H5 — Bind kube-system:default SA to cluster-admin in build + runtime.
# Local k3d only — production has scoped SAs (loop-runtime-ctrl-*).
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
ensure_admin "$BUILD_CTX" loop-runtime-api-build-admin
ensure_admin "$RT_CTX" loop-runtime-api-runtime-admin

# ---------------------------------------------------------------------------
# H2 — Refresh the peer-cluster kubeconfigs in loop-runtime-cluster-kubeconfigs with
# current IPs, a fresh token and the peer's live CA.
#
# The minting lives in `loop-runtime-ops seed cluster-kubeconfigs` (#236), the one
# writer of those keys. This block used to mint kube-system/default tokens for
# 24h with TLS verification off, which on every heal reverted two fixes: the
# scoped build identity (argo/loop-runtime-workflow-submitter, Sprint 7 #36) and the
# 720h lifetime (#242). It also overwrote the core key with an expiring token,
# where loop-runtime-api reaches its own cluster in-cluster anyway. The live CA is read
# on every run, so a k3s CA rotation no longer needs TLS turned off.
# ---------------------------------------------------------------------------
log "H2: refreshing the peer kubeconfigs in loop-runtime-cluster-kubeconfigs (loop-runtime-ops seed cluster-kubeconfigs)"
HEAL_REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
loop_runtime_ops() {
  if command -v loop-runtime-ops >/dev/null 2>&1; then
    loop-runtime-ops "$@"
  else
    (cd "$HEAL_REPO_ROOT/cmd/loop-runtime-ops" && go run . "$@")
  fi
}
if CORE_CONTEXT="$MGMT_CTX" loop_runtime_ops seed cluster-kubeconfigs; then
  log "  loop-runtime-cluster-kubeconfigs peer keys refreshed"
else
  warn "H2 failed — loop-runtime-api cannot reach the build or cell-0 cluster until it succeeds"
  HEAL_FAILED+=("H2")
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
# H6 — REMOVED (principle_violations#243). The build install
# (taskfiles/infra.yml infra:install:build) creates buildkit-registry-config
# with config.json AND the local registry's buildkitd.toml; H6 recreated it
# with config.json alone, dropping the insecure-registry setting.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# H7 — loop-runtime-minio-credentials with mgmt NodePort URL reachable from build.
# ---------------------------------------------------------------------------
log "H7: ensuring loop-runtime-minio-credentials in build cluster"
MINIO_AK=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret minio \
  -o jsonpath='{.data.rootUser}' 2>/dev/null | base64 -d 2>/dev/null || true)
MINIO_SK=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret minio \
  -o jsonpath='{.data.rootPassword}' 2>/dev/null | base64 -d 2>/dev/null || true)
MINIO_EP="http://${MGMT_IP}:32000"
if [[ -n "$MINIO_AK" && -n "$MINIO_SK" ]]; then
  kubectl --context "$BUILD_CTX" get ns argo >/dev/null 2>&1 || \
    kubectl --context "$BUILD_CTX" create ns argo >/dev/null
  kubectl --context "$BUILD_CTX" -n argo create secret generic loop-runtime-minio-credentials \
    --from-literal=AWS_ACCESS_KEY_ID="$MINIO_AK" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$MINIO_SK" \
    --from-literal=MINIO_ENDPOINT="$MINIO_EP" \
    --from-literal=AWS_DEFAULT_REGION=us-east-1 \
    --from-literal=AWS_ENDPOINT_URL="$MINIO_EP" \
    --dry-run=client -o yaml | kubectl --context "$BUILD_CTX" apply -f - >/dev/null
  log "  loop-runtime-minio-credentials → $MINIO_EP"
else
  warn "MinIO root creds not found in mgmt — H7 skipped"
fi

# ---------------------------------------------------------------------------
# H8, H9 — REMOVED (principle_violations#243). The install renders both:
# dev-public's loop-runtime-api overlay pins LOOP_RUNTIME_COSIGN_IMAGE/LOOP_RUNTIME_SYFT_IMAGE to the
# local registry (standalone's loop-runtime-api is Tilt's; LOOP_RUNTIME_CLONE_IMAGE has no
# reader), and build.helmfile gives loop-runtime-build-cwts the local clone runner
# wherever profile.localRegistry is set. H8 overwrote the pinned tags with
# :latest and H9 upgraded the release outside Helmfile.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# H10 — tenant-operator Deployment env: WORKLOAD_CLUSTER_URL must point at
# the runtime cluster apiServer IP (current). Hardcoded YAML overlay
# `charts/loop-runtime-tenant-operator/values-standalone-multi.yaml` uses an IP
# that drifts every k3d restart. Patch live until permanent solution
# (operator reads ClusterCredential CR for runtime → automatic).
# Discovered LIVE 2026-04-30 — NEW-14: ArgoCD App destination went to BUILD
# cluster IP (172.18.0.10) because the operator was started with the
# stale value baked into the helm overlay before the most recent restart.
# ADR-0023 / plan v1.1 T1.5 update: legacy `app=tenant-controller` selector
# replaced with `app.kubernetes.io/component=loop-runtime-operator,app.kubernetes.io/name=loop-runtime-tenant-operator`
# to match the split chart (loop-runtime-tenant-operator) — the only operator that
# wires SetupTenantController and therefore needs WORKLOAD_CLUSTER_URL.
# ---------------------------------------------------------------------------
log "H10: ensuring tenant-operator WORKLOAD_CLUSTER_URL points at runtime"
WORKLOAD_URL="https://${RT_IP}:6443"
# B27 fix (real-paas-cleanroom-dogfood-loop iter 25, 2026-05-05): match
# both the split chart name (loop-runtime-tenant-operator) and the Tilt-applied
# legacy name (tenant-operator). In cleanroom only `tenant-operator`
# exists; production helmfile sync produces `loop-runtime-tenant-operator`.
# Without matching both, H10 silently no-ops in cleanroom and the operator
# keeps a stale IP / DNS-name baked into the Tilt YAML — ArgoCD App
# destination then fails with "unable to find destination server".
TC_DEPLOY=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get deploy \
  -l app.kubernetes.io/name=loop-runtime-tenant-operator -o name 2>/dev/null | head -1)
if [[ -z "$TC_DEPLOY" ]]; then
  TC_DEPLOY=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get deploy \
    -l app=tenant-operator -o name 2>/dev/null | head -1)
fi
# tenant-operator may be absent in standalone (tilt-owned, installed:false in the
# mgmt helmfile per the dual-ownership fix — tilt's manifest already sets
# WORKLOAD_CLUSTER_URL). Skip the operator env-patch + rollout when the deployment
# does not exist, so the bootstrap heal does not fail under `set -e`. The ArgoCD
# Application URL patching below still runs. Fix: finding-2026-05-22-pg-password-divergence § k3d-heal-H10.
if [[ -z "$TC_DEPLOY" ]] || ! kubectl --context "$MGMT_CTX" -n loop-runtime-system get "$TC_DEPLOY" >/dev/null 2>&1; then
  log "  tenant-operator deployment absent — skipping operator env-patch + rollout (tilt-owned in standalone)"
else
CURRENT_URL=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get "$TC_DEPLOY" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="WORKLOAD_CLUSTER_URL")].value}' 2>/dev/null)
if [[ "$CURRENT_URL" == "$WORKLOAD_URL" ]]; then
  log "  $TC_DEPLOY: WORKLOAD_CLUSTER_URL already $WORKLOAD_URL"
else
  kubectl --context "$MGMT_CTX" -n loop-runtime-system set env "$TC_DEPLOY" \
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
kubectl --context "$MGMT_CTX" -n loop-runtime-system rollout restart "$TC_DEPLOY" >/dev/null 2>&1
log "  $TC_DEPLOY: rolled, waiting for new pod ready..."
if kubectl --context "$MGMT_CTX" -n loop-runtime-system rollout status "$TC_DEPLOY" --timeout=60s >/dev/null 2>&1; then
  log "  $TC_DEPLOY: rollout complete (old pod terminated)"
else
  warn "  $TC_DEPLOY: rollout status timeout (60s) — proceeding"
fi

# Force the tenant-operator to re-emit Application destination on existing
# tenants by triggering a reconcile via touch annotation. ONLY the new pod
# (env=fresh URL) sees this, since the old pod was killed above.
TENANTS=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get tenants \
  --no-headers 2>/dev/null | awk '$2=="Active" {print $1}' | head -3)
for t in $TENANTS; do
  kubectl --context "$MGMT_CTX" -n loop-runtime-system annotate tenant "$t" \
    runtime.getloopscale.com/heal-trigger="$(date +%s)" --overwrite >/dev/null 2>&1
done
[[ -n "$TENANTS" ]] && log "  triggered reconcile on $(echo $TENANTS | wc -w) Active tenant(s)"
fi  # end tenant-operator-present guard (H10 dual-ownership fix)

# Patch only TENANT ArgoCD Applications carrying a stale URL.
# CHG-2026-04-30-DELETE-SINGLE-CELL: skip apps that use destination.name
# (cell-aware multi-cell pattern). Only patch ones that still use legacy
# destination.server with stale IP. loop-runtime-tenant-operator v0.4.1+ writes
# `name: <cellID>` for non-default cells — those resolve via ArgoCD's
# cluster registry and don't need IP patching.
# NOTE: the LABEL VALUE `tenant-controller` is the legacy-but-stable value
# written by `constants.ManagedByTenantController` in the operator code
# (preserves match with pre-existing Application objects). The BINARY that
# writes it is now `loop-runtime-tenant-operator`.
TENANT_APPS=$(kubectl --context "$MGMT_CTX" -n argocd get applications \
  -l runtime.getloopscale.com/managed-by=tenant-controller \
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
# patches were removed 2026-05-31 alongside H3. LoopRuntime-managed Applications
# (tenant + e2e dogfood) use `spec.destination.name` = cellID (e.g.
# "cell-0", "cell-0") which resolves canonically via
# `cluster-cell-*` Secrets. The legacy `build-` / `runtime-` apps were
# infra-bootstrap remnants that have been migrated to direct helmfile
# install (ADR-0008 multi-cell + 4-binary operator split).

# ---------------------------------------------------------------------------
# H11 — ArgoCD token expired. loop-runtime-api-tokens Secret may have expired (24h
# admin session token if provisioned with the wrong path). Detect via JWT
# exp claim and regenerate via provision-api-tokens.sh which mints from the
# `loop-runtime-api:apiKey` ArgoCD account (non-expiring).
# Discovered LIVE 2026-04-30 — NEW-15: project delete failed to trigger
# ArgoCD prune sync because [DEPLOY_SERVICE_AUTH_FAILED] — token had
# expired 13.8h before. Cleanup of solo-server resources never happened,
# Proof of Death timed out 300s.
# ---------------------------------------------------------------------------
log "H11: ensuring ArgoCD token in loop-runtime-api-tokens Secret is valid"
TOKEN_RAW=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret loop-runtime-api-tokens \
  -o jsonpath='{.data.argocd-token}' 2>/dev/null | base64 -d 2>/dev/null || true)
TOKEN_VALID="false"
if [[ -n "$TOKEN_RAW" ]]; then
  PAYLOAD=$(echo "$TOKEN_RAW" | cut -d. -f2)
  PAD=$(( 4 - ${#PAYLOAD} % 4 ))
  [[ $PAD -lt 4 ]] && PAYLOAD="${PAYLOAD}$(printf '%.0s=' $(seq 1 $PAD))"
  EXP=$(echo "$PAYLOAD" | tr '_-' '/+' | base64 -d 2>/dev/null \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
  NOW=$(date +%s)
  # exp=0 means non-expiring (loop-runtime-api:apiKey style). Anything >300s in the
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
      log "  argocd-token regenerated (loop-runtime-api:apiKey, non-expiring)"
    else
      warn "provision-api-tokens.sh failed — argocd sync calls may keep failing"
    fi
  else
    warn "provision-api-tokens.sh missing — manual regen required"
  fi
fi

# ---------------------------------------------------------------------------
# H12 — a cell whose ClusterCredential is missing or disconnected is
# re-bootstrapped through bootstrap-cell-cc.sh, which mints the scoped
# loop-runtime-ctrl-runtime identity, the live address and TLS posture, and labels the
# CR (regression matrix entries 153-155).
#
# H13 and H14 mode A used to rewrite the kubeconfig by hand instead: H13 with
# the core cluster's in-cluster address (its skip guard looked for the label
# `bootstrap-cell-0-cc`, which nothing writes), H14 with a kube-system/default
# token holding none of the scoped role. Both left a cell connected to /healthz
# and unable to deploy (principle_violations#243).
#
# K8sCellStore reads spec.cellID, not the CR name (MEET-053 B9).
# ---------------------------------------------------------------------------
REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
BOOTSTRAP="$REPO_ROOT/infra/helmfile/scripts/bootstrap-cell-cc.sh"
for cell in 0 1; do
  cell_id="cell-${cell}"
  log "H12: ensuring ClusterCredential ${cell_id} is connected"
  connected=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get clustercredentials \
    -o jsonpath="{.items[?(@.spec.cellID==\"${cell_id}\")].status.connected}" 2>/dev/null || echo "")
  if [[ "$connected" == "true" ]]; then
    log "  ${cell_id} already connected — skip"
    continue
  fi
  if KUBECONFIG="$(k3d kubeconfig write loop-runtime-core 2>/dev/null)" INGRESS_DOMAIN="loop-runtime.local" \
      bash "$BOOTSTRAP" --cell "$cell" --env standalone-multi >/dev/null 2>&1; then
    log "  ${cell_id} re-bootstrapped via bootstrap-cell-cc.sh"
  else
    warn "bootstrap-cell-cc.sh --cell ${cell} failed — ${cell_id} stays disconnected"
    HEAL_FAILED+=("H12:${cell_id}")
  fi
done

# ---------------------------------------------------------------------------
# H15 — ArgoCD cell-1-argocd-cluster bearer token sync (regression matrix
# entry 158 / NEW-X). Workaround for tenant-controller not propagating
# token rotation to dependent ArgoCD secrets. Reads the fresh token from
# cell-1-kubeconfig (post-H12), patches cell-1-argocd-cluster.data.config
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
    H15_FRESH_TOKEN=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret cell-1-kubeconfig \
      -o jsonpath='{.data.kubeconfig}' 2>/dev/null | base64 -d 2>/dev/null \
      | grep "token:" | awk '{print $2}' || echo "")
    if [[ -z "$H15_FRESH_TOKEN" ]]; then
      warn "  cell-1-kubeconfig missing token — H12 must run first"
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
# loop-runtime-platform-operator rewrites that Secret from CR.spec.tls.insecure on
# every reconcile loop (Secret watcher, periodic recheck, or any CC edit).
# Within minutes of H19 running, the Secret reverted to insecure:false and
# x509 came back. Fix the source of truth — flip spec.tls.insecure on the
# CR itself so the operator's next reconcile WRITES insecure:true into the
# Secret instead of overwriting our patch.
log "H19a: hardening ClusterCredential CRs (spec.tls.insecure=true — declarative source of truth)"
for H19A_CR in $(kubectl --context "$MGMT_CTX" -n loop-runtime-system get clustercredentials -o name 2>/dev/null \
  | sed 's#clustercredential.runtime.getloopscale.com/##'); do
  H19A_CURRENT=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get clustercredential "$H19A_CR" \
    -o jsonpath='{.spec.tls.insecure}' 2>/dev/null || echo "")
  if [[ "$H19A_CURRENT" == "true" ]]; then continue; fi
  if kubectl --context "$MGMT_CTX" -n loop-runtime-system patch clustercredential "$H19A_CR" \
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
# Restart loop-runtime-api so kubeconfigs Secret refresh takes effect (H2).
# ---------------------------------------------------------------------------
log "rolling loop-runtime-api so refreshed kubeconfigs take effect"
# loop-runtime-api is tilt-owned in standalone (helmfile app-tier installed:false), so the
# Deployment is absent during `task setup` (before `tilt up`). Skip when absent;
# tilt picks up the refreshed kubeconfigs Secret on its own next reconcile.
if ! kubectl --context "$MGMT_CTX" -n loop-runtime-system get deploy loop-runtime-api >/dev/null 2>&1; then
  log "  loop-runtime-api deployment absent — skipping rollout (tilt-owned in standalone)"
else
  kubectl --context "$MGMT_CTX" -n loop-runtime-system rollout restart deploy/loop-runtime-api >/dev/null 2>&1
  if kubectl --context "$MGMT_CTX" -n loop-runtime-system rollout status deploy/loop-runtime-api --timeout=90s >/dev/null 2>&1; then
    log "  loop-runtime-api Ready"
  else
    warn "loop-runtime-api rollout did not complete cleanly"
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
# T4.1 (simplify-deploy-loopzerokit-only v2.0): H16 loop-runtime-packs-runner +
# loop-runtime-dockerfile-lint registry seeding removed — those images are no longer
# used (canonical in-process Dockerfile generation replaced loop-runtime-packs; the
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
  # Probe loop-runtime-api pod readiness before attempting kubectl exec.
  if kubectl --context "$MGMT_CTX" -n loop-runtime-system get pod -l app=loop-runtime-api --no-headers 2>/dev/null | grep -q Running; then
    if CORE_CONTEXT="$MGMT_CTX" bash "$SCRIPT_DIR/run-migrations.sh" >/dev/null 2>&1; then
      log "  schema_migrations: backfilled / current"
    else
      warn "  run-migrations.sh failed — DB migrations may need manual attention"
    fi
  else
    log "  loop-runtime-api pod not Running yet — skip H17 (will heal on next pass)"
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

if (( ${#HEAL_FAILED[@]} > 0 )); then
  warn "FAILED steps: ${HEAL_FAILED[*]} — cross-cluster connectivity is NOT healed"
  exit 1
fi
log "DONE — cross-cluster connectivity healed (H1-H18 codified)"
