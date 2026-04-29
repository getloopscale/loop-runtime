#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-COMPLETO — T1.1
#
# Clone fresh do loopzero-stacks upstream e copia examples/monorepo-turbo
# para diretório de trabalho efêmero. Idempotente — executa exclude+clone
# por padrão para evitar drift entre sessões (ADR D1).
#
# Uso:
#   bash infra/tests/dogfood/setup-monorepo-turbo.sh
#   WORK_DIR=/tmp/dogfood-custom bash infra/tests/dogfood/setup-monorepo-turbo.sh
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

LOOPZERO_STACKS_REPO="${LOOPZERO_STACKS_REPO:-https://github.com/getloopscale/loopzero-stacks}"
LOOPZERO_STACKS_REF="${LOOPZERO_STACKS_REF:-main}"
WORK_DIR="${WORK_DIR:-/tmp/dogfood-monorepo-turbo}"
CLONE_DIR="${CLONE_DIR:-/tmp/loopzero-stacks}"
TEMPLATE_PATH="${TEMPLATE_PATH:-examples/monorepo-turbo}"

log() { echo "[setup-monorepo-turbo] $*" >&2; }

require_bin() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required binary: $1" >&2; exit 2; }
}

require_bin git

# Fresh clone — preserve previous WORK_DIR by exiting early if user wants.
if [[ -d "$WORK_DIR" ]]; then
  log "removing stale WORK_DIR=$WORK_DIR"
  rm -rf "$WORK_DIR"
fi

if [[ -d "$CLONE_DIR/.git" ]]; then
  log "updating existing clone at $CLONE_DIR (git fetch)"
  git -C "$CLONE_DIR" fetch --depth 1 origin "$LOOPZERO_STACKS_REF"
  git -C "$CLONE_DIR" reset --hard "origin/$LOOPZERO_STACKS_REF"
else
  rm -rf "$CLONE_DIR"
  log "cloning $LOOPZERO_STACKS_REPO@$LOOPZERO_STACKS_REF into $CLONE_DIR"
  if ! git clone --depth 1 --branch "$LOOPZERO_STACKS_REF" "$LOOPZERO_STACKS_REPO" "$CLONE_DIR" 2>&1; then
    echo "ERROR: failed to clone $LOOPZERO_STACKS_REPO" >&2
    exit 1
  fi
fi

if [[ ! -d "$CLONE_DIR/$TEMPLATE_PATH" ]]; then
  echo "ERROR: template path '$TEMPLATE_PATH' not found in $CLONE_DIR" >&2
  echo "Available examples:" >&2
  ls "$CLONE_DIR/examples" 2>/dev/null >&2 || ls "$CLONE_DIR" >&2
  exit 1
fi

cp -r "$CLONE_DIR/$TEMPLATE_PATH" "$WORK_DIR"
log "template copied to $WORK_DIR"

# Sanity: loopzero.yaml + Dockerfiles
[[ -f "$WORK_DIR/loopzero.yaml" ]] || { echo "ERROR: loopzero.yaml missing in template" >&2; exit 1; }

log "WORK_DIR=$WORK_DIR ready"
log "loopzero.yaml:"
cat "$WORK_DIR/loopzero.yaml" >&2
echo "$WORK_DIR"
