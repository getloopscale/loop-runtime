#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.3 + T2.4
# Generates Secrets for runtime cluster (cloudflared tunnel token, ESO source).
#
# IDEMPOTENT (iter 14): refuses to overwrite Secret values with empty
# strings. If LOOPZERO_CF_TUNNEL_TOKEN or LOOPZERO_DB_PASSWORD env vars are absent
# (e.g. wire script invoked without re-fetching pulumi outputs), the existing
# Secret is preserved instead of being clobbered with empty values.
#
# Usage: generate-runtime-secrets.sh <env> <cluster-id>
#   env:        dev | prod | standalone
#   cluster-id: runtime | runtime-2 | ...
#
# ADR D9 — cloudflared Deployment moved from Pulumi to Helmfile. Tunnel token
# is sourced from `pulumi stack output cfTunnelToken --show-secrets`.
set -euo pipefail

ENV="${1:?usage: $0 <env> <cluster-id>}"
CLUSTER_ID="${2:?usage: $0 <env> <cluster-id>}"

# Issue #117 (generalizing #115) — caller-pinned cluster contract, asserted.
#
# Unlike its mgmt-bound siblings, this script has no single fixed target: the
# runtime cluster varies with <cluster-id> (workload, runtime-2, ...). The
# established contract is that the CALLER pins it, e.g.
#   KUBECONFIG=$(k3d kubeconfig write loopzero-cell-0) bash $0 standalone-multi workload
# (see taskfiles/infra.yml, which deliberately pins via KUBECONFIG rather than
# mutating the global current-context).
#
# Inheriting the caller's choice is fine. Inheriting it SILENTLY is the #117
# bug: with KUBECONFIG unset we fall through to whatever kubecontext happens to
# be active and write runtime Secrets — cloudflared tunnel token, ESO source —
# into the wrong cluster, succeeding quietly. Fail loud instead.
: "${KUBECONFIG:?issue #117: refusing to run without an explicit KUBECONFIG. This script writes Secrets to the RUNTIME cluster and will not inherit an ambient kubecontext. Invoke as: KUBECONFIG=\$(k3d kubeconfig write loopzero-<cluster>) $0 <env> <cluster-id>}"

log() { printf '[generate-runtime-secrets] %s\n' "$*" >&2; }

ensure_namespace() {
  local ns="$1"
  kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

# get_existing: returns the decoded value of a Secret data key, or empty if absent.
get_existing() {
  local name="$1" ns="$2" key="$3"
  local raw
  raw=$(kubectl get secret "$name" -n "$ns" -o "jsonpath={.data.${key//./\\.}}" 2>/dev/null || true)
  [[ -z "$raw" ]] && { printf ''; return; }
  printf '%s' "$raw" | base64 -d 2>/dev/null || true
}

ensure_namespace loopzero-system

# 1) cloudflared tunnel token (D9) — IDEMPOTENT.
# Pulumi is the source of truth, but if env var is empty (e.g. invoked from
# wire script without re-fetching outputs), preserve existing Secret value.
TUNNEL_TOKEN="${LOOPZERO_CF_TUNNEL_TOKEN:-}"
if [[ -z "$TUNNEL_TOKEN" ]]; then
  TUNNEL_TOKEN=$(get_existing cloudflared-tunnel-token loopzero-system token)
  [[ -n "$TUNNEL_TOKEN" ]] && log "preserving existing cloudflared-tunnel-token (env var was empty)"
fi
if [[ -n "$TUNNEL_TOKEN" ]]; then
  kubectl create secret generic cloudflared-tunnel-token -n loopzero-system \
    --from-literal=token="$TUNNEL_TOKEN" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "cloudflared-tunnel-token applied (idempotent)"
elif [[ "${LOOPZERO_PROFILE:-${ENV:-}}" == *standalone* ]]; then
  # B61 (end-to-end-deploy-validation-plan T2.2) — standalone profile without
  # real CF Tunnel. Apply a sentinel token so the Secret exists; cloudflared
  # release uses replicas:0 in standalone (cloudflared/values.yaml) so the
  # token is never actually dialed against CF edge.
  kubectl create secret generic cloudflared-tunnel-token -n loopzero-system \
    --from-literal=token="STANDALONE_LOCAL_NO_CF_TUNNEL" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "B61: standalone fallback — sentinel cf-tunnel token (cloudflared replicas:0)"
else
  log "WARNING: LOOPZERO_CF_TUNNEL_TOKEN not set AND no existing Secret — cloudflared release will fail"
fi

# 2) ESO source connection — IDEMPOTENT for DB password.
ensure_namespace external-secrets
if [[ -n "${LOOPZERO_DB_HOST:-}" ]]; then
  DB_PASSWORD="${LOOPZERO_DB_PASSWORD:-}"
  if [[ -z "$DB_PASSWORD" ]]; then
    DB_PASSWORD=$(get_existing loopzero-eso-source external-secrets db-password)
    [[ -n "$DB_PASSWORD" ]] && log "preserving existing db-password in loopzero-eso-source"
  fi
  kubectl create secret generic loopzero-eso-source -n external-secrets \
    --from-literal=db-host="${LOOPZERO_DB_HOST}" \
    --from-literal=db-port="${LOOPZERO_DB_PORT:-5432}" \
    --from-literal=db-user="${LOOPZERO_DB_USER:-loopzero}" \
    --from-literal=db-password="${DB_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "loopzero-eso-source applied (cluster=$CLUSTER_ID, idempotent)"
fi

# 3) static-assets-s3 — credentials for nginx-s3-gateway (B82).
# Static frontend deploys (api/internal/manifests/renderer_static.go) target
# `Service loopzero-system/static-assets`; this Secret feeds AWS_ACCESS_KEY_ID +
# AWS_SECRET_ACCESS_KEY into the gateway. In standalone-multi the values come
# from LOOPZERO_S3_ACCESS_KEY / LOOPZERO_S3_SECRET_KEY (see generate-secrets.sh, which
# already exports MinIO root credentials). In dev/prod ESO replaces this Secret
# with values pulled from DO Spaces creds.
S3_KEY="${LOOPZERO_S3_ACCESS_KEY:-}"
S3_SECRET="${LOOPZERO_S3_SECRET_KEY:-}"
if [[ -z "$S3_KEY" ]]; then
  S3_KEY=$(get_existing static-assets-s3 loopzero-system AWS_ACCESS_KEY_ID)
fi
if [[ -z "$S3_SECRET" ]]; then
  S3_SECRET=$(get_existing static-assets-s3 loopzero-system AWS_SECRET_ACCESS_KEY)
fi
if [[ -z "$S3_KEY" || -z "$S3_SECRET" ]]; then
  if [[ "${LOOPZERO_PROFILE:-${ENV:-}}" == *standalone* ]]; then
    # Standalone uses MinIO root creds (helm/minio/values.yaml defaults).
    # Real production never hits this branch.
    # T1.3: pull credentials from minio-credentials Secret if it exists
    # (created by generate-secrets.sh earlier in the bootstrap chain), else
    # generate fresh random values. No hardcoded literal.
    S3_KEY="${S3_KEY:-$(kubectl -n loopzero-system get secret minio-credentials -o jsonpath='{.data.accesskey}' 2>/dev/null | base64 -d 2>/dev/null || openssl rand -hex 8)}"
    S3_SECRET="${S3_SECRET:-$(kubectl -n loopzero-system get secret minio-credentials -o jsonpath='{.data.secretkey}' 2>/dev/null | base64 -d 2>/dev/null || openssl rand -base64 24 | tr -d '/+=')}"
    log "B82: standalone fallback — using MinIO root credentials for static-assets-s3"
  else
    log "WARNING: static-assets-s3 credentials missing — static frontend delivery will fail"
  fi
fi
if [[ -n "$S3_KEY" && -n "$S3_SECRET" ]]; then
  kubectl create secret generic static-assets-s3 -n loopzero-system \
    --from-literal=AWS_ACCESS_KEY_ID="$S3_KEY" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$S3_SECRET" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "static-assets-s3 applied (idempotent)"
fi

# 4) Backup runtime Secrets for disaster recovery.
# The restorable backup lives in generate-secrets.sh; this one used to dump raw
# `kubectl get -o yaml`, which does not re-apply after the Secret changed. This
# script runs against the current context, so the backup is pinned to it.
KUBECTL_CONTEXT="$(command kubectl config current-context)" "$(dirname "$0")/generate-secrets.sh" --backup-only \
  "runtime-secrets-${CLUSTER_ID}-${ENV}" loopzero-system/cloudflared-tunnel-token external-secrets/loopzero-eso-source

log "runtime secrets generated (env=$ENV, cluster=$CLUSTER_ID, idempotent)"
