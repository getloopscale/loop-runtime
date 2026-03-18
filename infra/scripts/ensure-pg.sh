#!/usr/bin/env bash
# ensure-pg.sh — Ensures PostgreSQL is reachable, starting a port-forward if needed.
#
# SOURCE this file, do not execute it.
# Usage: source infra/scripts/ensure-pg.sh <host> <port> <user> <pass> <db> <kube-context>
#
# After sourcing:
#   - PGPASSWORD is exported (ready for psql)
#   - If a port-forward was started, _ENSURE_PG_PID is set (empty otherwise)
#   - Caller MUST run: [ -n "$_ENSURE_PG_PID" ] && kill "$_ENSURE_PG_PID" 2>/dev/null; true
#
# Example (from Taskfile):
#   source infra/scripts/ensure-pg.sh "$HOST" "$PORT" "$USER" "$PASS" "$DB" "$CTX"
#   ... do work ...
#   [ -n "$_ENSURE_PG_PID" ] && kill "$_ENSURE_PG_PID" 2>/dev/null; true

_EPG_HOST="${1:?ensure-pg.sh: host required}"
_EPG_PORT="${2:?ensure-pg.sh: port required}"
_EPG_USER="${3:?ensure-pg.sh: user required}"
_EPG_PASS="${4:?ensure-pg.sh: pass required}"
_EPG_DB="${5:?ensure-pg.sh: db required}"
_EPG_CTX="${6:?ensure-pg.sh: kube-context required}"

export PGPASSWORD="${_EPG_PASS}"
_ENSURE_PG_PID=""

if ! psql -h "${_EPG_HOST}" -p "${_EPG_PORT}" -U "${_EPG_USER}" -d "${_EPG_DB}" -c "SELECT 1" >/dev/null 2>&1; then
  kubectl --context "${_EPG_CTX}" port-forward -n loopzero-system svc/postgresql "${_EPG_PORT}":5432 &
  _ENSURE_PG_PID=$!
  sleep 2
fi
