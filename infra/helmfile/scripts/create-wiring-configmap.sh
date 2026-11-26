#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T1.4
# Creates the loop-runtime-platform-topology ConfigMap consumed by loop-runtime-api via envFrom.
# Replaces the Pulumi `wiring` object exported by the old index.ts.
#
# Usage: create-wiring-configmap.sh <env>
#
# Required env vars (from `pulumi stack output`):
#   LOOP_RUNTIME_DB_HOST, LOOP_RUNTIME_DB_PORT, LOOP_RUNTIME_DB_USER, LOOP_RUNTIME_DB_NAME
#   LOOP_RUNTIME_REGISTRY_ENDPOINT, LOOP_RUNTIME_REGISTRY_NAME
#   LOOP_RUNTIME_BUCKET_NAME, LOOP_RUNTIME_BUCKET_ENDPOINT
#   LOOP_RUNTIME_CF_KV_NAMESPACE
# Optional (computed via kubectl if absent):
#   LOOP_RUNTIME_MGMT_INGRESS_IP, LOOP_RUNTIME_RUNTIME_INGRESS_IP
set -euo pipefail
ENV="${1:?usage: $0 <env>}"

# Issue #117 (generalizing #115) — pin the target cluster BY CONSTRUCTION.
# The loop-runtime-platform-topology ConfigMap is consumed by loop-runtime-api via envFrom, so
# it belongs to MGMT. Without this pin the script wrote it to whatever
# kubecontext was active: on a dev box carrying residue (e.g. k3d-loop-runtime-build
# left over from an earlier bootstrap step) loop-runtime-api silently never sees its
# topology. The kubectl() override makes every bare call downstream carry the
# right --context. Override KUBECTL_CONTEXT for non-k3d targets (DOKS prod).
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loop-runtime-core}"
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

log() { printf '[wiring-configmap] %s\n' "$*" >&2; }

# Internal in-cluster URLs (constants from K8s service-DNS naming convention).
GITEA_INTERNAL_URL="${LOOP_RUNTIME_GITEA_INTERNAL_URL:-http://gitea-http.loop-runtime-system.svc.cluster.local:3000}"
GRAFANA_INTERNAL_URL="${LOOP_RUNTIME_GRAFANA_INTERNAL_URL:-http://grafana.monitoring.svc.cluster.local:3000}"
PROMETHEUS_INTERNAL_URL="${LOOP_RUNTIME_PROMETHEUS_INTERNAL_URL:-http://prometheus-server.monitoring.svc.cluster.local:80}"

# Resolve ingress IPs from kubectl if not provided.
if [[ -z "${LOOP_RUNTIME_MGMT_INGRESS_IP:-}" ]]; then
  LOOP_RUNTIME_MGMT_INGRESS_IP=$(kubectl get svc traefik -n traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
fi
if [[ -z "${LOOP_RUNTIME_RUNTIME_INGRESS_IP:-}" ]]; then
  LOOP_RUNTIME_RUNTIME_INGRESS_IP="${LOOP_RUNTIME_MGMT_INGRESS_IP}"  # default to mgmt for standalone
fi

DOMAIN="${LOOP_RUNTIME_DOMAIN:-dev.getloopscale.com}"

# Build the ConfigMap manifest.
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: loop-runtime-platform-topology
  namespace: loop-runtime-system
  labels:
    app.kubernetes.io/managed-by: loop-runtime-platform
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
  LOOP_RUNTIME_API_PUBLIC_URL: "https://api.${DOMAIN}"
  LOOP_CONSOLE_PUBLIC_URL: "https://app.${DOMAIN}"

  # Database (from Pulumi L1).
  DB_HOST: "${LOOP_RUNTIME_DB_HOST:-}"
  DB_PORT: "${LOOP_RUNTIME_DB_PORT:-5432}"
  DB_USER: "${LOOP_RUNTIME_DB_USER:-loop_runtime}"
  DB_NAME: "${LOOP_RUNTIME_DB_NAME:-loop_runtime}"

  # Registry (from Pulumi L1).
  REGISTRY_ENDPOINT: "${LOOP_RUNTIME_REGISTRY_ENDPOINT:-}"
  REGISTRY_NAME: "${LOOP_RUNTIME_REGISTRY_NAME:-}"

  # Storage (from Pulumi L1).
  STORAGE_BUCKET: "${LOOP_RUNTIME_BUCKET_NAME:-}"
  STORAGE_ENDPOINT: "${LOOP_RUNTIME_BUCKET_ENDPOINT:-}"

  # Cloudflare (from Pulumi L1).
  CF_KV_NAMESPACE: "${LOOP_RUNTIME_CF_KV_NAMESPACE:-}"

  # Ingress IPs (resolved post-Traefik install).
  MGMT_INGRESS_IP: "${LOOP_RUNTIME_MGMT_INGRESS_IP:-}"
  RUNTIME_INGRESS_IP: "${LOOP_RUNTIME_RUNTIME_INGRESS_IP:-}"

  # Environment metadata.
  ENVIRONMENT: "${ENV}"
  DOMAIN: "${DOMAIN}"
EOF
log "loop-runtime-platform-topology ConfigMap applied (env=$ENV)"
