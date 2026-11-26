#!/usr/bin/env bash
# run-full.sh — Run ALL 10 chaos scenarios sequentially with summary report.
#
# Maps each scenario to its appropriate target cluster (mgmt/build/workload).
# Captures per-scenario verdict + accumulates totals. Generates final
# Markdown summary across all scenarios.
#
# Usage:
#   bash infra/scripts/test/run-full.sh
#   bash infra/scripts/test/run-full.sh --baseline-duration 1m
#
# Exit codes:
#   0 — all PASS
#   1 — usage / pre-flight failure
#   2 — any FAIL or INCONCLUSIVE in suite

set -euo pipefail

set -uo pipefail

BASELINE_DURATION="${BASELINE_DURATION:-1m}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --baseline-duration) BASELINE_DURATION="$2"; shift 2 ;;
    -h|--help) sed -n '1,15p' "$0"; exit 0 ;;
    *) echo "usage: $0 [--baseline-duration 1m]" >&2; exit 1 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SCRIPTS_DIR="$REPO_ROOT/infra/scripts/test"
TS=$(date -u +%Y%m%dT%H%M%SZ)
SUITE_DIR="/tmp/chaos-full-${TS}"
mkdir -p "$SUITE_DIR"
SUMMARY="$SUITE_DIR/summary.md"

log() { printf '[chaos-full] %s\n' "$*" >&2; }

# ---------------------------------------------------------------------------
# Scenario → target cluster mapping (manifest selector.namespaces determines)
# ---------------------------------------------------------------------------
declare -A SCENARIO_CTX
SCENARIO_CTX["C01"]="k3d-loopruntime-core"        # pod-kill-api-leader → loopruntime-system
SCENARIO_CTX["C02"]="k3d-loopruntime-core"        # pod-kill-controller → loopruntime-system
SCENARIO_CTX["C03"]="k3d-loopruntime-cell-0"    # random-tenant-pod-kill → tenant ns
SCENARIO_CTX["C04"]="k3d-loopruntime-build"       # node-unschedulable → argo ns
SCENARIO_CTX["C05"]="k3d-loopruntime-core"        # argocd-crash → loopruntime-system
SCENARIO_CTX["C06"]="k3d-loopruntime-build"       # build-cluster-network-partition → argo
SCENARIO_CTX["C07"]="k3d-loopruntime-core"        # gitea-outage → loopruntime-system
SCENARIO_CTX["C08"]="k3d-loopruntime-core"        # pg-network-latency → loopruntime-system
SCENARIO_CTX["C09"]="k3d-loopruntime-core"        # pg-connection-storm → loopruntime-system
SCENARIO_CTX["C10"]="k3d-loopruntime-build"       # buildkit-io-pressure → argo

declare -a RESULTS
PASS_COUNT=0
FAIL_COUNT=0
INCONCLUSIVE_COUNT=0
TOTAL_DURATION=0

START_TS=$(date -u +%s)

cat > "$SUMMARY" <<EOF
# Chaos Test Suite — Full Run Report

**Generated:** $(date -u +%Y-%m-%dT%H:%M:%SZ)
**Mode:** --full (10 scenarios sequential)
**Baseline duration per scenario:** $BASELINE_DURATION

## Pre-flight

EOF

for ctx in k3d-loopruntime-core k3d-loopruntime-build k3d-loopruntime-cell-0 k3d-loopruntime-cell-1; do
  pods=$(kubectl --context "$ctx" get pods -n chaos-mesh 2>/dev/null | grep -c "Running" || echo 0)
  echo "- $ctx chaos-mesh: $pods pods Running" >> "$SUMMARY"
done

echo "" >> "$SUMMARY"
echo "## Scenarios" >> "$SUMMARY"
echo "" >> "$SUMMARY"

# ---------------------------------------------------------------------------
# Iterate scenarios C01..C10
# ---------------------------------------------------------------------------
for sid in C01 C02 C03 C04 C05 C06 C07 C08 C09 C10; do
  target="${SCENARIO_CTX[$sid]}"
  log "=========================================="
  log "Running $sid against $target"
  log "=========================================="

  scenario_start=$(date -u +%s)

  # Capture rc WITHOUT letting `set -e` abort the whole suite when a single
  # scenario's pre-flight die()s (e.g. chaos-mesh missing on a target cluster).
  # Without the `&& rc=0 || rc=$?` guard, set -e aborts at the invocation line
  # before the case below can record INCONCLUSIVE and continue to the next
  # scenario — turning one missing prereq into a total suite abort.
  rc=0
  TARGET_CONTEXT="$target" bash "$SCRIPTS_DIR/run-scenario.sh" \
    --scenario "$sid" \
    --baseline-duration "$BASELINE_DURATION" \
    --target-context "$target" \
    > "$SUITE_DIR/$sid.log" 2>&1 || rc=$?
  scenario_end=$(date -u +%s)
  scenario_dur=$((scenario_end - scenario_start))
  TOTAL_DURATION=$((TOTAL_DURATION + scenario_dur))

  case "$rc" in
    0) verdict="PASS"; PASS_COUNT=$((PASS_COUNT + 1)) ;;
    2) verdict="FAIL"; FAIL_COUNT=$((FAIL_COUNT + 1)) ;;
    *) verdict="INCONCLUSIVE"; INCONCLUSIVE_COUNT=$((INCONCLUSIVE_COUNT + 1)) ;;
  esac

  # Extract verdict reasons from scenario log
  reason=$(grep "during chaos:" "$SUITE_DIR/$sid.log" 2>/dev/null | head -1 | sed 's/.*\[chaos\]//' || echo "")
  # `|| echo ""` guard: when a scenario dies early (e.g. baseline aborts) its
  # log has no "baseline p95:" line, so grep returns 1 and — under `set -e` +
  # `pipefail` — would crash run-full.sh BEFORE recording the verdict, turning
  # one failed scenario into a total suite abort with no verdict logged.
  metrics=$(grep -E "baseline p95:|success rate:" "$SUITE_DIR/$sid.log" 2>/dev/null | tail -2 | head -2 | tr '\n' ' ' | sed 's/\[chaos\]//g' || echo "")

  RESULTS+=("| $sid | $target | $verdict | ${scenario_dur}s | $metrics |")

  echo "### $sid → $verdict (target=$target, duration=${scenario_dur}s)" >> "$SUMMARY"
  echo "" >> "$SUMMARY"
  echo "Metrics: $metrics" >> "$SUMMARY"
  echo "" >> "$SUMMARY"
  echo "Log: \`$SUITE_DIR/$sid.log\`" >> "$SUMMARY"
  echo "" >> "$SUMMARY"
  echo "---" >> "$SUMMARY"
  echo "" >> "$SUMMARY"

  log "  → $sid verdict: $verdict (${scenario_dur}s)"
done

END_TS=$(date -u +%s)
SUITE_DURATION=$((END_TS - START_TS))

# Final summary
cat >> "$SUMMARY" <<EOF

## Final results

**Suite duration:** $((SUITE_DURATION / 60))m $((SUITE_DURATION % 60))s

| Verdict | Count |
|---------|-------|
| PASS | $PASS_COUNT / 10 |
| FAIL | $FAIL_COUNT / 10 |
| INCONCLUSIVE | $INCONCLUSIVE_COUNT / 10 |

EOF

if [[ "$FAIL_COUNT" -eq 0 && "$INCONCLUSIVE_COUNT" -eq 0 ]]; then
  echo "**Resilience verdict: PASS (all 10 scenarios passed)**" >> "$SUMMARY"
elif [[ "$FAIL_COUNT" -gt 0 ]]; then
  echo "**Resilience verdict: DEGRADED ($FAIL_COUNT scenarios failed)**" >> "$SUMMARY"
else
  echo "**Resilience verdict: INCONCLUSIVE ($INCONCLUSIVE_COUNT scenarios couldn't run)**" >> "$SUMMARY"
fi

echo "" >> "$SUMMARY"
echo "Per-scenario logs in: \`$SUITE_DIR/\`" >> "$SUMMARY"

log "=========================================="
log "SUITE COMPLETE"
log "  Duration: $((SUITE_DURATION / 60))m $((SUITE_DURATION % 60))s"
log "  PASS: $PASS_COUNT / 10"
log "  FAIL: $FAIL_COUNT / 10"
log "  INCONCLUSIVE: $INCONCLUSIVE_COUNT / 10"
log "  Summary: $SUMMARY"
log "=========================================="

# Echo path for caller
echo "$SUMMARY"

if [[ "$FAIL_COUNT" -gt 0 || "$INCONCLUSIVE_COUNT" -gt 0 ]]; then
  exit 2
fi
exit 0
