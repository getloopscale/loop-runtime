#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T4.1
# Health gates: validates that a cluster role is healthy AFTER helmfile sync.
# Each check retries 30x with 10s interval (5min total).
#
# Usage:
#   health-check.sh <role> [domain]
#     role:   mgmt | build | runtime | wire
#     domain: optional, defaults to env LOOPZERO_DOMAIN or dev.loopzero.example
set -euo pipefail
ROLE="${1:?usage: $0 <role> [domain]}"
DOMAIN="${2:-${LOOPZERO_DOMAIN:-dev.loopzero.example}}"
PROFILE="${LOOPZERO_PROFILE:-${HELMFILE_ENVIRONMENT:-}}"

MAX_RETRIES="${HEALTH_CHECK_RETRIES:-30}"
RETRY_INTERVAL="${HEALTH_CHECK_INTERVAL_SEC:-10}"

log() { printf '[health-check %s] %s\n' "$ROLE" "$*" >&2; }

is_standalone() { [[ "$PROFILE" == standalone* ]]; }

retry() {
  local label="$1"; shift
  local cmd="$*"
  for i in $(seq 1 "$MAX_RETRIES"); do
    if eval "$cmd" >/dev/null 2>&1; then
      log "PASS  $label (attempt $i)"
      return 0
    fi
    sleep "$RETRY_INTERVAL"
  done
  log "FAIL  $label (after $MAX_RETRIES attempts)"
  return 1
}

case "$ROLE" in
  infra)
    retry "cluster reachable" "kubectl cluster-info --request-timeout=5s"
    if [[ -n "${LOOPZERO_DB_HOST:-}" ]]; then
      retry "PostgreSQL reachable" "pg_isready -h $LOOPZERO_DB_HOST -p ${LOOPZERO_DB_PORT:-5432} -U ${LOOPZERO_DB_USER:-loopzero} -t 5"
    fi
    ;;

  mgmt)
    if is_standalone; then
      # In standalone profiles, public DNS doesn't resolve. Use in-cluster service URLs.
      retry "Gitea API"   "kubectl exec -n loopzero-system deploy/gitea -- curl -sf -m 5 http://localhost:3000/api/v1/version 2>/dev/null || curl -sf -m 5 http://\$(kubectl get svc gitea-http -n loopzero-system -o jsonpath='{.spec.clusterIP}'):3000/api/v1/version"
      retry "ArgoCD API"  "kubectl get deploy -n argocd argocd-server -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
      retry "LoopZero API"    "kubectl get deploy -n loopzero-system loopzero-api -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
    else
      retry "Gitea API"   "curl -sf -m 5 https://git.${DOMAIN}/api/v1/version"
      retry "ArgoCD API"  "curl -sfk -m 5 https://argocd.${DOMAIN}/api/version"
      retry "LoopZero API"    "curl -sf -m 5 https://api.${DOMAIN}/api/v1/health"
    fi
    retry "MinIO ready"   "kubectl get pod -n loopzero-system -l app=minio -o jsonpath='{.items[*].status.phase}' | grep -q Running"
    if ! is_standalone; then
      retry "Harbor ready" "kubectl get pod -n loopzero-system -l app=harbor -o jsonpath='{.items[*].status.phase}' | grep -q Running"
    fi
    ;;

  build)
    if is_standalone; then
      # In k3d, use ClusterIP instead of LoadBalancer
      retry "Argo Workflows ready" "kubectl get deploy -n argo argo-workflows-server -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
    else
      retry "Argo Workflows API"   "curl -sf -m 5 http://\$(kubectl get svc -n argo argo-workflows-server -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):2746/api/v1/info"
    fi
    retry "Loki ready"         "kubectl get pod -n monitoring -l app.kubernetes.io/name=loki -o jsonpath='{.items[*].status.phase}' | grep -q Running"
    retry "Cosign Secret"      "kubectl get secret cosign-signing-key -n argo"
    retry "B1 — Cosign not placeholder" "[ \"\$(kubectl get secret cosign-signing-key -n argo -o jsonpath='{.data.cosign\\.key}' | base64 -d 2>/dev/null | grep -c 'PLACEHOLDER')\" = '0' ]"
    retry "B2 — local-path StorageClass" "kubectl get sc local-path -o jsonpath='{.provisioner}' | grep -q rancher.io/local-path"
    ;;

  runtime)
    if is_standalone; then
      retry "Traefik ready" "kubectl get deploy -n traefik traefik -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
    else
      retry "Traefik LoadBalancer IP" "kubectl get svc -n traefik traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' | grep -E '^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$'"
    fi
    retry "KEDA operator ready"     "kubectl get deploy -n keda keda-operator -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
    retry "policy-controller ready" "kubectl get deploy -n cosign-system policy-controller -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
    retry "B16 — ClusterImagePolicy exists" "[ \"\$(kubectl get clusterimagepolicy -o name 2>/dev/null | wc -l)\" -ge '1' ]"
    if ! is_standalone; then
      retry "cloudflared ready"     "kubectl get deploy -n loopzero-system cloudflared -o jsonpath='{.status.readyReplicas}' | grep -qE '^[1-9]'"
    else
      log "SKIP  cloudflared (standalone profile — not deployed)"
    fi
    ;;

  wire)
    retry "ArgoCD lists registered clusters" \
      "kubectl get secret -n argocd -l argocd.argoproj.io/secret-type=cluster --no-headers | grep -E '^(build|runtime)' "
    # B17 — repo-server cache: detect 'cached' staleness and restart if found
    if kubectl logs -n argocd deploy/argocd-repo-server --tail=200 2>/dev/null | grep -q "(cached)"; then
      log "B17 — repo-server cache stale detected, restarting"
      kubectl rollout restart deploy/argocd-repo-server -n argocd >/dev/null
      retry "ArgoCD repo-server restarted" \
        "kubectl rollout status deploy/argocd-repo-server -n argocd --timeout=120s"
    fi
    ;;

  *)
    log "ERROR: unknown role '$ROLE' (expected: infra|mgmt|build|runtime|wire)"
    exit 2
    ;;
esac

log "all checks PASS for role=$ROLE"
