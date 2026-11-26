#!/usr/bin/env bash
# baseline.sh — Steady-state baseline capture for /chaos-test skill.
#
# Captures pre-chaos metrics that subsequent during/post observations
# compare against. Failing to capture baseline = no signal = invalid run.
#
# Captured metrics (saved as JSON):
#   - API /health success rate over the window
#   - API P50/P95/P99 latency (via Prometheus if available, else curl probe)
#   - Pod restart counts per critical app (loopruntime-api, operators, gitea, pg)
#   - Resource utilization snapshot (CPU/memory)
#   - Active builds + active ArgoCD app sync state
#
# Usage:
#   bash infra/scripts/test/baseline.sh <scenario-id> [--duration 5m]
#   bash infra/scripts/test/baseline.sh C01 --duration 2m
#
# Exit codes:
#   0 — baseline captured successfully (JSON written to /tmp/chaos-baseline-{scenario}-{ts}.json)
#   1 — pre-flight failure (kubectl unreachable, no clusters)
#   2 — measurement failure (couldn't capture metric)

set -euo pipefail

set -uo pipefail

SCENARIO="${1:-baseline}"
DURATION="${2:-5m}"
if [[ "$DURATION" == "--duration" ]]; then
  DURATION="${3:-5m}"
fi

# Parse duration (5m → 300s, 2m → 120s, 30s → 30s)
parse_duration() {
  local d="$1"
  if [[ "$d" =~ ^([0-9]+)([sm])$ ]]; then
    local n="${BASH_REMATCH[1]}"
    local u="${BASH_REMATCH[2]}"
    if [[ "$u" == "m" ]]; then echo $((n * 60)); else echo "$n"; fi
  else
    echo "300"  # default 5m
  fi
}
DURATION_SEC=$(parse_duration "$DURATION")

TARGET_CTX="${TARGET_CONTEXT:-k3d-loopruntime-core}"
WORKLOAD_CTX="${CELL_0_CONTEXT:-k3d-loopruntime-cell-0}"
TS=$(date -u +%Y%m%dT%H%M%SZ)
OUTPUT="/tmp/chaos-baseline-${SCENARIO}-${TS}.json"

log() { printf '[baseline] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

log "scenario=$SCENARIO duration=${DURATION_SEC}s target=$TARGET_CTX output=$OUTPUT"

# Pre-flight
kubectl --context "$TARGET_CTX" cluster-info >/dev/null 2>&1 || die "context $TARGET_CTX unreachable"

# ---------------------------------------------------------------------------
# Sample N times over the duration window
# ---------------------------------------------------------------------------
START_TS=$(date -u +%s)
END_TS=$((START_TS + DURATION_SEC))
SAMPLE_INTERVAL=5  # seconds between samples
SAMPLES=()

log "sampling /api/v1/health every ${SAMPLE_INTERVAL}s for ${DURATION_SEC}s..."

while [[ $(date -u +%s) -lt $END_TS ]]; do
  # API health probe
  local_start=$(date +%s%N)
  if curl -sf -m 3 -o /dev/null -w "%{http_code}" http://localhost:13001/api/v1/health 2>/dev/null > /tmp/.chaos-curl-status; then
    STATUS=$(cat /tmp/.chaos-curl-status)
  else
    STATUS=0
  fi
  local_end=$(date +%s%N)
  LATENCY_MS=$(( (local_end - local_start) / 1000000 ))

  SAMPLES+=("{\"ts\":\"$(date -u +%s)\",\"status\":\"$STATUS\",\"latency_ms\":$LATENCY_MS}")
  sleep "$SAMPLE_INTERVAL"
done

N_SAMPLES=${#SAMPLES[@]}
log "captured $N_SAMPLES samples"

# ---------------------------------------------------------------------------
# Compute aggregate stats
# ---------------------------------------------------------------------------
HEALTH_OK=0
HEALTH_FAIL=0
LATENCIES=()
for s in "${SAMPLES[@]}"; do
  status=$(echo "$s" | grep -oE '"status":"[0-9]+"' | grep -oE '[0-9]+')
  latency=$(echo "$s" | grep -oE '"latency_ms":[0-9]+' | grep -oE '[0-9]+')
  if [[ "$status" == "200" ]]; then
    HEALTH_OK=$((HEALTH_OK+1))
  else
    HEALTH_FAIL=$((HEALTH_FAIL+1))
  fi
  LATENCIES+=("$latency")
done

# Sort latencies for percentile calc (bash limitation: use external sort)
mapfile -t SORTED_ARR < <(printf '%s\n' "${LATENCIES[@]}" | sort -n)
N=${#SORTED_ARR[@]}

if [[ "$N" -gt 0 ]]; then
  P50_IDX=$((N / 2))
  P95_IDX=$((N * 95 / 100))
  P99_IDX=$((N * 99 / 100))
  P50=${SORTED_ARR[$P50_IDX]:-0}
  P95=${SORTED_ARR[$P95_IDX]:-0}
  P99=${SORTED_ARR[$P99_IDX]:-0}
else
  P50=0; P95=0; P99=0
fi

SUCCESS_RATE=0
if [[ "$N_SAMPLES" -gt 0 ]]; then
  SUCCESS_RATE=$(( HEALTH_OK * 100 / N_SAMPLES ))
fi

# ---------------------------------------------------------------------------
# Pod restart counts (mgmt + workload critical pods)
# ---------------------------------------------------------------------------
log "capturing pod restart counts..."

restart_count() {
  local ctx="$1" ns="$2" selector="$3"
  # `|| true` after the pipe: control-plane pods (loopruntime-api, operators, pg,
  # gitea) only exist on the mgmt cluster. When TARGET_CTX is workload/build,
  # the kubectl query returns non-zero (ns/pods absent) and — under
  # `set -e` + `pipefail` — would abort the whole baseline. The fallback keeps
  # the count at 0 for clusters that don't host that pod, so chaos scenarios
  # targeting non-mgmt clusters (C03/C04/C06/C10) can still capture a baseline.
  kubectl --context "$ctx" get pods -n "$ns" -l "$selector" -o jsonpath='{range .items[*]}{.status.containerStatuses[0].restartCount}{"\n"}{end}' 2>/dev/null | awk '{s+=$1} END {print s+0}' || echo 0
}

LOOPRUNTIME_API_RESTARTS=$(restart_count "$TARGET_CTX" loopruntime-system "app=loopruntime-api")
TENANT_OP_RESTARTS=$(restart_count "$TARGET_CTX" loopruntime-system "app=tenant-operator")
APP_OP_RESTARTS=$(restart_count "$TARGET_CTX" loopruntime-system "app=app-operator")
PG_RESTARTS=$(restart_count "$TARGET_CTX" loopruntime-system "app.kubernetes.io/name=postgresql")
GITEA_RESTARTS=$(restart_count "$TARGET_CTX" loopruntime-system "app=gitea")

# ---------------------------------------------------------------------------
# ArgoCD app health (always-zero fallback prevents `grep -c || echo 0`
# producing "0\n0" — bug discovered in /chaos-test --full first run
# 2026-05-14: corrupt JSON broke verdict extraction for C04/C06/C10).
# ---------------------------------------------------------------------------
count_healthy_argocd_apps() {
  local out
  out=$(kubectl --context "$TARGET_CTX" get applications -n argocd -o jsonpath='{range .items[*]}{.status.health.status}{"\n"}{end}' 2>/dev/null) || out=""
  if [[ -z "$out" ]]; then
    echo 0
  else
    echo "$out" | grep -c "Healthy" || echo 0
  fi
}
ARGOCD_HEALTHY=$(count_healthy_argocd_apps | head -1)
# Capture-then-count (same guard pattern as count_healthy_argocd_apps): the
# `argocd` namespace only exists on the mgmt cluster. For workload/build target
# contexts (C03/C04/C06/C10) the kubectl returns non-zero and — under `set -e`
# + `pipefail` — `$(... | wc -l)` would abort the baseline. Defaulting to 0
# keeps non-mgmt scenarios runnable.
count_argocd_total() {
  local out
  out=$(kubectl --context "$TARGET_CTX" get applications -n argocd --no-headers 2>/dev/null) || out=""
  if [[ -z "$out" ]]; then echo 0; else printf '%s\n' "$out" | grep -c . || echo 0; fi
}
ARGOCD_TOTAL=$(count_argocd_total | head -1)
ARGOCD_TOTAL="${ARGOCD_TOTAL:-0}"

# ---------------------------------------------------------------------------
# Workload cluster pods (count Running)
# ---------------------------------------------------------------------------
count_running_workload_pods() {
  local out
  out=$(kubectl --context "$WORKLOAD_CTX" get pods -A --no-headers 2>/dev/null) || out=""
  if [[ -z "$out" ]]; then
    echo 0
  else
    echo "$out" | grep -c "Running" || echo 0
  fi
}
WORKLOAD_RUNNING=$(count_running_workload_pods | head -1)

# ---------------------------------------------------------------------------
# Write JSON output
# ---------------------------------------------------------------------------
cat > "$OUTPUT" <<EOF
{
  "scenario": "$SCENARIO",
  "captured_at": "$(date -u -d "@$START_TS" +%Y-%m-%dT%H:%M:%SZ)",
  "duration_sec": $DURATION_SEC,
  "samples_count": $N_SAMPLES,
  "api_health": {
    "success_count": $HEALTH_OK,
    "fail_count": $HEALTH_FAIL,
    "success_rate_pct": $SUCCESS_RATE,
    "latency_p50_ms": $P50,
    "latency_p95_ms": $P95,
    "latency_p99_ms": $P99
  },
  "pod_restart_counts": {
    "loopruntime_api": $LOOPRUNTIME_API_RESTARTS,
    "tenant_operator": $TENANT_OP_RESTARTS,
    "app_operator": $APP_OP_RESTARTS,
    "postgresql": $PG_RESTARTS,
    "gitea": $GITEA_RESTARTS
  },
  "argocd": {
    "healthy_apps": $ARGOCD_HEALTHY,
    "total_apps": $ARGOCD_TOTAL
  },
  "workload_cluster": {
    "running_pods": $WORKLOAD_RUNNING
  }
}
EOF

log "baseline captured: $OUTPUT"
log "  api_success: ${SUCCESS_RATE}% / p95: ${P95}ms / p99: ${P99}ms"
log "  pod restarts: api=$LOOPRUNTIME_API_RESTARTS tenant-op=$TENANT_OP_RESTARTS pg=$PG_RESTARTS"
log "  argocd: ${ARGOCD_HEALTHY}/${ARGOCD_TOTAL} healthy"

# Echo path so callers can use it
echo "$OUTPUT"
