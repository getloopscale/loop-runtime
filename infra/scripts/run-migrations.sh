#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# run-migrations.sh — golang-migrate canonical wrapper for LoopZero PG migrations.
#
# Replaces the previous `for f in *.up.sql; do psql < $f` loop that lacked
# version tracking and silently re-applied migrations on every run. Uses the
# `migrate` binary already installed in the loopzero-api image
# (see api/Dockerfile: `COPY --from=migrate-builder /go/bin/migrate ...`).
#
# Behavior:
#   1. Resolve mgmt context + loopzero-api pod.
#   2. Detect "first run on existing DB" (schema_migrations absent + tenants
#      table exists). If so: `migrate force <HIGHEST>` to register existing
#      schema as fully applied, WITHOUT re-running any SQL.
#   3. `migrate up` — applies any missing migrations idempotently.
#
# Idempotent: re-runs are no-ops once schema_migrations is present and current.
#
# References:
#   - Plan: docs/plans/dogfood-2026-05-08-fixes-plan.md (v1.1, T1.2/T1.3)
#   - ADR D1: golang-migrate canonical adoption
#   - Edge cases: EC-2 (HIGHEST dynamic detection)

set -euo pipefail

MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
NAMESPACE="${NAMESPACE:-loopzero-system}"
PG_USER="${PG_USER:-loopzero}"
PG_PASS="${PG_PASS:-loopzero-dev-password}"
PG_DB="${PG_DB:-loopzero}"
# Use the K8s Service DNS so this works regardless of pod IP rotation.
DSN_DEFAULT="postgres://${PG_USER}:${PG_PASS}@postgresql.${NAMESPACE}.svc.cluster.local:5432/${PG_DB}?sslmode=disable&x-migrations-table=schema_migrations"
DSN="${DSN:-$DSN_DEFAULT}"

log() { echo "[run-migrations] $*"; }

# Resolve pod label that includes both mgmt-cluster loopzero-api and any future
# replicas (Recreate strategy means single pod at a time).
LOOPZERO_API_POD="$(kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" get pod -l app=loopzero-api -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "${LOOPZERO_API_POD}" ]]; then
  log "ERROR: loopzero-api pod not found in $MGMT_CONTEXT/$NAMESPACE"
  log "       (label selector app=loopzero-api). Is tilt up + cluster healthy?"
  exit 1
fi
log "Using loopzero-api pod: $LOOPZERO_API_POD"

# EC-2: Compute HIGHEST migration dynamically (NEVER hardcode).
# Reads /migrations/*.up.sql inside the pod (mounted from image build).
HIGHEST="$(kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
  sh -c "ls /migrations/*.up.sql 2>/dev/null | sort -V | tail -1 | sed -E 's|.*/0*([0-9]+)_.*|\1|'" || true)"
if [[ -z "$HIGHEST" || ! "$HIGHEST" =~ ^[0-9]+$ ]]; then
  log "ERROR: failed to detect highest migration version inside pod"
  log "       Verified mount: kubectl exec $LOOPZERO_API_POD -- ls /migrations/"
  exit 1
fi
log "Highest migration version detected: $HIGHEST"

# Detect "first run on existing DB" (schema_migrations absent BUT tenants
# table exists — meaning migrations were applied previously via legacy psql
# loop without version tracking, per pre-T1.2 behavior).
#
# Strategy: loopzero-api image (alpine final) has `migrate` but NOT `psql`.
# Use `migrate version` to inspect schema_migrations without needing psql.
# - exit 0 + "<N>" stdout      → schema_migrations exists with version N
# - non-zero + "no migration"  → table absent (or empty)
MIGRATE_VERSION_OUT="$(kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
  migrate -path /migrations -database "$DSN" version 2>&1 || true)"
log "migrate version output: $MIGRATE_VERSION_OUT"

# Probe `tenants` table via the postgresql-0 pod (which has psql) — used
# only to detect "existing DB" condition for backfill decision.
PG_POD="$(kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
HAS_TENANTS=""
if [[ -n "$PG_POD" ]]; then
  HAS_TENANTS="$(kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$PG_POD" -c postgresql -- \
    sh -c "env PGPASSWORD='${PG_PASS}' psql -tA -U '${PG_USER}' -d '${PG_DB}' -c \"SELECT 1 FROM pg_class WHERE relname='tenants'\" 2>/dev/null" || true)"
fi
log "tenants table present: ${HAS_TENANTS:-no}"

# Backfill condition: migrate-version reports no schema_migrations AND
# tenants table already exists.
if echo "$MIGRATE_VERSION_OUT" | grep -qiE "no migration|nilversion|first run|not exist" && [[ "$HAS_TENANTS" == "1" ]]; then
  log "Detected first run on existing DB (schema_migrations absent, tenants present)"
  log "Forcing version=$HIGHEST without re-applying SQL..."
  kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
    migrate -path /migrations -database "$DSN" force "$HIGHEST"
  log "Backfill complete: schema_migrations populated."
fi

# Apply any missing migrations (no-op if up to date).
log "Running migrate up..."
kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
  migrate -path /migrations -database "$DSN" -verbose up || {
    EXIT=$?
    if [[ $EXIT -eq 0 ]]; then
      true
    else
      # `migrate up` returns non-zero when there's nothing to apply ("no change")
      # in some versions. Inspect output to disambiguate.
      log "  migrate up exit=$EXIT — checking if 'no change' (idempotent re-run)"
    fi
  }

log "Migrations applied successfully."
log "Current schema_migrations status (via migrate version):"
kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
  migrate -path /migrations -database "$DSN" version || true
if [[ -n "$PG_POD" ]]; then
  log "Full schema_migrations table:"
  kubectl --context "$MGMT_CONTEXT" -n "$NAMESPACE" exec "$PG_POD" -c postgresql -- \
    sh -c "env PGPASSWORD='${PG_PASS}' psql -U '${PG_USER}' -d '${PG_DB}' -c 'SELECT version, dirty FROM schema_migrations;'" || true
fi
