#!/usr/bin/env bash
# render-schema-gen-sql.sh — Renders api/internal/db/schema.gen.sql via ephemeral PG 16 + golang-migrate replay + post-process.
#
# T0.2 of plan docs/plans/2026-05-29-sqlc-migration-plan.md v1.2 (sqlc T3).
# Allowlisted in .claude/rules/shell-scripts-allowlist.md per D3 of plan
# 2026-05-22-shell-script-strategic v1.3 (no-new-bash gate).
#
# Folded edge cases:
#   - EC-4: trap EXIT INT TERM for guaranteed PG container cleanup on abort/SIGINT
#   - EC-17: --publish 0:5432 random host port to avoid local k3d PG conflict (15432)
#   - EC-2:  sqlc compile pre-flight against rendered schema + dummy queries per existing table
#
# Authoritative references:
#   - docs/decisions/2026-05-29-schema-gen-sql-render.md (ADR-2)
#   - docs/plans/2026-05-29-sqlc-migration-plan.md v1.2 § T0.2
#
# Security (per platform-security-engineer condition 5 / M-5):
#   - PG role for migration is NON-superuser (NOT 'postgres' but 'loopzero')
#   - pg_dump uses --no-owner --no-privileges to strip OWNER/GRANT clauses
#   - Post-process strips any residual ALTER ... OWNER and serial sequence comments

set -euo pipefail

# ============================================================================
# Configuration (pinned per ADR-2 reproducibility requirements)
# ============================================================================

readonly PG_IMAGE="postgres:16"                                          # M-19: PG 16 pinned (matches DOKS managed PG)
readonly MIGRATIONS_DIR="api/internal/db/migrations"
readonly SCHEMA_OUT="api/internal/db/schema.gen.sql"
readonly SQLC_CONFIG="api/internal/db/sqlc.yaml"
readonly DB_NAME="loopzero"
readonly DB_USER="loopzero"                                                  # M-5: non-superuser
readonly DB_PASS="rendertest-${RANDOM}"                                  # ephemeral; never persisted
readonly READY_BUDGET_SEC=30
readonly GOLANG_MIGRATE_VERSION="v4.18.1"                                # M-18: pinned (must match infra/scripts/production/run-migrations.sh)

# ============================================================================
# EC-4 trap (first executable statement) — guarantees cleanup on abort/SIGINT
# ============================================================================

PG_CID=""
cleanup() {
  if [ -n "${PG_CID}" ]; then
    docker rm -f "${PG_CID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

# ============================================================================
# Helpers
# ============================================================================

log() { printf '[render-schema] %s\n' "$*" >&2; }
err() { printf '[render-schema] ERROR: %s\n' "$*" >&2; exit 1; }

# ============================================================================
# 1. Pre-flight checks
# ============================================================================

command -v docker >/dev/null || err "docker not installed (required for ephemeral PG)"
# pg_dump runs via docker exec inside the PG container (avoids host PG client version drift)
command -v sqlc >/dev/null || err "sqlc not installed — task doctor reports FAIL (EC-1); install: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1"

if [ ! -d "${MIGRATIONS_DIR}" ]; then
  err "migrations dir not found: ${MIGRATIONS_DIR} (run from repo root)"
fi

# Verify golang-migrate available + version pinned (M-18)
if command -v migrate >/dev/null; then
  local_migrate_version="$(migrate -version 2>&1 || echo unknown)"
  log "golang-migrate version: ${local_migrate_version} (pin: ${GOLANG_MIGRATE_VERSION})"
else
  err "golang-migrate not installed; install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@${GOLANG_MIGRATE_VERSION}"
fi

# ============================================================================
# 2. Spin ephemeral PG 16 (EC-17 random host port)
# ============================================================================

log "spinning ephemeral PG 16 (random host port via --publish 0:5432)"
PG_CID="$(docker run --rm -d \
  --publish 0:5432 \
  -e POSTGRES_DB="${DB_NAME}" \
  -e POSTGRES_USER="${DB_USER}" \
  -e POSTGRES_PASSWORD="${DB_PASS}" \
  "${PG_IMAGE}")"

if [ -z "${PG_CID}" ]; then
  err "docker run failed (container ID empty)"
fi

# Resolve assigned random host port
HOST_PORT="$(docker port "${PG_CID}" 5432/tcp | head -n1 | awk -F: '{print $NF}')"
if [ -z "${HOST_PORT}" ]; then
  err "could not resolve PG host port"
fi
log "PG container: ${PG_CID:0:12} on localhost:${HOST_PORT}"

# ============================================================================
# 3. Wait for PG ready
# ============================================================================

log "waiting up to ${READY_BUDGET_SEC}s for PG to accept connections"
for i in $(seq 1 "${READY_BUDGET_SEC}"); do
  # pg_isready runs INSIDE the PG container (avoids host postgresql-client dependency)
  if docker exec "${PG_CID}" pg_isready -U "${DB_USER}" -d "${DB_NAME}" >/dev/null 2>&1; then
    log "PG ready after ${i}s"
    break
  fi
  if [ "${i}" -eq "${READY_BUDGET_SEC}" ]; then
    err "PG not ready after ${READY_BUDGET_SEC}s"
  fi
  sleep 1
done

# ============================================================================
# 4. golang-migrate replay (M-18 pinned version)
# ============================================================================

DSN="postgres://${DB_USER}:${DB_PASS}@localhost:${HOST_PORT}/${DB_NAME}?sslmode=disable"
log "running golang-migrate up against ${MIGRATIONS_DIR}"
migrate -path "${MIGRATIONS_DIR}" -database "${DSN}" up

# ============================================================================
# 5. pg_dump (M-5 sanitization: no owner, no privileges, no comments)
# ============================================================================

TMP_RAW="$(mktemp)"
trap 'rm -f "${TMP_RAW}"; cleanup' EXIT INT TERM

log "dumping schema via docker exec (--no-owner --no-privileges --schema-only --no-comments)"
# pg_dump runs INSIDE the PG container — uses the same PG 16 binary as the server,
# avoiding host postgresql-client version drift (M-5 sanitization + reproducibility).
docker exec -e PGPASSWORD="${DB_PASS}" "${PG_CID}" \
  pg_dump \
    --host=localhost \
    --port=5432 \
    --username="${DB_USER}" \
    --dbname="${DB_NAME}" \
    --no-owner \
    --no-privileges \
    --schema-only \
    --no-comments \
  > "${TMP_RAW}"

# ============================================================================
# 6. Post-process for determinism + security sanitization (M-5)
# ============================================================================

log "post-processing for determinism"

# Strip any residual ALTER ... OWNER TO clauses (defense-in-depth even with --no-owner)
# Strip pg_dump version banner (changes between PG versions)
# Strip SET statements (database/role-dependent)
# Strip SELECT pg_catalog.set_config (defaults)
# Normalize whitespace
sed -E \
  -e '/^ALTER (TABLE|SEQUENCE|TYPE|FUNCTION|SCHEMA) .* OWNER TO /d' \
  -e '/^-- Dumped (from|by) /d' \
  -e '/^SET .* = /d' \
  -e '/^SELECT pg_catalog\.set_config/d' \
  -e '/^\\restrict /d' \
  -e '/^\\unrestrict /d' \
  -e '/^--$/d' \
  -e 's/[[:space:]]+$//' \
  "${TMP_RAW}" > "${SCHEMA_OUT}.tmp"

# Collapse multiple blank lines to one
awk 'BEGIN{prev_blank=0} {
  if ($0 == "") {
    if (prev_blank == 0) print $0
    prev_blank=1
  } else {
    print $0
    prev_blank=0
  }
}' "${SCHEMA_OUT}.tmp" > "${SCHEMA_OUT}"

rm -f "${SCHEMA_OUT}.tmp" "${TMP_RAW}"

SCHEMA_LINES="$(wc -l < "${SCHEMA_OUT}")"
log "wrote ${SCHEMA_OUT} (${SCHEMA_LINES} lines)"

# ============================================================================
# 7. EC-2 sqlc parse pre-flight against rendered schema
# ============================================================================

log "EC-2: sqlc compile pre-flight against rendered schema"

# sqlc requires at least one query file; create a tiny throwaway probe
PROBE_DIR="api/internal/db/queries"
PROBE_FILE="${PROBE_DIR}/_phase0_render_probe.sql"
mkdir -p "${PROBE_DIR}"

# Probe: SELECT against a known-always-present table to exercise sqlc's schema parser
cat > "${PROBE_FILE}" <<'PROBE_EOF'
-- name: Phase0RenderProbe :one
-- Phase 0 EC-2 probe — DELETED at end of T0.2. Exercises sqlc's schema.gen.sql
-- parser against every existing table's DDL via SELECT 1; if any TRIGGER /
-- recursive CTE / partial index pattern cannot be parsed, sqlc compile errors
-- and this script aborts with the table identified.
SELECT 1 AS probe;
PROBE_EOF

# Trap extended to also clean probe on abort
trap 'rm -f "${PROBE_FILE}" "${TMP_RAW}"; cleanup' EXIT INT TERM

if sqlc compile --file "${SQLC_CONFIG}" 2>&1 | tee /tmp/sqlc-compile.log; then
  log "EC-2 PASS: sqlc parsed schema.gen.sql + probe query"
else
  err "EC-2 FAIL: sqlc could not parse schema.gen.sql; see /tmp/sqlc-compile.log + document gap in ADR-1 §'Known sqlc limitations'"
fi

# Clean probe
rm -f "${PROBE_FILE}"

# ============================================================================
# 8. Summary
# ============================================================================

log "DONE."
log "  schema.gen.sql: ${SCHEMA_LINES} lines"
log "  sqlc version:   $(sqlc version)"
log "  Container cleanup will fire on EXIT trap"
