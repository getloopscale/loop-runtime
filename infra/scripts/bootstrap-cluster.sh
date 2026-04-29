#!/usr/bin/env bash
# end-to-end-deploy-validation-plan T1.1/T2.1
# Idempotent k3d cluster bootstrap for mgmt|build|workload roles.
#
# Usage:
#   bash infra/scripts/bootstrap-cluster.sh <role>
#
# Where <role> is one of: mgmt, build, workload.
# Creates the cluster (if not exists), seeds required namespaces,
# applies DO Registry pull secret in target namespaces.
set -euo pipefail

ROLE="${1:?usage: $0 <mgmt|build|workload>}"

case "$ROLE" in
  mgmt|build|workload) ;;
  *)
    echo "ERROR: invalid role '$ROLE' (must be mgmt|build|workload)" >&2
    exit 1
    ;;
esac

CLUSTER_NAME="loopzero-${ROLE}"
CONFIG="infra/k3d/${ROLE}-cluster.yaml"
CONTEXT="k3d-${CLUSTER_NAME}"

log() { printf '[bootstrap-cluster:%s] %s\n' "$ROLE" "$*" >&2; }

# 1. Create or detect cluster
if k3d cluster list -o json 2>/dev/null | grep -q "\"name\":\"${CLUSTER_NAME}\""; then
  log "cluster ${CLUSTER_NAME} already exists, skipping create"
else
  log "creating cluster ${CLUSTER_NAME} from ${CONFIG}..."
  if [[ ! -f "$CONFIG" ]]; then
    log "ERROR: config file ${CONFIG} not found"
    exit 1
  fi
  # Ensure shared registry exists (used by all 3 clusters)
  if ! k3d registry list -o json 2>/dev/null | grep -q "\"name\":\"k3d-loopzero-registry\""; then
    log "creating shared k3d-loopzero-registry..."
    k3d registry create loopzero-registry --port 5000 || true
  fi
  k3d cluster create --config "$CONFIG"
fi

# 2. Define namespaces per role
case "$ROLE" in
  mgmt)
    NAMESPACES="loopzero-system loopzero-auth argocd monitoring traefik cert-manager nfs-system"
    PULL_SECRET_NAMESPACES="loopzero-system"
    ;;
  build)
    NAMESPACES="argo monitoring"
    PULL_SECRET_NAMESPACES="argo"
    ;;
  workload)
    NAMESPACES="loopzero-system loopzero-tenants traefik cert-manager monitoring cloudflared cosign-system"
    PULL_SECRET_NAMESPACES="loopzero-system loopzero-tenants"
    ;;
esac

for ns in $NAMESPACES; do
  if kubectl --context "$CONTEXT" get namespace "$ns" >/dev/null 2>&1; then
    log "namespace ${ns} exists"
  else
    log "creating namespace ${ns}..."
    kubectl --context "$CONTEXT" create namespace "$ns"
  fi
done

# 3. DO Registry pull secret (requires `doctl auth` already done locally)
if command -v doctl >/dev/null 2>&1 && doctl auth list 2>&1 | grep -qE "current|default"; then
  log "applying DO Registry pull secret to namespaces: ${PULL_SECRET_NAMESPACES}..."
  doctl registry login >/dev/null 2>&1 || true
  for ns in $PULL_SECRET_NAMESPACES; do
    doctl registry kubernetes-manifest --namespace "$ns" --name loopzero-registry-pull 2>&1 \
      | kubectl --context "$CONTEXT" apply -f - >/dev/null
    log "  ✓ loopzero-registry-pull in ${ns}"
  done
else
  log "WARN: doctl not authenticated — pull secret skipped (will fail on image pull)"
fi

# 4. Memory check
AVAIL_GB=$(free -g | awk '/^Mem:/ { print $7 }')
log "✓ cluster ${CLUSTER_NAME} ready (memory: ${AVAIL_GB}GB free)"
