#!/usr/bin/env bash
# port-forward-public-dashboard.sh — T7.3 of plan
# 2026-05-29-dashboard-public-integration-canonicalization
#
# Persistent 4-port-forward orchestrator with auto-retry, PID file,
# signal trap, and exponential backoff. Replaces the ad-hoc
# `setsid bash -c "while true; do kubectl port-forward; sleep 2; done"`
# pattern from the 2026-05-29 LIVE session.
#
# Ports:
#   13002 → loop-console   (svc/loop-console:13002)
#   14444 → ory-hydra    (svc/ory-hydra-public:4444)
#   14433 → ory-kratos   (svc/ory-kratos-public:80)
#   14434 → ory-kratos-admin (svc/ory-kratos-admin:80)
#
# v1.1 EC-S3 (exponential backoff): kubectl crash → sleep grows 2s→4s→8s→16s→30s
# capped, so a downed cluster doesn't burn CPU + spam logs.
#
# v1.1 EC-S4 (signal handling): SIGTERM/SIGINT to parent kills all 4 children.
# Without trap, parent dies but child kubectl orphans hold ports.
#
# Usage:
#   port-forward-public-dashboard.sh start    # spawn 4 children, write PIDs
#   port-forward-public-dashboard.sh stop     # SIGTERM children + cleanup PID file
#   port-forward-public-dashboard.sh status   # show running children + ports
set -euo pipefail

CONTEXT="${CONTEXT:-k3d-loop-runtime-core}"
PID_FILE="${PID_FILE:-/tmp/loop-runtime-port-forward-public.pids}"
LOG_DIR="${LOG_DIR:-/tmp}"

# Each entry: namespace:resource:local_port:remote_port:log_suffix
#
# T2.4 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2:
# extended with Hydra-admin (14445) for J-05 AcceptLogin/AcceptConsent flow
# + dashboard vite dev server (5173) for app-dev local UI iteration.
# Closes catch #15 from T6.2 session (memory j05-aprendizado-hydra-urls-
# canonical-via-helmfile-overlay) — Hydra admin port-forward was missing
# during sponsor walkthrough debug, requiring ad-hoc kubectl invocations.
SERVICES=(
  "loop-runtime-system:svc/loop-console:13002:13002:cloud"
  "loop-runtime-auth:svc/ory-hydra-public:14444:4444:hydra-public"
  "loop-runtime-auth:svc/ory-hydra-admin:14445:4445:hydra-admin"
  "loop-runtime-auth:svc/ory-kratos-public:14433:80:kratos-public"
  "loop-runtime-auth:svc/ory-kratos-admin:14434:80:kratos-admin"
  "loop-console-dashboard:svc/loop-console-dashboard:5173:5173:dashboard-vite"
)

log() { printf '[port-fwd] %s\n' "$*" >&2; }

# v1.1 EC-S3 — exponential backoff supervisor for one port-forward
run_forever() {
  local ns="$1" resource="$2" lport="$3" rport="$4" logfile="$5"
  local sleeps=(2 4 8 16 30 30 30 30)
  local i=0
  while true; do
    # `if/then`, and not `&& i=0 || true`: the port-forward exiting successfully
    # resets the backoff; the `||` would make the `true` run also when the assignment
    # failed, masking the distinction between the two outcomes.
    if kubectl --context="$CONTEXT" -n "$ns" port-forward \
      "$resource" "$lport:$rport" >>"$logfile" 2>&1; then i=0; fi
    local sl="${sleeps[$i]}"
    [ "$i" -lt $(( ${#sleeps[@]} - 1 )) ] && i=$((i+1))
    echo "[port-fwd] $resource exited; sleeping ${sl}s before retry" >>"$logfile"
    sleep "$sl"
  done
}

cmd_start() {
  if [ -f "$PID_FILE" ] && pgrep -F "$PID_FILE" >/dev/null 2>&1; then
    log "already running (PID file $PID_FILE has live processes)"
    return 0
  fi
  : > "$PID_FILE"

  for spec in "${SERVICES[@]}"; do
    IFS=: read -r ns resource lport rport suffix <<< "$spec"
    local logfile="${LOG_DIR}/pf-${suffix}.log"
    log "starting port-forward ${ns}/${resource} ${lport}→${rport} (log: ${logfile})"
    # setsid detaches from parent shell so children survive the orchestrator script exit
    setsid bash -c "$(declare -f run_forever); run_forever '${ns}' '${resource}' ${lport} ${rport} '${logfile}'" \
      </dev/null >>/dev/null 2>&1 &
    echo $! >> "$PID_FILE"
  done

  log "wrote PIDs to $PID_FILE"
  sleep 3
  cmd_status
}

cmd_stop() {
  if [ ! -f "$PID_FILE" ]; then
    log "no PID file at $PID_FILE — nothing to stop"
    return 0
  fi
  log "stopping children"
  while read -r pid; do
    [ -z "$pid" ] && continue
    if kill -0 "$pid" 2>/dev/null; then
      # SIGTERM the process group — kills the supervisor + the kubectl child
      kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
      log "  TERM pid=$pid"
    fi
  done < "$PID_FILE"
  sleep 2
  # Force-kill survivors
  while read -r pid; do
    [ -z "$pid" ] && continue
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
      log "  KILL pid=$pid"
    fi
  done < "$PID_FILE"
  rm -f "$PID_FILE"
  log "stopped"
}

cmd_status() {
  if [ ! -f "$PID_FILE" ]; then
    log "not running (no PID file)"
    return 1
  fi
  local alive=0
  while read -r pid; do
    [ -z "$pid" ] && continue
    if kill -0 "$pid" 2>/dev/null; then
      log "  alive: pid=$pid"
      alive=$((alive+1))
    else
      log "  dead:  pid=$pid"
    fi
  done < "$PID_FILE"

  log "listening ports:"
  ss -tlnp 2>/dev/null | grep -E ":(13002|14433|14434|14444) " | sed 's/^/    /' >&2 || true

  [ "$alive" -ge 1 ]
}

case "${1:-status}" in
  start)  cmd_start ;;
  stop)   cmd_stop ;;
  status) cmd_status ;;
  *) echo "Usage: $0 {start|stop|status}" >&2; exit 1 ;;
esac
