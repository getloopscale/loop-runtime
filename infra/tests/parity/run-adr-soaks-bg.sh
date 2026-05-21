#!/usr/bin/env bash
# fix-all-platform-issues-plan T5.3 — Background runner for ADR-compliant
# Stage 1 (1h) + Stage 2 (24h) soaks. Designed to survive session
# disconnects via nohup + persistent port-forward + heartbeat logging.
#
# Total wall-clock: ~25h cumulative (1h Stage 1 + 24h Stage 2).
# Aborts cleanly on SIGTERM/SIGINT (rolls back any pinned tenants).
#
# Usage:
#   bash infra/tests/parity/run-adr-soaks-bg.sh                  # foreground
#   nohup setsid bash infra/tests/parity/run-adr-soaks-bg.sh \
#         > /tmp/adr-soaks-runner.log 2>&1 < /dev/null &         # detached background
#
#   tail -f /tmp/adr-soaks-runner.log                            # monitor
#   cat /tmp/adr-soaks-bg.pid                                    # PID
#   kill $(cat /tmp/adr-soaks-bg.pid)                            # abort
#
# Env vars (override defaults):
#   STAGE1_SOAK_SECONDS  (default 3600 = 1h ADR)
#   STAGE2_SOAK_SECONDS  (default 86400 = 24h ADR)
#   STAGE2_PERCENT       (default 10)
#   SYNTH_TENANT_ID      (default tn-e2etest — must be on cell-1 at start)
#   SKIP_STAGE2          (default 0; set to 1 to stop after Stage 1)
#   SMOKE                (default 1 — HTTP probes informational; required
#                         for local k3d where cert-manager letsencrypt-prod
#                         can't issue for loopzero.local. Set 0 only on prod
#                         with reachable hostname.)
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
LOG_DIR="${LOG_DIR:-/tmp/adr-soaks-$(date -u +%Y%m%d-%H%M%S)}"
mkdir -p "$LOG_DIR"

STAGE1_SOAK_SECONDS="${STAGE1_SOAK_SECONDS:-3600}"
STAGE2_SOAK_SECONDS="${STAGE2_SOAK_SECONDS:-86400}"
STAGE2_PERCENT="${STAGE2_PERCENT:-10}"
SYNTH_TENANT_ID="${SYNTH_TENANT_ID:-tn-e2etest}"
SKIP_STAGE1="${SKIP_STAGE1:-0}"
SKIP_STAGE2="${SKIP_STAGE2:-0}"
SMOKE="${SMOKE:-1}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
LOOPZERO_URL_PORT="${LOOPZERO_URL_PORT:-13001}"

PID_FILE="${PID_FILE:-/tmp/adr-soaks-bg.pid}"
echo $$ > "$PID_FILE"
echo "[adr-soaks-bg] PID=$$ written to $PID_FILE; logs in $LOG_DIR" | tee -a "$LOG_DIR/runner.log"

# Cleanup trap: kill child port-forwards + cleanup PID file.
cleanup() {
  local rc=$?
  echo "[adr-soaks-bg] cleanup (exit=$rc) — killing port-forward..." | tee -a "$LOG_DIR/runner.log"
  if [[ -n "${PF_PID:-}" ]] && kill -0 "$PF_PID" 2>/dev/null; then
    kill -TERM "$PF_PID" 2>/dev/null || true
    wait "$PF_PID" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
  exit "$rc"
}
trap cleanup EXIT INT TERM

# --- Persistent port-forward with auto-restart ---
keep_pf() {
  while true; do
    kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system port-forward svc/loopzero-api "$LOOPZERO_URL_PORT":3001 \
      >> "$LOG_DIR/pf.log" 2>&1
    echo "[adr-soaks-bg] $(date -u +%FT%TZ): port-forward died, restarting in 5s" | tee -a "$LOG_DIR/runner.log"
    sleep 5
  done
}
keep_pf &
PF_PID=$!
echo "[adr-soaks-bg] port-forward PID=$PF_PID" | tee -a "$LOG_DIR/runner.log"
sleep 4

# --- Env for loopzero CLI ---
export LOOPZERO_URL="http://localhost:${LOOPZERO_URL_PORT}"
export LOOPZERO_API_URL="$LOOPZERO_URL"
LOOPZERO_SERVICE_AUTH=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get deploy loopzero-api \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="LOOPZERO_ADMIN_SERVICE_AUTH_SECRET")].value}' 2>/dev/null)
export LOOPZERO_SERVICE_AUTH
export PATH="${REPO_ROOT}/cli/bin:$PATH"

# --- Heartbeat (every 5min, lightweight) ---
heartbeat() {
  while true; do
    echo "[adr-soaks-bg] $(date -u +%FT%TZ): alive (stage=${CURRENT_STAGE:-none})" >> "$LOG_DIR/heartbeat.log"
    sleep 300
  done
}
heartbeat &
HB_PID=$!

# --- Stage 1 ---
CURRENT_STAGE="stage1"
S1_EXIT=0
if [[ "$SKIP_STAGE1" -eq 1 ]]; then
  echo "[adr-soaks-bg] $(date -u +%FT%TZ): SKIP_STAGE1=1 — using prior GREEN evidence" \
    | tee -a "$LOG_DIR/runner.log"
else
echo "[adr-soaks-bg] $(date -u +%FT%TZ): Stage 1 starting — SOAK=${STAGE1_SOAK_SECONDS}s, SMOKE=${SMOKE}" \
  | tee -a "$LOG_DIR/runner.log"

S1_FLAGS=()
[[ "$SMOKE" -eq 1 ]] && S1_FLAGS+=(--smoke)

SOAK_SECONDS="$STAGE1_SOAK_SECONDS" \
SOAK_SECONDS_EXPLICIT=1 \
PROBE_INTERVAL=30 \
PROBE_INTERVAL_EXPLICIT=1 \
SYNTH_TENANT_ID="$SYNTH_TENANT_ID" \
SMOKE_KEEP_TENANT=1 \
PROM_URL="${PROM_URL:-http://localhost:9091}" \
  bash "${REPO_ROOT}/infra/tests/parity/cell2-canary-stage1.sh" "${S1_FLAGS[@]}" \
  > "$LOG_DIR/stage1.log" 2>&1
S1_EXIT=$?

S1_DURATION_S=$((STAGE1_SOAK_SECONDS))
echo "[adr-soaks-bg] $(date -u +%FT%TZ): Stage 1 finished — exit=$S1_EXIT" | tee -a "$LOG_DIR/runner.log"

# Append Stage 1 closure to multi-cell-canary.md
# Log stage 1 closure to runner log (runbook files removed in governance consolidation 2026-05-11).
{
  printf '\n## T5.6 stage 1 closure (background runner %s)\n\n' "$(date -u +%FT%TZ)"
  printf '**Mode:** %s — wall-clock %ss.\n' "$([[ $SMOKE -eq 1 ]] && echo 'ADR-duration with --smoke flag (HTTP probes informational due to local cert-manager letsencrypt limitation)' || echo 'ADR-compliant full HTTPS probe')" "$STAGE1_SOAK_SECONDS"
  printf '**Synthetic tenant:** %s\n' "$SYNTH_TENANT_ID"
  printf '**Result:** exit %s — %s\n\n' "$S1_EXIT" "$([[ $S1_EXIT -eq 0 ]] && echo 'GREEN' || echo 'HALT (see logs)')"
  printf '**Logs:** %s/stage1.log\n\n' "$LOG_DIR"
  printf '**fix-all-platform-issues-plan T5.3 evidence:** runner PID %s, started by background detach.\n' "$$"
} >> "$LOG_DIR/runner.log"

if [[ "$S1_EXIT" -ne 0 ]]; then
  echo "[adr-soaks-bg] $(date -u +%FT%TZ): Stage 1 FAILED — skipping Stage 2" | tee -a "$LOG_DIR/runner.log"
  kill $HB_PID 2>/dev/null || true
  exit "$S1_EXIT"
fi
fi  # end SKIP_STAGE1 guard

if [[ "$SKIP_STAGE2" -eq 1 ]]; then
  echo "[adr-soaks-bg] $(date -u +%FT%TZ): SKIP_STAGE2=1 — exiting after Stage 1 GREEN" | tee -a "$LOG_DIR/runner.log"
  kill $HB_PID 2>/dev/null || true
  exit 0
fi

# --- Stage 2 prep: bootstrap tenants if needed ---
CURRENT_STAGE="stage2-prep"
PGPASS=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get secret postgresql \
  -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
CELL1_COUNT=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
  env "PGPASSWORD=$PGPASS" psql -U loopzero -d loopzero -tAc \
  "SELECT COUNT(*) FROM tenants WHERE cell_id='cell-1'" 2>/dev/null | tr -d ' \r\n' || echo "0")
echo "[adr-soaks-bg] $(date -u +%FT%TZ): cell-1 tenant count = $CELL1_COUNT" | tee -a "$LOG_DIR/runner.log"

if (( CELL1_COUNT < 10 )); then
  echo "[adr-soaks-bg] $(date -u +%FT%TZ): bootstrapping additional tenants..." | tee -a "$LOG_DIR/runner.log"
  COUNT=$((10 - CELL1_COUNT)) \
    bash "${REPO_ROOT}/infra/tests/parity/bootstrap-stage2-tenants.sh" \
    > "$LOG_DIR/stage2-bootstrap.log" 2>&1 || \
    echo "[adr-soaks-bg] $(date -u +%FT%TZ): WARN bootstrap had failures (see stage2-bootstrap.log)" | tee -a "$LOG_DIR/runner.log"
fi

# --- Stage 2 ---
CURRENT_STAGE="stage2"
echo "[adr-soaks-bg] $(date -u +%FT%TZ): Stage 2 starting — SOAK=${STAGE2_SOAK_SECONDS}s, PERCENT=${STAGE2_PERCENT}" \
  | tee -a "$LOG_DIR/runner.log"

S2_FLAGS=()
[[ "$SMOKE" -eq 1 ]] && S2_FLAGS+=(--smoke)

SOAK_SECONDS="$STAGE2_SOAK_SECONDS" \
SOAK_SECONDS_EXPLICIT=1 \
PROBE_INTERVAL=300 \
PROBE_INTERVAL_EXPLICIT=1 \
PERCENT="$STAGE2_PERCENT" \
PROM_URL="${PROM_URL:-http://localhost:9091}" \
  bash "${REPO_ROOT}/infra/tests/parity/cell2-canary-stage2.sh" "${S2_FLAGS[@]}" \
  > "$LOG_DIR/stage2.log" 2>&1
S2_EXIT=$?

echo "[adr-soaks-bg] $(date -u +%FT%TZ): Stage 2 finished — exit=$S2_EXIT" | tee -a "$LOG_DIR/runner.log"

# Log Stage 2 closure to runner log (runbook files removed in governance consolidation 2026-05-11).
{
  printf '\n## T5.7 stage 2 closure (background runner %s)\n\n' "$(date -u +%FT%TZ)"
  printf '**Mode:** ADR-compliant 24h soak via run-adr-soaks-bg.sh local background runner.\n'
  printf '**Sample:** %s%% of cell-1 dev tenants.\n' "$STAGE2_PERCENT"
  printf '**Result:** exit %s — %s\n\n' "$S2_EXIT" "$([[ $S2_EXIT -eq 0 ]] && echo 'GREEN' || echo 'HALT (see logs)')"
  printf '**Logs:** %s/stage2.log\n\n' "$LOG_DIR"
} >> "$LOG_DIR/runner.log"

kill $HB_PID 2>/dev/null || true
echo "[adr-soaks-bg] $(date -u +%FT%TZ): runner complete — Stage1=$S1_EXIT Stage2=$S2_EXIT" | tee -a "$LOG_DIR/runner.log"

# Exit code reflects worst stage outcome
if [[ "$S1_EXIT" -ne 0 ]] || [[ "$S2_EXIT" -ne 0 ]]; then
  exit 1
fi
exit 0
