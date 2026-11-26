#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# run-migrations.sh — golang-migrate canonical wrapper for LoopRuntime PG migrations.
#
# Replaces the previous `for f in *.up.sql; do psql < $f` loop that lacked
# version tracking and silently re-applied migrations on every run. Uses the
# `migrate` binary already installed in the loop-runtime-api image
# (see api/Dockerfile: `COPY --from=migrate-builder /go/bin/migrate ...`).
#
# Behavior:
#   1. Resolve mgmt context + loop-runtime-api pod.
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
#   - Plan: dogfood-2026-05-08-fixes-plan (v1.1, T1.2/T1.3)
#   - ADR D1: golang-migrate canonical adoption
#   - Edge cases: EC-2 (HIGHEST dynamic detection)

set -euo pipefail

# T2.2-followup (plan 2026-05-21-deep-review-remediation): adopt dry-run lib.
# In dry-run mode, the script prints what migration would be applied
# (via `migrate version`) and exits without executing `migrate up`.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/dry-run.sh disable=SC1091
source "$SCRIPT_DIR/../lib/dry-run.sh"
parse_dry_run_flag "$@"
if [ "${#REMAINING_ARGS[@]}" -gt 0 ]; then
  set -- "${REMAINING_ARGS[@]}"
else
  set --
fi

# Plan 2026-05-16 T2.4 — parameterize cluster reference for drill.
#
# Two modes:
#   1. Local k3d (default): kubectl --context k3d-loop-runtime-core
#   2. Drill DOKS:          kubectl --kubeconfig <DRILL_KUBECONFIG>
#
# Drill mode triggered when DRILL_KUBECONFIG env var is set + readable.
# This avoids hardcoding `k3d-loop-runtime-core` (which doesn't exist in drill DOKS
# kubeconfig) and prevents the current-context drift surface that caused
# finding-2026-05-16-drill-teardown-kubeconfig-orphan.md.

CORE_CONTEXT="${CORE_CONTEXT:-k3d-loop-runtime-core}"
NAMESPACE="${NAMESPACE:-loop-runtime-system}"
PG_USER="${PG_USER:-loop_runtime}"
# T1.3 (plan 2026-05-21-deep-review-remediation): read from K8s Secret SoT.
# Fail fast if neither env nor Secret provides it.
PG_PASS="${PG_PASS:-$(kubectl --context "$CORE_CONTEXT" -n "$NAMESPACE" get secret loop-runtime-db-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d || true)}"
if [[ -z "$PG_PASS" ]]; then
  echo "ERROR: PG_PASS unset AND loop-runtime-db-credentials Secret missing in $NAMESPACE" >&2
  echo "       Run generate-secrets.sh first OR set PG_PASS env var" >&2
  exit 1
fi
PG_DB="${PG_DB:-loop_runtime}"
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
    command kubectl --context "$CORE_CONTEXT" "$@"
  fi
}

log() { echo "[run-migrations] $*"; }

# pg_psql <pod> <psql args...> — psql inside the postgresql pod with
# ON_ERROR_STOP=1, so a failed statement is a non-zero exit instead of a
# warning psql would otherwise swallow. stdin is forwarded.
pg_psql() {
  local pod="$1"
  shift
  kctl -n "$NAMESPACE" exec -i "$pod" -c postgresql -- \
    env PGPASSWORD="$PG_PASS" psql -v ON_ERROR_STOP=1 -U "$PG_USER" -d "$PG_DB" "$@"
}

# stamp_schema_version <pod> <version> <dirty> — write golang-migrate's
# single-row schema_migrations record. Same contract as the migrate binary:
# dirty=true is written BEFORE a file runs and cleared only after it
# succeeds, so a failure leaves the database marked dirty and every later
# `migrate up` refuses it until an operator resolves the half-applied file.
stamp_schema_version() {
  local pod="$1" version="$2" dirty="$3"
  pg_psql "$pod" -q -c "BEGIN; TRUNCATE schema_migrations; INSERT INTO schema_migrations (version, dirty) VALUES (${version}, ${dirty}); COMMIT;" >/dev/null
}

# apply_migrations_with_psql <pod> <dir> — the pre-API bootstrap path.
#
# The migrate binary ships only in the loop-runtime-api image, so before that pod is
# Ready the SQL files are applied with psql directly. This path keeps the
# guarantees golang-migrate gives: it resumes from the recorded version,
# refuses a dirty database, stops at the first failed file and never records
# a failed schema as clean. A failure exits non-zero naming the file.
apply_migrations_with_psql() {
  local pod="$1" dir="$2"
  pg_psql "$pod" -q -c "CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL);" >/dev/null

  local recorded current dirty
  recorded="$(pg_psql "$pod" -tA -c "SELECT MAX(version), bool_or(dirty) FROM schema_migrations;")"
  current="${recorded%%|*}"
  dirty="${recorded##*|}"
  [[ -z "$current" ]] && current=0
  if [[ "$dirty" == "t" ]]; then
    log "ERROR: schema_migrations is dirty at version $current — a previous migration failed half-way."
    # allowlist: migrate force (printed advice only — the operator runs it after repairing the half-applied file by hand; this script never forces)
    log "       Inspect the database, repair it, then clear the flag with"
    log "       'migrate force $current' before running migrations again."
    exit 1
  fi
  log "  schema_migrations at version $current"

  local f name v out applied=0
  for f in "$dir"/*.up.sql; do
    [[ -f "$f" ]] || continue
    name="$(basename "$f")"
    v="$(grep -oE '^[0-9]+' <<<"$name" | sed 's/^0*//')"
    if [[ -z "$v" ]]; then
      log "ERROR: migration file without a numeric version prefix: $name"
      exit 1
    fi
    (( v <= current )) && continue
    log "  applying $name..."
    stamp_schema_version "$pod" "$v" true
    if ! out="$(pg_psql "$pod" -q < "$f" 2>&1)"; then
      log "ERROR: migration $name failed; schema_migrations left at version $v dirty=true."
      printf '%s\n' "$out" | sed 's/^/    [psql] /'
      exit 1
    fi
    stamp_schema_version "$pod" "$v" false
    applied=$((applied + 1))
  done
  log "  applied $applied migration(s); schema_migrations clean."
}

# Resolve pod label that includes both core-cluster loop-runtime-api and any future
# replicas (Recreate strategy means single pod at a time).
LOOP_RUNTIME_API_POD="$(kctl -n "$NAMESPACE" get pod -l app=loop-runtime-api -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"

# Issue #106 Design E (Cleanroom catch 2026-06-19; ADR
# `2026-06-19-loop-runtime-api-vault-bootstrap-ordering`). The pod-NAME presence
# check alone misses the CrashLoopBackOff case. In dev-public env,
# loop-runtime-api ships with OPENBAO_TRANSIT_ENABLED=true which makes the binary
# fail-fast on missing OPENBAO_VAULT_SECRET_ID (that env var only ships
# from seed-post-cleanroom-all.sh Step 7b post-vault-bootstrap). The pod
# NAME exists (Deployment scheduled) but the container "loop-runtime-api" is not
# Running, so `kubectl exec` errors with `container not found`. Honor the
# existing fallback intent by ALSO triggering the psql-loop path when
# the named container is not Ready. Defensive jsonpath picks the
# container by name (safe against future sidecars).
LOOP_RUNTIME_API_READY="false"
if [[ -n "${LOOP_RUNTIME_API_POD}" ]]; then
  LOOP_RUNTIME_API_READY="$(kctl -n "$NAMESPACE" get pod "$LOOP_RUNTIME_API_POD" -o jsonpath='{.status.containerStatuses[?(@.name=="loop-runtime-api")].ready}' 2>/dev/null || echo "false")"
  [[ -z "$LOOP_RUNTIME_API_READY" ]] && LOOP_RUNTIME_API_READY="false"
fi

if [[ -z "${LOOP_RUNTIME_API_POD}" || "$LOOP_RUNTIME_API_READY" != "true" ]]; then
  # `task setup` calls `task db:migrate` BEFORE `tilt up` so loop-runtime-api pod
  # may not exist yet (only Helm bootstrap charts are installed by setup).
  # Tilt's local_resource db-migrate handles schema bootstrap via psql loop
  # + seeds schema_migrations to the highest version. This script (the
  # canonical golang-migrate path) takes over post-tilt via `task k3d:heal`
  # H17 + subsequent `task db:migrate` calls.
  if [[ "${LOOP_RUNTIME_MIGRATIONS_REQUIRE_API:-true}" == "false" ]]; then
    log "INFO: loop-runtime-api pod absent OR not Ready (CrashLoopBackOff); skipping golang-migrate (pre-tilt setup)."
    log "      Tilt's db-migrate local_resource will bootstrap schema via psql"
    log "      and seed schema_migrations. Run 'task k3d:heal' after 'tilt up'"
    log "      to canonicalize via golang-migrate."
    exit 0
  fi
  # Auto-detect: if postgresql is up but loop-runtime-api is not, fall back to the
  # legacy psql-loop schema bootstrap. `task setup` calls this BEFORE
  # `tilt up`, and the rest of bootstrap (e.g., bootstrap:standalone seed
  # of users) requires the tables to exist. We can't use golang-migrate
  # without loop-runtime-api pod (image has the migrate binary) but we CAN apply
  # the same SQL files via `psql` directly to postgresql-0 — exactly what
  # `task db:migrate` did before the T1.2 refactor.
  PG_POD_BOOT="$(kctl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$PG_POD_BOOT" ]]; then
    if [[ -z "${LOOP_RUNTIME_API_POD}" ]]; then
      log "INFO: loop-runtime-api pod absent — running psql-loop fallback (pre-tilt setup)."
    else
      log "INFO: loop-runtime-api pod ${LOOP_RUNTIME_API_POD} not Ready (CrashLoopBackOff suspected) — running psql-loop fallback (issue #106 Design E)."
    fi
    log "      Applies only the migrations newer than schema_migrations, stamps"
    log "      each one the way golang-migrate does, and stops at the first failure."
    if [[ -z "${MIGRATIONS_DIR:-}" ]]; then
      MIGRATIONS_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)/api/internal/db/migrations"
    fi
    if [[ ! -d "$MIGRATIONS_DIR" ]]; then
      log "ERROR: migrations directory not found at $MIGRATIONS_DIR"
      exit 1
    fi
    apply_migrations_with_psql "$PG_POD_BOOT" "$MIGRATIONS_DIR"
    exit 0
  fi
  log "ERROR: loop-runtime-api pod not found in $CORE_CONTEXT/$NAMESPACE"
  log "       (label selector app=loop-runtime-api). Is tilt up + cluster healthy?"
  exit 1
fi
log "Using loop-runtime-api pod: $LOOP_RUNTIME_API_POD"

# EC-2: Compute HIGHEST migration dynamically (NEVER hardcode).
# Reads /migrations/*.up.sql inside the pod (mounted from image build).
HIGHEST="$(kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
  sh -c "ls /migrations/*.up.sql 2>/dev/null | sort -V | tail -1 | sed -E 's|.*/0*([0-9]+)_.*|\1|'" || true)"
if [[ -z "$HIGHEST" || ! "$HIGHEST" =~ ^[0-9]+$ ]]; then
  log "ERROR: failed to detect highest migration version inside pod"
  log "       Verified mount: kubectl exec $LOOP_RUNTIME_API_POD -- ls /migrations/"
  exit 1
fi
log "Highest migration version detected: $HIGHEST"

# Detect "first run on existing DB" (schema_migrations absent BUT tenants
# table exists — meaning migrations were applied previously via legacy psql
# loop without version tracking, per pre-T1.2 behavior).
#
# Strategy: loop-runtime-api image (alpine final) has `migrate` but NOT `psql`.
# Use `migrate version` to inspect schema_migrations without needing psql.
# - exit 0 + "<N>" stdout      → schema_migrations exists with version N
# - non-zero + "no migration"  → table absent (or empty)
MIGRATE_VERSION_OUT="$(kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
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
  kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
    migrate -path /migrations -database "$DSN" force "$HIGHEST"
  log "Backfill complete: schema_migrations populated."
fi

# Apply any missing migrations (no-op if up to date).
# Bug #14 LIVE FIX (caught 2026-05-18 dogfood-drill iter 6):
# Previous code logged "checking if 'no change'" but did NOT actually check —
# always fell through to "Migrations applied successfully" log line. In drill
# DOKS first-run (empty PG, no schema_migrations, no tenants table), migrate
# up exit=1 was masked as idempotent → drill-mint-jwt later failed with
# `relation "users" does not exist` because schema was NEVER applied.
#
# Proper disambiguation: capture stdout+stderr, then on non-zero exit:
# (a) "no change" string in output → genuinely idempotent, OK
# (b) `migrate version` reports a numeric version → schema_migrations exists,
#     migrations are applied → idempotent OK
# (c) neither (a) nor (b) → real failure, exit with error
log "Running migrate up..."
if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN=1 — skipping 'migrate up'. Would have executed:"
  log "  kubectl exec $LOOP_RUNTIME_API_POD -- migrate -path /migrations -database \$DSN -verbose up"
  log "Current schema version (read-only):"
  kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
    migrate -path /migrations -database "$DSN" version 2>&1 | sed 's/^/  /' || true
  log "Exiting cleanly without applying migrations."
  exit 0
fi
MIGRATE_UP_OUT="$(kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
  migrate -path /migrations -database "$DSN" -verbose up 2>&1)" && MIGRATE_UP_EXIT=0 || MIGRATE_UP_EXIT=$?
echo "$MIGRATE_UP_OUT" | sed 's/^/  [migrate-up] /'
if [[ $MIGRATE_UP_EXIT -ne 0 ]]; then
  log "  migrate up exit=$MIGRATE_UP_EXIT — disambiguating no-change vs real failure"
  # Check (a): explicit "no change" message
  if echo "$MIGRATE_UP_OUT" | grep -qiE "no change|nothing to apply"; then
    log "  ✓ Disambiguation: 'no change' — schema already current (idempotent re-run)"
  else
    # Check (b): migrate version returns a numeric version
    POST_VERSION_OUT="$(kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
      migrate -path /migrations -database "$DSN" version 2>&1 || true)"
    # grep returns exit 1 on no match → pipefail + set -e would kill script.
    # Append `|| true` to ensure pipeline exits 0 regardless of grep result.
    POST_VERSION_NUMBER=$(echo "$POST_VERSION_OUT" | grep -oE '^[0-9]+' | head -1 || true)
    if [[ -n "$POST_VERSION_NUMBER" ]] && [[ "$POST_VERSION_NUMBER" -ge "$HIGHEST" ]]; then
      log "  ✓ Disambiguation: schema_migrations version=$POST_VERSION_NUMBER >= highest=$HIGHEST (idempotent)"
    else
      log "  ✗ Disambiguation: REAL FAILURE — migrate up exited $MIGRATE_UP_EXIT without 'no change' AND version=${POST_VERSION_NUMBER:-MISSING} < highest=$HIGHEST"
      log "  ✗ Schema NOT applied. Subsequent steps requiring tables (users, tenants) will fail."
      exit "$MIGRATE_UP_EXIT"
    fi
  fi
fi

log "Migrations applied successfully."
log "Current schema_migrations status (via migrate version):"
kctl -n "$NAMESPACE" exec "$LOOP_RUNTIME_API_POD" -- \
  migrate -path /migrations -database "$DSN" version || true
if [[ -n "$PG_POD" ]]; then
  log "Full schema_migrations table:"
  kctl -n "$NAMESPACE" exec "$PG_POD" -c postgresql -- \
    sh -c "env PGPASSWORD='${PG_PASS}' psql -U '${PG_USER}' -d '${PG_DB}' -c 'SELECT version, dirty FROM schema_migrations;'" || true
fi
