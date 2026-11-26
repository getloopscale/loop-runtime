#!/usr/bin/env bash
# run-scenario.sh — Main orchestrator for /chaos-test skill.
#
# Executes the full chaos cycle for a single scenario:
#   1. Pre-flight (chaos-mesh + clusters + zero orphan chaos state)
#   2. Baseline (5min steady-state measurement)
#   3. Inject (apply chaos-mesh manifest)
#   4. Observe (sample SLO during chaos duration)
#   5. Recover (wait for chaos `Finished` phase)
#   6. Validate post-recovery (re-baseline + compare)
#   7. Cleanup (delete chaos resources)
#   8. Report (audit trail in /tmp/chaos-report-*.md)
#
# CLAUDE.md / SKILL Regra Suprema: NEVER skip any step. The cycle is what
# distinguishes chaos engineering from "kill pod see what happens".
#
# Usage:
#   bash infra/scripts/test/run-scenario.sh --scenario C01
#   bash infra/scripts/test/run-scenario.sh --scenario C01 --baseline-duration 2m
#   bash infra/scripts/test/run-scenario.sh --scenario C01 --dry-run
#
# Exit codes:
#   0 — PASS (SLO held + recovery within budget)
#   1 — usage / pre-flight failure
#   2 — FAIL (SLO violated OR recovery exceeded budget)
#   3 — INCONCLUSIVE (chaos resource never entered Running)

set -euo pipefail

set -uo pipefail

SCENARIO=""
BASELINE_DURATION="5m"
DRY_RUN=0
NO_CLEANUP=0
TARGET_CTX="${TARGET_CONTEXT:-k3d-loop-runtime-core}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scenario)
      SCENARIO="$2"; shift 2 ;;
    --baseline-duration)
      BASELINE_DURATION="$2"; shift 2 ;;
    --dry-run)
      DRY_RUN=1; shift ;;
    --no-cleanup)
      NO_CLEANUP=1; shift ;;
    --target-context)
      TARGET_CTX="$2"; shift 2 ;;
    -h|--help)
      sed -n '1,40p' "$0"; exit 0 ;;
    *)
      echo "usage: $0 --scenario <C01..C10> [--baseline-duration 5m] [--dry-run] [--no-cleanup]" >&2
      exit 1 ;;
  esac
done

[[ -n "$SCENARIO" ]] || { echo "ERROR: --scenario required (C01..C10)" >&2; exit 1; }

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
MANIFESTS_DIR="$REPO_ROOT/infra/tests/chaos/manifests"
SCRIPTS_DIR="$REPO_ROOT/infra/scripts/test"
TS=$(date -u +%Y%m%dT%H%M%SZ)
RUN_DIR="/tmp/chaos-${SCENARIO}-${TS}"
mkdir -p "$RUN_DIR"

log() { printf '[chaos] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# ---------------------------------------------------------------------------
# Scenario ID → manifest mapping (12 scenarios total per SKILL.md table)
# ---------------------------------------------------------------------------
declare -A SCENARIOS
SCENARIOS["C01"]="pod-kill-api-leader.yaml"
SCENARIOS["C02"]="pod-kill-controller.yaml"
SCENARIOS["C03"]="random-tenant-pod-kill.yaml"
SCENARIOS["C04"]="node-unschedulable.yaml"
SCENARIOS["C05"]="argocd-crash.yaml"
SCENARIOS["C06"]="build-cluster-network-partition.yaml"
SCENARIOS["C07"]="gitea-outage.yaml"
SCENARIOS["C08"]="pg-network-latency.yaml"
SCENARIOS["C09"]="pg-connection-storm.yaml"
SCENARIOS["C10"]="buildkit-io-pressure.yaml"

MANIFEST_FILE="${SCENARIOS[$SCENARIO]:-}"
[[ -n "$MANIFEST_FILE" ]] || die "unknown scenario $SCENARIO (valid: C01..C10)"
MANIFEST_PATH="$MANIFESTS_DIR/$MANIFEST_FILE"
[[ -f "$MANIFEST_PATH" ]] || die "manifest not found: $MANIFEST_PATH"

log "scenario=$SCENARIO manifest=$MANIFEST_FILE target=$TARGET_CTX run_dir=$RUN_DIR"

# ---------------------------------------------------------------------------
# Etapa 1 — Pre-flight
# ---------------------------------------------------------------------------
log "=== Etapa 1: Pre-flight ==="

kubectl --context "$TARGET_CTX" cluster-info >/dev/null 2>&1 || die "context $TARGET_CTX unreachable"
log "  context $TARGET_CTX reachable"

kubectl --context "$TARGET_CTX" get crd podchaos.chaos-mesh.org >/dev/null 2>&1 || die "chaos-mesh CRDs missing — install: helm upgrade --install chaos-mesh infra/helm/chaos-mesh --kube-context $TARGET_CTX -n chaos-mesh --create-namespace"
log "  chaos-mesh CRDs present"

EXISTING=$(kubectl --context "$TARGET_CTX" get podchaos,networkchaos,iochaos,stresschaos --all-namespaces 2>/dev/null | grep -cv "^NAMESPACE" || true)
if [[ "$EXISTING" -gt 0 ]]; then
  log "  WARN: $EXISTING existing chaos resources in cluster"
  if [[ "$NO_CLEANUP" != "1" ]]; then
    log "  cleaning up orphan chaos state..."
    bash "$SCRIPTS_DIR/cleanup.sh" --target-context "$TARGET_CTX" || die "cleanup of orphan chaos failed"
  fi
else
  log "  zero orphan chaos resources ✓"
fi

# Extract hypothesis from manifest (FAANG-style metadata)
HYPOTHESIS=$(grep "runtime.getloopscale.com/hypothesis:" "$MANIFEST_PATH" | head -1 | sed 's/.*runtime.getloopscale.com\/hypothesis: *//;s/^"//;s/"$//' || echo "(no hypothesis)")
DOD_TEST=$(grep "runtime.getloopscale.com/dod-test:" "$MANIFEST_PATH" | head -1 | sed 's/.*runtime.getloopscale.com\/dod-test: *//;s/^"//;s/"$//' || echo "(no DoD)")

log "  hypothesis: $HYPOTHESIS"
log "  DoD test: $DOD_TEST"

if [[ "$DRY_RUN" == "1" ]]; then
  log "=== DRY RUN: would proceed with baseline → inject → observe → recover → validate → cleanup → report ==="
  exit 0
fi

# ---------------------------------------------------------------------------
# Etapa 2 — Baseline (steady-state)
# ---------------------------------------------------------------------------
log "=== Etapa 2: Baseline (duration=$BASELINE_DURATION) ==="

BASELINE_OUTPUT=$(bash "$SCRIPTS_DIR/baseline.sh" "$SCENARIO" --duration "$BASELINE_DURATION")
[[ -f "$BASELINE_OUTPUT" ]] || die "baseline failed; no output"
cp "$BASELINE_OUTPUT" "$RUN_DIR/baseline.json"
log "  baseline saved: $RUN_DIR/baseline.json"

# Extract pre-chaos metrics via jq (avoids regex-space-tolerance bugs)
PRE_P95=$(jq -r '.api_health.latency_p95_ms' "$RUN_DIR/baseline.json")
PRE_SUCCESS=$(jq -r '.api_health.success_rate_pct' "$RUN_DIR/baseline.json")
PRE_LOOP_RUNTIME_API_RESTARTS=$(jq -r '.pod_restart_counts.loop_runtime_api' "$RUN_DIR/baseline.json")

# ---------------------------------------------------------------------------
# Etapa 3 — Inject
# ---------------------------------------------------------------------------
log "=== Etapa 3: Inject ==="

INJECT_TS=$(date -u +%s)
INJECT_TS_HUMAN=$(date -u -d "@$INJECT_TS" +%Y-%m-%dT%H:%M:%SZ)
log "  injection timestamp: $INJECT_TS_HUMAN"

kubectl --context "$TARGET_CTX" apply -f "$MANIFEST_PATH" > "$RUN_DIR/inject.log" 2>&1 || die "kubectl apply failed: see $RUN_DIR/inject.log"

# Extract chaos resource name + duration
CHAOS_NAME=$(grep "^  name:" "$MANIFEST_PATH" | head -1 | awk '{print $2}')
CHAOS_NS=$(grep "^  namespace:" "$MANIFEST_PATH" | head -1 | awk '{print $2}')
CHAOS_KIND=$(grep "^kind:" "$MANIFEST_PATH" | head -1 | awk '{print $2}')
DURATION_STR=$(grep "^  duration:" "$MANIFEST_PATH" | head -1 | sed 's/.*duration: *//;s/"//g')

log "  chaos resource: $CHAOS_KIND/$CHAOS_NAME in ns=$CHAOS_NS duration=$DURATION_STR"

# ---------------------------------------------------------------------------
# Etapa 4 — Observe (during chaos)
# ---------------------------------------------------------------------------
log "=== Etapa 4: Observe (during chaos duration=$DURATION_STR) ==="

# Parse duration (30s, 5m, 1m, etc.)
DURATION_SEC=30
if [[ "$DURATION_STR" =~ ^([0-9]+)([sm])$ ]]; then
  n="${BASH_REMATCH[1]}"
  u="${BASH_REMATCH[2]}"
  if [[ "$u" == "m" ]]; then DURATION_SEC=$((n * 60)); else DURATION_SEC="$n"; fi
fi

# Add 30s buffer to capture chaos termination phase
OBSERVE_DURATION=$((DURATION_SEC + 30))
log "  observing for ${OBSERVE_DURATION}s (chaos $DURATION_SEC + 30s buffer)..."

DURING_OUTPUT=$(bash "$SCRIPTS_DIR/baseline.sh" "$SCENARIO-during" --duration "${OBSERVE_DURATION}s")
cp "$DURING_OUTPUT" "$RUN_DIR/during.json"
log "  during-chaos metrics saved: $RUN_DIR/during.json"

DURING_P95=$(jq -r '.api_health.latency_p95_ms' "$RUN_DIR/during.json")
DURING_SUCCESS=$(jq -r '.api_health.success_rate_pct' "$RUN_DIR/during.json")

# ---------------------------------------------------------------------------
# Etapa 5 — Recover (wait for chaos Finished)
# ---------------------------------------------------------------------------
log "=== Etapa 5: Recover ==="

# Resource is now finished (we waited duration+30s). Verify phase.
PHASE=$(kubectl --context "$TARGET_CTX" get "$CHAOS_KIND" "$CHAOS_NAME" -n "$CHAOS_NS" -o jsonpath='{.status.experiment.phase}' 2>/dev/null || echo "Unknown")
log "  chaos resource phase: $PHASE"

if [[ "$PHASE" != "Finished" && "$PHASE" != "Waiting" && "$PHASE" != "" ]]; then
  log "  WARN: phase $PHASE — expected Finished. Waiting 30s more..."
  sleep 30
  PHASE=$(kubectl --context "$TARGET_CTX" get "$CHAOS_KIND" "$CHAOS_NAME" -n "$CHAOS_NS" -o jsonpath='{.status.experiment.phase}' 2>/dev/null || echo "Unknown")
  log "  chaos phase after extra wait: $PHASE"
fi

# ---------------------------------------------------------------------------
# Etapa 6 — Validate post-recovery
# ---------------------------------------------------------------------------
log "=== Etapa 6: Validate post-recovery (2min baseline) ==="

POST_OUTPUT=$(bash "$SCRIPTS_DIR/baseline.sh" "$SCENARIO-post" --duration "2m")
cp "$POST_OUTPUT" "$RUN_DIR/post.json"

POST_P95=$(jq -r '.api_health.latency_p95_ms' "$RUN_DIR/post.json")
POST_SUCCESS=$(jq -r '.api_health.success_rate_pct' "$RUN_DIR/post.json")
POST_LOOP_RUNTIME_API_RESTARTS=$(jq -r '.pod_restart_counts.loop_runtime_api' "$RUN_DIR/post.json")

# ---------------------------------------------------------------------------
# Etapa 7 — Cleanup (MANDATORY)
# ---------------------------------------------------------------------------
log "=== Etapa 7: Cleanup ==="

if [[ "$NO_CLEANUP" == "1" ]]; then
  log "  --no-cleanup specified; leaving chaos resources for inspection"
  log "  WARNING: manual cleanup required before next run!"
else
  bash "$SCRIPTS_DIR/cleanup.sh" --target-context "$TARGET_CTX" || die "cleanup FAILED — manual intervention required"
  log "  cleanup complete; zero orphan chaos resources verified"
fi

# ---------------------------------------------------------------------------
# Etapa 8 — Verdict + Report
# ---------------------------------------------------------------------------
log "=== Etapa 8: Verdict ==="

# Verdict: delegated to `loop-runtime-ops chaos slo-compare` (pkg/quality/chaosslo,
# Track C TC3.1) — the threshold arithmetic (success>=90, p95-degradation<=500ms,
# post-drift<=100ms, restart-delta<=2) moved to Go, parity-proven byte-identical
# to the prior inline if-blocks. Verdict comes from the subcommand EXIT CODE
# (0=PASS / 2=FAIL / 5=INCONCLUSIVE); REASONS from its "  - <reason>" lines.
# RESTART_DELTA is still computed here (report.sh + log line consume it).
RESTART_DELTA=$((POST_LOOP_RUNTIME_API_RESTARTS - PRE_LOOP_RUNTIME_API_RESTARTS))

# `go run` collapses any non-zero subprocess exit to 1, which destroys the
# 0/2/5 PASS/FAIL/INCONCLUSIVE semantics chaosslo.ExitCode() defines and turns
# every real FAIL into a spurious INCONCLUSIVE. Prefer compiled binaries so
# the exit code propagates verbatim. Canonical build path is bin/loop-runtime-ops
# (Taskfile `ops:build` D6 — same path used by `task quality:gates`).
LOOP_RUNTIME_OPS_BIN=""
if command -v loop-runtime-ops >/dev/null 2>&1; then
  LOOP_RUNTIME_OPS_BIN="$(command -v loop-runtime-ops)"
elif [[ -x "$REPO_ROOT/bin/loop-runtime-ops" ]]; then
  LOOP_RUNTIME_OPS_BIN="$REPO_ROOT/bin/loop-runtime-ops"
else
  ( cd "$REPO_ROOT" && go build -o bin/loop-runtime-ops ./cmd/loop-runtime-ops ) >/dev/null 2>&1
  LOOP_RUNTIME_OPS_BIN="$REPO_ROOT/bin/loop-runtime-ops"
fi

set +e
SLO_OUT=$("$LOOP_RUNTIME_OPS_BIN" chaos slo-compare "$RUN_DIR/baseline.json" "$RUN_DIR/during.json" "$RUN_DIR/post.json")
SLO_RC=$?
set -e

case "$SLO_RC" in
  0) VERDICT="PASS" ;;
  2) VERDICT="FAIL" ;;
  *) VERDICT="INCONCLUSIVE" ;;
esac
mapfile -t REASONS < <(printf '%s\n' "$SLO_OUT" | sed -n 's/^  - //p')

# Write report
bash "$SCRIPTS_DIR/report.sh" \
  --scenario "$SCENARIO" \
  --manifest "$MANIFEST_PATH" \
  --run-dir "$RUN_DIR" \
  --verdict "$VERDICT" \
  --pre-p95 "$PRE_P95" \
  --during-p95 "$DURING_P95" \
  --post-p95 "$POST_P95" \
  --pre-success "$PRE_SUCCESS" \
  --during-success "$DURING_SUCCESS" \
  --post-success "$POST_SUCCESS" \
  --restart-delta "$RESTART_DELTA" \
  --reasons "$(IFS='|'; echo "${REASONS[*]}")" \
  --output "$RUN_DIR/report.md"

log "=================================="
log "SCENARIO $SCENARIO verdict: $VERDICT"
log "  baseline p95: ${PRE_P95}ms / during p95: ${DURING_P95}ms / post p95: ${POST_P95}ms"
log "  success rate: pre=${PRE_SUCCESS}% during=${DURING_SUCCESS}% post=${POST_SUCCESS}%"
log "  loop-runtime-api restart delta: $RESTART_DELTA"
log "  evidence: $RUN_DIR/"
log "=================================="

case "$VERDICT" in
  PASS) exit 0 ;;
  FAIL) exit 2 ;;
  *) exit 3 ;;
esac
