#!/usr/bin/env bash
# Canonical post-Cleanroom seed for MinIO root password resolution +
# cross-cluster credential sync. Per CLAUDE.md, infra/helmfile/mgmt.helmfile.yaml.gotmpl
# explicitly declares `rootPassword: loopzero-dev-password` for MinIO bootstrap
# chart (lines 311-312). However, the MinIO Helm chart auto-generates a
# random rootPassword on first install AND preserves it across helm
# upgrade reuse-values — the helmfile declaration is overridden by chart
# auto-gen unless --set forces it.
#
# Without this script, dogfood-secrets fails Scenario A (S3 signature
# mismatch) because loopzero-api uses the canonical-declared password from
# values-dev.yaml but MinIO server actually has the random auto-gen.
#
# Layers:
# 1. Reset mgmt MinIO Secret rootPassword to canonical loopzero-dev-password
# 2. Restart MinIO pod to reload with canonical password
# 3. Sync build cluster loopzero-minio-credentials.AWS_SECRET_ACCESS_KEY +
#    minio-credentials.secretkey (legacy alias) to canonical
# 4. Restart loopzero-api so it can S3-upload again
#
# Idempotent — re-running with the same canonical password is a no-op.
# Per [[feedback_no_hardcode_gate_2026_06_09]]: the canonical password
# `loopzero-dev-password` is the declared SoT from
# infra/helm/postgresql/values.yaml + helmfile values, not a hardcode.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT_MGMT:-k3d-loopzero-mgmt}"
BUILD_CONTEXT="${KUBE_CONTEXT_BUILD:-k3d-loopzero-build}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
BUILD_NAMESPACE="${BUILD_NAMESPACE:-argo}"
CANONICAL_PW="${LOOPZERO_MINIO_ROOT_PASSWORD:-loopzero-dev-password}"

DRY_RUN=0
VERIFY=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --verify) VERIFY=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-minio-canonical] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight
kubectl --context "$MGMT_CONTEXT" get ns "$LOOPZERO_NAMESPACE" >/dev/null 2>&1 \
  || fail "mgmt context $MGMT_CONTEXT unreachable"
kubectl --context "$BUILD_CONTEXT" cluster-info >/dev/null 2>&1 \
  || fail "build context $BUILD_CONTEXT unreachable"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would sync canonical password to mgmt MinIO + build cluster credentials + restart loopzero-api"
  exit 0
fi

PW_B64=$(printf "%s" "$CANONICAL_PW" | base64 -w 0)

# Layer 1 — mgmt MinIO Secret rootPassword
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  patch secret minio --type=json \
  -p="[{\"op\":\"replace\",\"path\":\"/data/rootPassword\",\"value\":\"$PW_B64\"}]" >/dev/null
log "Layer 1: mgmt loopzero-system/minio.rootPassword synced to canonical"

# Also sync legacy alias minio-credentials.secretkey (in case anything still reads it)
if kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" get secret minio-credentials >/dev/null 2>&1; then
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    patch secret minio-credentials --type=json \
    -p="[{\"op\":\"replace\",\"path\":\"/data/secretkey\",\"value\":\"$PW_B64\"}]" >/dev/null
  log "Layer 1b: mgmt loopzero-system/minio-credentials.secretkey synced (legacy alias)"
fi

# Layer 2 — restart MinIO so it reloads root password
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout restart deployment/minio >/dev/null
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout status deployment/minio --timeout=60s >/dev/null
log "Layer 2: mgmt MinIO restarted (canonical password active)"

# Layer 3 — build cluster loopzero-minio-credentials
if kubectl --context "$BUILD_CONTEXT" -n "$BUILD_NAMESPACE" get secret loopzero-minio-credentials >/dev/null 2>&1; then
  kubectl --context "$BUILD_CONTEXT" -n "$BUILD_NAMESPACE" \
    patch secret loopzero-minio-credentials --type=json \
    -p="[{\"op\":\"replace\",\"path\":\"/data/AWS_SECRET_ACCESS_KEY\",\"value\":\"$PW_B64\"}]" >/dev/null
  log "Layer 3: build $BUILD_NAMESPACE/loopzero-minio-credentials.AWS_SECRET_ACCESS_KEY synced"
else
  log "Layer 3 SKIP: build cluster loopzero-minio-credentials Secret missing (workflow chart not yet installed)"
fi

# Layer 4 — restart loopzero-api so S3 client re-initializes with canonical creds
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout restart deployment/loopzero-api >/dev/null
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout status deployment/loopzero-api --timeout=120s >/dev/null
log "Layer 4: loopzero-api restarted (S3 client uses canonical password)"

# Verify
if [[ "$VERIFY" == "1" ]]; then
  # Probe MinIO health endpoint inside cluster
  MINIO_HEALTH=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    exec deploy/loopzero-api -- wget -qO- -S http://minio.loopzero-system.svc.cluster.local:9000/minio/health/live 2>&1 \
    | grep -c "200 OK" || echo "0")
  if [[ "$MINIO_HEALTH" -gt 0 ]]; then
    log "OK verify: MinIO /health/live returns 200 OK from loopzero-api pod"
  else
    log "FAIL verify: MinIO health probe did not return 200"
    exit 1
  fi
fi

log "DONE — canonical MinIO password synced (mgmt + build clusters)"
