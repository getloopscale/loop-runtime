#!/usr/bin/env bash
# stop-themory.sh — graceful shutdown of TheMemory native process via PID file.
#
# Per plan v1.3 § Phase 1 / T1.1 of loop-console-themory-e2e-finalize-plan.md.
# Companion to start-themory.sh.
#
# Sequence:
#   1. Read PID file (no-op + exit 0 if absent — already stopped)
#   2. Send SIGTERM; wait up to 5s for graceful exit
#   3. Send SIGKILL if still alive
#   4. Remove PID file
#
# Bootstrap token (.themory-bootstrap-token) is NOT removed — it's tied to the
# server-process lifetime; next start-themory.sh overwrites with new random.

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PID_FILE="${SCRIPT_DIR}/.themory.pid"
readonly TERM_TIMEOUT="${TERM_TIMEOUT:-5}"

readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

# Logs to stderr for consistency with start-themory.sh (parsers don't get colors)
log_info() { printf "%b[INFO]%b %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn() { printf "%b[WARN]%b %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }

main() {
  if [ ! -f "$PID_FILE" ]; then
    log_info "PID file not found — TheMemory already stopped"
    exit 0
  fi

  local pid
  pid="$(cat "$PID_FILE")"

  if ! kill -0 "$pid" 2>/dev/null; then
    log_warn "PID file pointed to dead process ($pid); cleaning up"
    rm -f "$PID_FILE"
    exit 0
  fi

  log_info "Sending SIGTERM to TheMemory (PID $pid)"
  kill -TERM "$pid" 2>/dev/null || true

  local elapsed=0
  while [ "$elapsed" -lt "$TERM_TIMEOUT" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      log_info "TheMemory exited gracefully"
      rm -f "$PID_FILE"
      exit 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  log_warn "Graceful exit timed out after ${TERM_TIMEOUT}s; sending SIGKILL"
  kill -KILL "$pid" 2>/dev/null || true
  sleep 1
  rm -f "$PID_FILE"
  log_info "TheMemory force-killed"
}

main "$@"
