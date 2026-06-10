#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T2.4 — Drill-aware migration runner wrapper.
#
# Thin wrapper at the path Helmfile expects (`infra/helmfile/scripts/`).
# Delegates to the canonical `infra/scripts/production/run-migrations.sh` which holds
# all the migration logic (golang-migrate canonical + psql-loop fallback +
# kctl dispatcher for DRILL_KUBECONFIG OR k3d MGMT_CONTEXT).
#
# This wrapper exists because:
# 1. Helmfile env drill.yaml uses scripts from `infra/helmfile/scripts/`
#    by convention (drill-mint-jwt, bootstrap-oauth-clients, etc.)
# 2. Plan T2.4 referenced this exact path in Files to edit
# 3. run-migrations.sh stays in `infra/scripts/` (canonical LoopZero location)
#    where Tilt + task db:migrate already invoke it
#
# Usage (drill mode):
#   DRILL_KUBECONFIG=/tmp/drill-mgmt-kc.yaml \
#   PG_HOST=<pulumi output pg.host> \
#   PG_PORT=25060 \
#   PG_PASS=<drill PG password> \
#   bash infra/helmfile/scripts/migrate-database.sh
#
# Usage (local k3d mode — backward compat):
#   bash infra/helmfile/scripts/migrate-database.sh
#   # Falls back to k3d-loopzero-mgmt context + in-cluster postgresql
#
# All behavior + error codes come from run-migrations.sh per Plan T2.4.

set -euo pipefail

WRAPPER_DIR="$(cd "$(dirname "$0")" && pwd)"
CANONICAL_SCRIPT="$WRAPPER_DIR/../../scripts/production/run-migrations.sh"

if [[ ! -x "$CANONICAL_SCRIPT" ]]; then
  echo "[migrate-database] ERROR: canonical run-migrations.sh missing at $CANONICAL_SCRIPT" >&2
  exit 1
fi

# Pass through all args + env to canonical script. The kctl() dispatcher
# inside run-migrations.sh handles drill vs k3d mode selection automatically.
exec "$CANONICAL_SCRIPT" "$@"
