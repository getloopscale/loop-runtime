#!/usr/bin/env bash
# infra/scripts/build-and-push-loopzero-packs.sh
#
# build-mode-dispatch-and-dockerfile-security-plan T3.3 (2026-05-07).
#
# Builds loopzero-packs-runner from local source at /home/paulo/loopzero-packs
# (or path overridden via LOOPZERO_PACKS_SRC env var) and pushes the resulting
# image as v0.4.0 + :latest to the k3d-loopzero-registry.
#
# Idempotent. Safe to call from `task setup`, `task k3d:heal`, and the
# cleanroom dogfood loop. Fails fast with actionable message when source
# is missing OR when cluster registry is unreachable.
#
# Usage:
#   bash infra/scripts/build-and-push-loopzero-packs.sh         # build + push
#   LOOPZERO_PACKS_SRC=/path/to/loopzero-packs bash ...sh            # custom path
#   LOOPZERO_PACKS_SRC=skip bash ...sh                           # skip if image already present
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

VERSION="${LOOPZERO_PACKS_VERSION:-v0.4.0}"
REGISTRY_PORT="${LOOPZERO_REGISTRY_PORT:-5000}"
REGISTRY_HOST="${LOOPZERO_REGISTRY_HOST:-localhost}"
SOURCE_DIR="${LOOPZERO_PACKS_SRC:-/home/paulo/loopzero-packs}"
IMAGE_NAME="loopzero-packs-runner"

log() { printf '[build-loopzero-packs %s] %s\n' "$(date +%H:%M:%S)" "$*"; }

if [[ "$SOURCE_DIR" == "skip" ]]; then
  log "SKIP: LOOPZERO_PACKS_SRC=skip — assuming image already present in registry"
  exit 0
fi

# Verify registry reachable
if ! curl -sf -o /dev/null --max-time 3 "http://${REGISTRY_HOST}:${REGISTRY_PORT}/v2/" 2>/dev/null; then
  log "ERROR: registry ${REGISTRY_HOST}:${REGISTRY_PORT} not reachable"
  log "  Hint: ensure k3d cluster + registry are running (task setup)"
  exit 1
fi

# Verify source exists (T0.1-T0.3 validated source has lenient behavior)
if [[ ! -d "$SOURCE_DIR" ]]; then
  log "ERROR: loopzero-packs source not found at $SOURCE_DIR"
  log "  Hint: clone https://github.com/getloopscale/loopzero-packs to $SOURCE_DIR"
  log "  Or set LOOPZERO_PACKS_SRC=/path/to/loopzero-packs"
  log "  Or set LOOPZERO_PACKS_SRC=skip if you've manually pre-loaded the image"
  exit 1
fi

if [[ ! -f "$SOURCE_DIR/Dockerfile.generate" ]]; then
  log "ERROR: $SOURCE_DIR is not a loopzero-packs source tree (no Dockerfile.generate)"
  exit 1
fi

# Idempotent: skip rebuild if image with this digest is already present and a
# canary check (running `loopzeropacks-generate --help` or trivial smoke) succeeds.
# For now we always rebuild — Docker layer caching makes this cheap (~1s no-op).

log "Building $IMAGE_NAME from $SOURCE_DIR ..."
docker build \
  -f "$SOURCE_DIR/Dockerfile.generate" \
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

log "DONE — loopzero-packs-runner $VERSION + latest published to ${REGISTRY_HOST}:${REGISTRY_PORT}"
