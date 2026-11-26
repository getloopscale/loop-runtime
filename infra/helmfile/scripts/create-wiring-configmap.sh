#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T1.4
# Creates the loopruntime-platform-topology ConfigMap consumed by loopruntime-api via envFrom.
# Replaces the Pulumi `wiring` object exported by the old index.ts.
#
# Usage: create-wiring-configmap.sh <env>
#
# Required env vars (from `pulumi stack output`):
#   LOOPRUNTIME_DB_HOST, LOOPRUNTIME_DB_PORT, LOOPRUNTIME_DB_USER, LOOPRUNTIME_DB_NAME
#   LOOPRUNTIME_REGISTRY_ENDPOINT, LOOPRUNTIME_REGISTRY_NAME
#   LOOPRUNTIME_BUCKET_NAME, LOOPRUNTIME_BUCKET_ENDPOINT
#   LOOPRUNTIME_CF_KV_NAMESPACE
# Optional (computed via kubectl if absent):
#   LOOPRUNTIME_MGMT_INGRESS_IP, LOOPRUNTIME_RUNTIME_INGRESS_IP
set -euo pipefail
ENV="${1:?usage: $0 <env>}"

# Issue #117 (generalizing #115) — pin the target cluster BY CONSTRUCTION.
# The loopruntime-platform-topology ConfigMap is consumed by loopruntime-api via envFrom, so
# it belongs to MGMT. Without this pin the script wrote it to whatever
# kubecontext was active: on a dev box carrying residue (e.g. k3d-loopruntime-build
# left over from an earlier bootstrap step) loopruntime-api silently never sees its
# topology. The kubectl() override makes every bare call downstream carry the
# right --context. Override KUBECTL_CONTEXT for non-k3d targets (DOKS prod).
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopruntime-core}"
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

log() { printf '[wiring-configmap] %s\n' "$*" >&2; }

# Internal in-cluster URLs (constants from K8s service-DNS naming convention).
GITEA_INTERNAL_URL="${LOOPRUNTIME_GITEA_INTERNAL_URL:-http://gitea-http.loopruntime-system.svc.cluster.local:3000}"
GRAFANA_INTERNAL_URL="${LOOPRUNTIME_GRAFANA_INTERNAL_URL:-http://grafana.monitoring.svc.cluster.local:3000}"
PROMETHEUS_INTERNAL_URL="${LOOPRUNTIME_PROMETHEUS_INTERNAL_URL:-http://prometheus-server.monitoring.svc.cluster.local:80}"

# Resolve ingress IPs from kubectl if not provided.
if [[ -z "${LOOPRUNTIME_MGMT_INGRESS_IP:-}" ]]; then
  LOOPRUNTIME_MGMT_INGRESS_IP=$(kubectl get svc traefik -n traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
fi
if [[ -z "${LOOPRUNTIME_RUNTIME_INGRESS_IP:-}" ]]; then
  LOOPRUNTIME_RUNTIME_INGRESS_IP="${LOOPRUNTIME_MGMT_INGRESS_IP}"  # default to mgmt for standalone
fi

DOMAIN="${LOOPRUNTIME_DOMAIN:-dev.getloopscale.com}"

# Build the ConfigMap manifest.
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: loopruntime-platform-topology
  namespace: loopruntime-system
  labels:
    app.kubernetes.io/managed-by: loopruntime-platform
    runtime.getloopscale.com/component: wiring
data:
  # Internal service-DNS URLs (constants — match K8s naming convention).
  GITEA_INTERNAL_URL: "${GITEA_INTERNAL_URL}"
  GRAFANA_INTERNAL_URL: "${GRAFANA_INTERNAL_URL}"
  PROMETHEUS_INTERNAL_URL: "${PROMETHEUS_INTERNAL_URL}"

  # Public URLs (derived from domain).
  GITEA_PUBLIC_URL: "https://git.${DOMAIN}"
  GRAFANA_PUBLIC_URL: "https://grafana.${DOMAIN}"
  ARGOCD_PUBLIC_URL: "https://argocd.${DOMAIN}"
  LOOPRUNTIME_API_PUBLIC_URL: "https://api.${DOMAIN}"
  LOOPRUNTIME_CLOUD_PUBLIC_URL: "https://app.${DOMAIN}"

  # Database (from Pulumi L1).
  DB_HOST: "${LOOPRUNTIME_DB_HOST:-}"
  DB_PORT: "${LOOPRUNTIME_DB_PORT:-5432}"
  DB_USER: "${LOOPRUNTIME_DB_USER:-loopruntime}"
  DB_NAME: "${LOOPRUNTIME_DB_NAME:-loopruntime}"

  # Registry (from Pulumi L1).
  REGISTRY_ENDPOINT: "${LOOPRUNTIME_REGISTRY_ENDPOINT:-}"
  REGISTRY_NAME: "${LOOPRUNTIME_REGISTRY_NAME:-}"

  # Storage (from Pulumi L1).
  STORAGE_BUCKET: "${LOOPRUNTIME_BUCKET_NAME:-}"
  STORAGE_ENDPOINT: "${LOOPRUNTIME_BUCKET_ENDPOINT:-}"

  # Cloudflare (from Pulumi L1).
  CF_KV_NAMESPACE: "${LOOPRUNTIME_CF_KV_NAMESPACE:-}"

  # Ingress IPs (resolved post-Traefik install).
  MGMT_INGRESS_IP: "${LOOPRUNTIME_MGMT_INGRESS_IP:-}"
  RUNTIME_INGRESS_IP: "${LOOPRUNTIME_RUNTIME_INGRESS_IP:-}"

  # Environment metadata.
  ENVIRONMENT: "${ENV}"
  DOMAIN: "${DOMAIN}"
EOF
log "loopruntime-platform-topology ConfigMap applied (env=$ENV)"
