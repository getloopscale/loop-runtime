#!/usr/bin/env bash
# infra/scripts/build/build-and-push-dockerfile-lint.sh
#
# build-mode-dispatch-and-dockerfile-security-plan T3.3 (2026-05-07).
#
# Builds loopzero-dockerfile-lint from local source at tools/loopzero-dockerfile-lint
# and pushes the resulting image as :v0.1.0 + :latest to the
# k3d-loopzero-registry.
#
# Idempotent. Safe to call from `task setup`, `task k3d:heal`, and the
# cleanroom dogfood loop.
#
# Usage:
#   bash infra/scripts/build/build-and-push-dockerfile-lint.sh
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

VERSION="${LOOPZERO_LINT_VERSION:-v0.1.0}"
REGISTRY_PORT="${LOOPZERO_REGISTRY_PORT:-5000}"
REGISTRY_HOST="${LOOPZERO_REGISTRY_HOST:-localhost}"
IMAGE_NAME="loopzero-dockerfile-lint"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE_DIR="${REPO_ROOT}/tools/loopzero-dockerfile-lint"

log() { printf '[build-dockerfile-lint %s] %s\n' "$(date +%H:%M:%S)" "$*"; }

# Verify registry reachable
if ! curl -sf -o /dev/null --max-time 3 "http://${REGISTRY_HOST}:${REGISTRY_PORT}/v2/" 2>/dev/null; then
  log "ERROR: registry ${REGISTRY_HOST}:${REGISTRY_PORT} not reachable"
  log "  Hint: ensure k3d cluster + registry are running (task setup)"
  exit 1
fi

if [[ ! -f "$SOURCE_DIR/Dockerfile" ]]; then
  log "ERROR: lint source not found at $SOURCE_DIR (no Dockerfile)"
  exit 1
fi

log "Building $IMAGE_NAME from $SOURCE_DIR ..."
docker build \
  -f "$SOURCE_DIR/Dockerfile" \
  -t "${REGISTRY_HOST}:${REGISTRY_PORT}/${IMAGE_NAME}:${VERSION}" \
  -t "${REGISTRY_HOST}:${REGISTRY_PORT}/${IMAGE_NAME}:latest" \
  "$SOURCE_DIR"

log "Pushing ${REGISTRY_HOST}:${REGISTRY_PORT}/${IMAGE_NAME}:${VERSION} ..."
docker push "${REGISTRY_HOST}:${REGISTRY_PORT}/${IMAGE_NAME}:${VERSION}"

log "Pushing ${REGISTRY_HOST}:${REGISTRY_PORT}/${IMAGE_NAME}:latest ..."
docker push "${REGISTRY_HOST}:${REGISTRY_PORT}/${IMAGE_NAME}:latest"

log "Verifying registry has $VERSION tag ..."
TAGS=$(curl -sf "http://${REGISTRY_HOST}:${REGISTRY_PORT}/v2/${IMAGE_NAME}/tags/list" 2>/dev/null || echo "")
if ! echo "$TAGS" | grep -q "\"$VERSION\""; then
  log "ERROR: $VERSION tag not visible in registry after push"
  log "  Registry response: $TAGS"
  exit 1
fi

log "DONE — loopzero-dockerfile-lint $VERSION + latest published to ${REGISTRY_HOST}:${REGISTRY_PORT}"
