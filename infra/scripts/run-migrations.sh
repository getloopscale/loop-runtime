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
#      table exists). If so: register existing schema as fully applied (see
#      step inside this script — `migrate` invocation is allowlisted below).
#      # allowlist: migrate force (heal recovery — first-run-on-existing-DB only,
#      operator-invoked path; per CORRECTIONS.txt item B this is the heal pattern)
#   3. `migrate up` — applies any missing migrations idempotently.
#
# Idempotent: re-runs are no-ops once schema_migrations is present and current.
#
# References:
#   - Plan: docs/plans/dogfood-2026-05-08-fixes-plan.md (v1.1, T1.2/T1.3)
#   - ADR D1: golang-migrate canonical adoption
#   - Edge cases: EC-2 (HIGHEST dynamic detection)

set -euo pipefail

# Plan 2026-05-16 T2.4 — parameterize cluster reference for drill.
#
# Two modes:
#   1. Local k3d (default): kubectl --context k3d-loopzero-mgmt
#   2. Drill DOKS:          kubectl --kubeconfig <DRILL_KUBECONFIG>
#
# Drill mode triggered when DRILL_KUBECONFIG env var is set + readable.
# This avoids hardcoding `k3d-loopzero-mgmt` (which doesn't exist in drill DOKS
# kubeconfig) and prevents the current-context drift surface that caused
# finding-2026-05-16-drill-teardown-kubeconfig-orphan.md.

MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
NAMESPACE="${NAMESPACE:-loopzero-system}"
PG_USER="${PG_USER:-loopzero}"
PG_PASS="${PG_PASS:-loopzero-dev-password}"
PG_DB="${PG_DB:-loopzero}"
PG_HOST="${PG_HOST:-postgresql.${NAMESPACE}.svc.cluster.local}"
PG_PORT="${PG_PORT:-5432}"
# SSL mode — local k3d bitnami uses sslmode=disable; DO managed PG REQUIRES
# sslmode=require (DSN handshake fails otherwise). Drill helmfile bootstrap
# wrapper sets PG_SSLMODE=require explicitly. Default preserves local k3d.
# L2-unlock plan T2.1.bis (EC-02 MUST FIX).
PG_SSLMODE="${PG_SSLMODE:-disable}"
# Use the K8s Service DNS so this works regardless of pod IP rotation.
# Drill sets PG_HOST + PG_PORT to managed PG endpoint (Pulumi output).
DSN_DEFAULT="postgres://${PG_USER}:${PG_PASS}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=${PG_SSLMODE}&x-migrations-table=schema_migrations"
DSN="${DSN:-$DSN_DEFAULT}"

# kctl: kubectl dispatcher. Selects --kubeconfig (drill) OR --context (local k3d)
# based on DRILL_KUBECONFIG presence. Replaces direct `kubectl --context` calls.
kctl() {
  if [[ -n "${DRILL_KUBECONFIG:-}" ]]; then
    if [[ ! -r "$DRILL_KUBECONFIG" ]]; then
      echo "[run-migrations] ERROR: DRILL_KUBECONFIG not readable: $DRILL_KUBECONFIG" >&2
      return 1
    fi
    command kubectl --kubeconfig "$DRILL_KUBECONFIG" "$@"
  else
    command kubectl --context "$MGMT_CONTEXT" "$@"
  fi
}

log() { echo "[run-migrations] $*"; }

# Resolve pod label that includes both mgmt-cluster loopzero-api and any future
# replicas (Recreate strategy means single pod at a time).
LOOPZERO_API_POD="$(kctl -n "$NAMESPACE" get pod -l app=loopzero-api -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "${LOOPZERO_API_POD}" ]]; then
  # `task setup` calls `task db:migrate` BEFORE `tilt up` so loopzero-api pod
  # may not exist yet (only Helm bootstrap charts are installed by setup).
  # Tilt's local_resource db-migrate handles schema bootstrap via psql loop
  # + seeds schema_migrations to the highest version. This script (the
  # canonical golang-migrate path) takes over post-tilt via `task k3d:heal`
  # H17 + subsequent `task db:migrate` calls.
  if [[ "${LOOPZERO_MIGRATIONS_REQUIRE_API:-true}" == "false" ]]; then
    log "INFO: loopzero-api pod not found; skipping golang-migrate (pre-tilt setup)."
    log "      Tilt's db-migrate local_resource will bootstrap schema via psql"
    log "      and seed schema_migrations. Run 'task k3d:heal' after 'tilt up'"
    log "      to canonicalize via golang-migrate."
    exit 0
  fi
  # Auto-detect: if postgresql is up but loopzero-api is not, fall back to the
  # legacy psql-loop schema bootstrap. `task setup` calls this BEFORE
  # `tilt up`, and the rest of bootstrap (e.g., bootstrap:standalone seed
  # of users) requires the tables to exist. We can't use golang-migrate
  # without loopzero-api pod (image has the migrate binary) but we CAN apply
  # the same SQL files via `psql` directly to postgresql-0 — exactly what
  # `task db:migrate` did before the T1.2 refactor.
  PG_POD_BOOT="$(kctl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$PG_POD_BOOT" ]]; then
    log "INFO: loopzero-api pod absent — running psql-loop fallback (pre-tilt setup)."
    log "      schema_migrations table will be seeded with the highest version"
    log "      so a later golang-migrate up call (via task k3d:heal H17 or"
    log "      task db:migrate post-tilt) is a no-op."
    MIGRATIONS_DIR="$(cd "$(dirname "$0")/.." && pwd)/../api/internal/db/migrations"
    if [[ ! -d "$MIGRATIONS_DIR" ]]; then
      MIGRATIONS_DIR="$(pwd)/api/internal/db/migrations"
    fi
    if [[ ! -d "$MIGRATIONS_DIR" ]]; then
      log "ERROR: migrations directory not found at $MIGRATIONS_DIR"
      exit 1
    fi
    HIGHEST_FILE_VERSION=0
    for f in "$MIGRATIONS_DIR"/*.up.sql; do
      [[ -f "$f" ]] || continue
      log "  applying $(basename "$f")..."
      kctl -n "$NAMESPACE" exec -i "$PG_POD_BOOT" -c postgresql -- \
        env PGPASSWORD="$PG_PASS" psql -U "$PG_USER" -d "$PG_DB" < "$f" >/dev/null 2>&1 || \
        log "    (psql exit non-zero — IF NOT EXISTS guards usually mean ok; continuing)"
      v=$(basename "$f" | grep -oE '^[0-9]+' | sed 's/^0*//')
      [[ -z "$v" ]] && v=0
      (( v > HIGHEST_FILE_VERSION )) && HIGHEST_FILE_VERSION=$v
    done
    log "  highest version applied: $HIGHEST_FILE_VERSION"
    # Seed schema_migrations so a later golang-migrate run sees DB current.
    kctl -n "$NAMESPACE" exec "$PG_POD_BOOT" -c postgresql -- \
      env PGPASSWORD="$PG_PASS" psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -c "
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version BIGINT NOT NULL PRIMARY KEY,
            dirty BOOLEAN NOT NULL
        );
        INSERT INTO schema_migrations (version, dirty) VALUES ($HIGHEST_FILE_VERSION, false)
        ON CONFLICT (version) DO UPDATE SET dirty = false;
      " >/dev/null 2>&1 && log "  schema_migrations seeded version=$HIGHEST_FILE_VERSION"
    exit 0
  fi
  log "ERROR: loopzero-api pod not found in $MGMT_CONTEXT/$NAMESPACE"
  log "       (label selector app=loopzero-api). Is tilt up + cluster healthy?"
  exit 1
fi
log "Using loopzero-api pod: $LOOPZERO_API_POD"

# EC-2: Compute HIGHEST migration dynamically (NEVER hardcode).
# Reads /migrations/*.up.sql inside the pod (mounted from image build).
HIGHEST="$(kctl -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
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
MIGRATE_VERSION_OUT="$(kctl -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
  migrate -path /migrations -database "$DSN" version 2>&1 || true)"
log "migrate version output: $MIGRATE_VERSION_OUT"

# Probe `tenants` table via the postgresql-0 pod (which has psql) — used
# only to detect "existing DB" condition for backfill decision.
PG_POD="$(kctl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
HAS_TENANTS=""
if [[ -n "$PG_POD" ]]; then
  HAS_TENANTS="$(kctl -n "$NAMESPACE" exec "$PG_POD" -c postgresql -- \
    sh -c "env PGPASSWORD='${PG_PASS}' psql -tA -U '${PG_USER}' -d '${PG_DB}' -c \"SELECT 1 FROM pg_class WHERE relname='tenants'\" 2>/dev/null" || true)"
fi
log "tenants table present: ${HAS_TENANTS:-no}"

# Backfill condition: migrate-version reports no schema_migrations AND
# tenants table already exists.
if echo "$MIGRATE_VERSION_OUT" | grep -qiE "no migration|nilversion|first run|not exist" && [[ "$HAS_TENANTS" == "1" ]]; then
  log "Detected first run on existing DB (schema_migrations absent, tenants present)"
  log "Forcing version=$HIGHEST without re-applying SQL..."
  kctl -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
    migrate -path /migrations -database "$DSN" force "$HIGHEST"
  log "Backfill complete: schema_migrations populated."
fi

# Apply any missing migrations (no-op if up to date).
log "Running migrate up..."
kctl -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
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
kctl -n "$NAMESPACE" exec "$LOOPZERO_API_POD" -- \
  migrate -path /migrations -database "$DSN" version || true
if [[ -n "$PG_POD" ]]; then
  log "Full schema_migrations table:"
  kctl -n "$NAMESPACE" exec "$PG_POD" -c postgresql -- \
    sh -c "env PGPASSWORD='${PG_PASS}' psql -U '${PG_USER}' -d '${PG_DB}' -c 'SELECT version, dirty FROM schema_migrations;'" || true
fi
