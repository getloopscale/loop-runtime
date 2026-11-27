#!/usr/bin/env bash
# start-themory.sh — launch TheMemory as a native host process for the dev loop.
#
# Per plan v1.3 § Phase 1 / T1.1 of loop-console-themory-e2e-finalize-plan.md.
# Idempotent (skip if PID file present + process alive), fail-loud on real errors.
#
# Sequence:
#   1. Pre-flight: refuse if .themory.pid already points at a live process (EC-9)
#   2. Create themory_dev database if not exists (EC-3 idempotent + fail-loud)
#   3. Run migrations: node "$THEMORY_CLI" migrate
#   4. Export env (incl. random THEMEMORY_ADMIN_BOOTSTRAP_TOKEN per session)
#   5. nohup-exec `themory server`; capture PID; disown (EC-8)
#   6. Wait up to 30s for /v1/health 200
#   7. Print ready URL + exit 0
#
# Companion: stop-themory.sh (graceful SIGTERM via PID file)
# Logs: infra/scripts/local/.themory.log (tail -f for live output)
# Bootstrap token: infra/scripts/local/.themory-bootstrap-token (chmod 600)
#   Consumed by bootstrap-themory-key.sh (T2.1) for the FIRST keys-create invocation.

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PID_FILE="${SCRIPT_DIR}/.themory.pid"
readonly LOG_FILE="${SCRIPT_DIR}/.themory.log"
readonly BOOTSTRAP_TOKEN_FILE="${SCRIPT_DIR}/.themory-bootstrap-token"
readonly ENV_FILE="${SCRIPT_DIR}/.themory.env"

# Source operator dotenv if present (PGURL, OPENAI_API_KEY, OPENAI_BASE_URL, LOOP_RUNTIMEMEM_LLM_PROVIDER, etc.)
# Format: KEY=value, one per line, no quotes. Gitignored per .gitignore.
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

readonly PGURL="${PGURL:-postgresql://localhost:5432/postgres}"
readonly PGURL_BASE="${PGURL_BASE:-postgresql://localhost:5432}"
readonly THEMORY_CLI="${THEMORY_CLI:-$(realpath -m "${SCRIPT_DIR}/../../../../../loop-runtime-memory/dist/agent-tools/cli.js")}"
readonly THEMORY_PORT="${THEMORY_PORT:-8080}"
readonly THEMORY_HOST="${THEMORY_HOST:-0.0.0.0}"
readonly HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-30}"

readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

# Logs go to stderr so functions returning data via stdout are not contaminated
# by command substitution `$(...)` swallowing log lines as data.
log_info()  { printf "%b[INFO]%b  %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn()  { printf "%b[WARN]%b  %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }
log_fatal() { printf "%b[FATAL]%b %s\n" "$C_RED" "$C_RESET" "$1" >&2; exit 1; }

check_idempotent_pid() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    log_info "TheMemory already running (PID $(cat "$PID_FILE")); skipping start"
    exit 0
  fi
  if [ -f "$PID_FILE" ]; then
    log_warn "stale PID file detected; removing"
    rm -f "$PID_FILE"
  fi
}

ensure_db_exists() {
  if psql "$PGURL" -lqt 2>/dev/null | cut -d \| -f 1 | grep -qw themory_dev; then
    log_info "Database themory_dev already exists"
    return 0
  fi
  log_info "Creating database themory_dev"
  # CREATE DATABASE via psql (portable; createdb's URL parsing is implementation-defined).
  psql "$PGURL" -c 'CREATE DATABASE themory_dev' >/dev/null \
    || log_fatal "CREATE DATABASE failed — check PG access at $PGURL (psql -lqt to inspect)"
}

ensure_cli_present() {
  if [ ! -f "$THEMORY_CLI" ]; then
    log_fatal "TheMemory CLI not found at $THEMORY_CLI (cd ../loop-runtime-memory && npm run build OR set THEMORY_CLI env)"
  fi
}

run_migrations() {
  log_info "Running TheMemory migrations against themory_dev"
  LOOP_RUNTIMEMEM_PG_URI="${PGURL_BASE}/themory_dev" node "$THEMORY_CLI" migrate \
    || log_fatal "themory migrate FAILED — see error above"
}

generate_bootstrap_token() {
  local token
  token="$(openssl rand -hex 32)"
  echo "$token" > "$BOOTSTRAP_TOKEN_FILE"
  chmod 600 "$BOOTSTRAP_TOKEN_FILE"
  log_info "Bootstrap token written to $BOOTSTRAP_TOKEN_FILE (chmod 600)"
  echo "$token"
}

start_server() {
  local bootstrap_token="$1"
  log_info "Starting themory server on ${THEMORY_HOST}:${THEMORY_PORT}"
  # OPENAI_API_KEY + OPENAI_BASE_URL + LOOP_RUNTIMEMEM_LLM_PROVIDER are inherited from
  # the caller's shell OR sourced from .themory.env above. Required by the LLM
  # extraction step in TheMemory's write pipeline.
  LOOP_RUNTIMEMEM_PG_URI="${PGURL_BASE}/themory_dev" \
  LOOP_RUNTIMEMEM_PORT="$THEMORY_PORT" \
  LOOP_RUNTIMEMEM_HOST="$THEMORY_HOST" \
  THEMORY_ADMIN_BOOTSTRAP_TOKEN="$bootstrap_token" \
    nohup node "$THEMORY_CLI" server >"$LOG_FILE" 2>&1 &
  echo "$!" > "$PID_FILE"
  disown
}

wait_for_health() {
  local elapsed=0
  while [ "$elapsed" -lt "$HEALTH_TIMEOUT" ]; do
    if curl -fsS "http://localhost:${THEMORY_PORT}/v1/health" >/dev/null 2>&1; then
      log_info "TheMemory ready at http://localhost:${THEMORY_PORT}"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  log_fatal "TheMemory failed health probe after ${HEALTH_TIMEOUT}s — inspect $LOG_FILE"
}

main() {
  printf "start-themory.sh — bootstrap + launch TheMemory native process\n\n"
  check_idempotent_pid
  ensure_cli_present
  ensure_db_exists
  run_migrations
  local token
  token="$(generate_bootstrap_token)"
  start_server "$token"
  wait_for_health
  printf "\nNext: bash infra/scripts/local/bootstrap-themory-key.sh\n"
  printf "Tail logs: tail -f %s\n" "$LOG_FILE"
  printf "Stop:      bash infra/scripts/local/stop-themory.sh\n"
}

main "$@"
