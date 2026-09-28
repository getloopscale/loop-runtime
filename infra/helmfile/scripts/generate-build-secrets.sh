#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.2
# Generates Secrets required by build cluster releases.
#
# IDEMPOTENT (iter 14): cosign keypair is generated ONCE and preserved across
# re-runs. Re-builds NEVER invalidate the previous signing key — sigstore
# verification chain stays intact for previously-signed images.
#
# Usage: generate-build-secrets.sh <env>
#
# B1 fix: cosign keypair is GENERATED via `cosign generate-key-pair` and
#         persisted in Secret cosign-signing-key. Iter 14 adds idempotency:
#         only regenerates if missing OR is the literal "PLACEHOLDER" string.
# B12 fix: registry credentials Secret is host-scoped (handled by
#          registry_auth_merge.go).
#
# Backup: every successful run dumps cosign-signing-key + buildkit-registry-config
# to ~/.loopzero/backups/build-secrets-{env}-{timestamp}.yaml (chmod 600).
set -euo pipefail

ENV="${1:?usage: $0 <env>}"
# Absolute, resolved before anything changes directory: the cosign step below
# `cd`s into a temporary directory on a cold install, and a path derived from $0
# afterwards pointed nowhere (#399).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Issue #117 (generalizing #115) — pin the target cluster BY CONSTRUCTION.
# This script writes Secrets to the BUILD cluster. Before this pin, it relied
# on the caller having mutated the global kubecontext (`kubectl config
# use-context k3d-loopzero-build` in Taskfile.yml). That is fragile in two ways:
# a caller that forgets provisions build Secrets into whatever cluster is
# active, and mutating the operator's global context as a side effect leaks
# out of the task when it fails mid-way. The `kubectl()` override below makes
# every bare invocation downstream carry the right --context, so neither can
# happen. Override KUBECTL_CONTEXT for non-k3d targets (DOKS prod).
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-build}"
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

log() { printf '[generate-build-secrets] %s\n' "$*" >&2; }

ensure_namespace() {
  local ns="$1"
  kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

# is_valid_cosign_secret: returns 0 if the Secret exists, has cosign.key,
# AND the value is not the literal "PLACEHOLDER" sentinel.
is_valid_cosign_secret() {
  local raw existing
  raw=$(kubectl get secret cosign-signing-key -n argo -o 'jsonpath={.data.cosign\.key}' 2>/dev/null || true)
  [[ -z "$raw" ]] && return 1
  existing=$(printf '%s' "$raw" | base64 -d 2>/dev/null || true)
  [[ -z "$existing" ]] && return 1
  [[ "$existing" == *PLACEHOLDER* ]] && return 1
  # Sanity: cosign private keys start with "-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----"
  [[ "$existing" == *"BEGIN ENCRYPTED SIGSTORE PRIVATE KEY"* ]] || return 1
  return 0
}

ensure_namespace argo

# 1) cosign keypair — IDEMPOTENT (B1 + iter 14)
COSIGN_DIR="$(mktemp -d)"
trap 'rm -rf "$COSIGN_DIR"' EXIT

if is_valid_cosign_secret; then
  log "cosign-signing-key is valid and non-placeholder — preserving (idempotent)"
else
  if ! command -v cosign >/dev/null 2>&1; then
    log "ERROR: cosign not installed. Install: https://docs.sigstore.dev/cosign/installation/"
    exit 1
  fi

  # Reuse existing password if Secret has one (preserves rotation history).
  EXISTING_PWD=$(kubectl get secret cosign-signing-key -n argo \
    -o 'jsonpath={.data.cosign\.password}' 2>/dev/null | base64 -d 2>/dev/null || true)
  COSIGN_PASSWORD="${COSIGN_PASSWORD:-${EXISTING_PWD:-$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)}}"
  export COSIGN_PASSWORD

  cd "$COSIGN_DIR"
  cosign generate-key-pair --output-key-prefix cosign 2>&1 | grep -v "^Private key" >&2

  kubectl create secret generic cosign-signing-key -n argo \
    --from-file=cosign.key=cosign.key \
    --from-file=cosign.pub=cosign.pub \
    --from-literal=cosign.password="${COSIGN_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "cosign-signing-key generated and applied (B1 fixed permanently)"

  # T2.3 of openbao-adoption v1.3 (iter 26): also write cosign passphrase to
  # Vault path secret/loopzero/<env>/build/secrets/cosign/passphrase. Dual-write
  # during migration window (Day 30 verify per T10; Day 60 removes K8s
  # cosign.password key). When BAO_ADDR + BAO_TOKEN are set, mirror passphrase
  # to Vault so ESO ExternalSecret CR `cosign-passphrase` (per
  # infra/helm/loopzero-external-secrets/values.yaml) materializes it independently.
  # FAANG defense-in-depth: passphrase + key separately rotatable.
  if [[ -n "${BAO_ADDR:-}" && -n "${BAO_TOKEN:-}" ]]; then
    if command -v bao >/dev/null 2>&1; then
      log "T2.3: writing cosign passphrase to Vault secret/loopzero/${ENV}/build/secrets/cosign/passphrase"
      if bao kv put "secret/loopzero/${ENV}/build/secrets/cosign/passphrase" \
          "cosign.password=${COSIGN_PASSWORD}" >/dev/null 2>&1; then
        log "T2.3: Vault write OK (dual-write active; canonical SoT post Day-30 verify)"
      else
        log "[WARN] T2.3: bao kv put returned non-zero — K8s Secret remains canonical"
      fi
    else
      log "[INFO] T2.3: bao CLI not available — skipping Vault dual-write (BAO_ADDR set but no bao)"
    fi
  else
    log "[INFO] T2.3: BAO_ADDR/BAO_TOKEN not set — K8s Secret remains canonical (set both to dual-write)"
  fi
fi

# 2) Registry credentials for BuildKit (host-scoped — B12 fix).
# Always overwritten — values come from L1 outputs, not stable in cluster.
if [[ -n "${LOOPZERO_REGISTRY_ENDPOINT:-}" ]]; then
  AUTH_VALUE=$(printf '%s' "${LOOPZERO_SPACES_KEY:-}:${LOOPZERO_SPACES_SECRET:-}" | base64 -w0)
  CONFIG=$(printf '{"auths":{"%s":{"auth":"%s"}}}' "${LOOPZERO_REGISTRY_ENDPOINT}" "${AUTH_VALUE}")
  kubectl create secret generic buildkit-registry-config -n argo \
    --from-literal="config.json=${CONFIG}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "buildkit-registry-config applied (host-scoped)"
fi

# 3) MinIO/S3 credentials for artifact upload — passthrough from L1.
if [[ -n "${LOOPZERO_BUCKET_NAME:-}" ]]; then
  kubectl create secret generic minio-credentials -n argo \
    --from-literal=accesskey="${LOOPZERO_SPACES_KEY:-}" \
    --from-literal=secretkey="${LOOPZERO_SPACES_SECRET:-}" \
    --from-literal=endpoint="${LOOPZERO_BUCKET_ENDPOINT:-}" \
    --from-literal=bucket="${LOOPZERO_BUCKET_NAME}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "minio-credentials applied"
fi

# 4) Backup all build-cluster Secrets for disaster recovery.
# The restorable backup lives in generate-secrets.sh; this one used to dump raw
# `kubectl get -o yaml`, which does not re-apply after the Secret changed.
KUBECTL_CONTEXT="$KUBECTL_CONTEXT" "$SCRIPT_DIR/generate-secrets.sh" --backup-only \
  "build-secrets-${ENV}" argo/cosign-signing-key argo/buildkit-registry-config argo/minio-credentials

log "build secrets generated for env=$ENV (idempotent — re-runs preserve cosign signing key)"
