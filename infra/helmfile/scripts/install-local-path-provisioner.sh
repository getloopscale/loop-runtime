#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.2 / B2 fix
# Installs Rancher local-path-provisioner. Idempotent.
# Resolves B2 (DO API rate limit on PVC creation during L3 burst) — local-path
# uses node disk instead of dynamic DO Block Storage.
set -euo pipefail
ENV="${1:-dev}"
PROVISIONER_VERSION="${LOCAL_PATH_VERSION:-v0.0.31}"

log() { printf '[local-path-provisioner] %s\n' "$*" >&2; }

if kubectl get sc local-path -o jsonpath='{.provisioner}' 2>/dev/null | grep -q rancher.io/local-path; then
  log "StorageClass local-path already present (provisioner=rancher.io/local-path)"
  exit 0
fi

log "installing local-path-provisioner $PROVISIONER_VERSION (env=$ENV)"
kubectl apply -f "https://raw.githubusercontent.com/rancher/local-path-provisioner/${PROVISIONER_VERSION}/deploy/local-path-storage.yaml" >/dev/null

# Make local-path the default StorageClass for build cluster
kubectl patch storageclass local-path -p \
  '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}' >/dev/null || true

log "local-path-provisioner installed (StorageClass default)"
