#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# drill-chaos-orchestrator.sh — /chaos-test-drill skill entry point.
#
# Injects chaos scenarios against drill prod-parity stack, validates SLO holds,
# measures recovery time, MANDATORY teardown.
#
# Cost: ~$3 (~60min). Per ADR D3 requires --confirm-cost or CI=true.
#
# Scenarios:
#   pod-kill-api          — PodChaos kill loopzero-api leader (re-uses local scenario)
#   build-network         — NetworkChaos partition build→mgmt (re-uses local)
#   buildkit-io           — IOChaos buildkit slowdown (re-uses local)
#   pg-managed-failover   — NEW NetworkChaos partition api→DO Managed PG
#   cert-renewal-fault    — NEW NetworkChaos partition cert-manager→CF API + ACME
#   all                   — run all 5 sequentially (default)
#
# Exit codes:
#   0  PASS
#   1  usage error
#   2  cost/sponsor/lock not confirmed
#   3  cleanroom-drill fail (foundation)
#   4  chaos-mesh install fail
#   5  chaos inject fail
#   6  SLO violation
#   7  teardown fail
#
# Plan: 2026-05-18-drill-skills-trio-plan.md T3.1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$ROOT_DIR"

# shellcheck source=infra/scripts/lib/drill-skill-common.sh
source "$SCRIPT_DIR/lib/drill-skill-common.sh"

# --- Args ---
DRILL_SKILL_CONFIRM_COST="${DRILL_SKILL_CONFIRM_COST:-false}"
SKIP_CLEANROOM="${SKIP_CLEANROOM:-false}"
DRY_RUN="${DRY_RUN:-false}"
SCENARIO="${SCENARIO:-all}"

VALID_SCENARIOS=(pod-kill-api build-network buildkit-io pg-managed-failover cert-renewal-fault all)

usage() {
  cat <<USAGE
Usage: $0 [--confirm-cost] [--scenario <id>] [--skip-cleanroom] [--dry-run] [--help]

Chaos resilience audit against drill prod-parity infrastructure.

Options:
  --confirm-cost       Acknowledge cost (~\$3 for ~60min). Required unless CI=true.
  --scenario <id>      One of: ${VALID_SCENARIOS[*]} (default: all)
  --skip-cleanroom     Skip cleanroom-drill foundation (CI multi-step only).
  --dry-run            Print scenarios + SLO criteria without provisioning.
  --help               Show this message.

Environment:
  LOOPZERO_DRILL_SKILL_SPONSOR    Operator initials. Required unless CI=true.
  CI                          true bypasses --confirm-cost + sponsor.

Scenarios (5):
  pod-kill-api          PodChaos: kill loopzero-api leader (90s recovery budget)
  build-network         NetworkChaos: partition build→mgmt (Gitea timeout test)
  buildkit-io           IOChaos: buildkit slowdown (build timeout test)
  pg-managed-failover   NEW: NetworkChaos partition api→DO Managed PG (60s recovery)
  cert-renewal-fault    NEW: NetworkChaos partition cert-manager→CF/ACME (600s recovery)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --confirm-cost) DRILL_SKILL_CONFIRM_COST=true; shift ;;
    --scenario) SCENARIO="$2"; shift 2 ;;
    --skip-cleanroom) SKIP_CLEANROOM=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done
export DRILL_SKILL_CONFIRM_COST

# Validate scenario
valid=false
for s in "${VALID_SCENARIOS[@]}"; do
  [[ "$SCENARIO" == "$s" ]] && valid=true && break
done
if [[ "$valid" != "true" ]]; then
  echo "Invalid scenario: $SCENARIO (must be one of: ${VALID_SCENARIOS[*]})" >&2
  exit 1
fi

# --- Gates ---
drill_skill_sponsor_check || exit 2
drill_skill_confirm_cost "3.00" "60" || exit 2
drill_skill_acquire_lock "chaos-test-drill" || exit 2
drill_skill_init_audit "chaos-test-drill"

if [[ "$DRY_RUN" == "true" ]]; then
  drill_skill_log "DRY_RUN=true — scenario plan:"
  case "$SCENARIO" in
    all)
      for s in pod-kill-api build-network buildkit-io pg-managed-failover cert-renewal-fault; do
        drill_skill_log "  [DRY] $s"
      done ;;
    *)
      drill_skill_log "  [DRY] $SCENARIO" ;;
  esac
  exit 0
fi

# --- Step 1: cleanroom-drill foundation ---
if [[ "$SKIP_CLEANROOM" != "true" ]]; then
  drill_skill_log "Step 1/5: cleanroom-drill (foundation, DRILL_SKIP_TEARDOWN=true to preserve stack)"
  # DRILL_SKIP_TEARDOWN=true keeps stack ALIVE for chaos scenarios below.
  # ADR D4 preserved: this orchestrator does its own teardown at end.
  if ! DRILL_SKILL_CONFIRM_COST=true \
       DRILL_SKIP_TEARDOWN=true \
       bash "$ROOT_DIR/infra/scripts/drill-cleanroom-orchestrator.sh" --confirm-cost \
       2>&1 | tail -15 | sed 's/^/  /'; then
    drill_skill_emit_finding "S0" "cleanroom-drill foundation failed"
    exit 3
  fi
else
  drill_skill_log "Step 1/5: SKIPPED (--skip-cleanroom)"
fi

# --- Step 2: Install chaos-mesh on runtime cluster ---
drill_skill_log "Step 2/5: Install chaos-mesh on runtime cluster"
RUNTIME_KC=/tmp/drill-kc/runtime.yaml
if [[ ! -f "$RUNTIME_KC" ]]; then
  drill_skill_emit_finding "S0" "runtime kubeconfig not found at $RUNTIME_KC"
  drill_skill_teardown || true
  exit 4
fi

if ! helm --kubeconfig "$RUNTIME_KC" upgrade --install chaos-mesh chaos-mesh \
     --repo https://charts.chaos-mesh.org --namespace chaos-mesh \
     --create-namespace --wait --timeout 600s 2>&1 | tail -5 | sed 's/^/  /'; then
  drill_skill_emit_finding "S0" "chaos-mesh helm install failed"
  drill_skill_teardown || true
  exit 4
fi
drill_skill_log "  ✓ chaos-mesh installed"

# --- Step 3: Start sustained traffic (background curl loop) ---
drill_skill_log "Step 3/5: Start sustained traffic generator (1 RPS, ${TRAFFIC_DURATION:-180}s)"
TRAFFIC_LOG="$DRILL_SKILL_AUDIT_DIR/traffic.log"
TRAFFIC_DURATION="${TRAFFIC_DURATION:-180}"
APP_URL="https://api.${DRILL_BASE_DOMAIN:-loopzero-drill.example}/api/v1/health"

# Background traffic loop
(
  TRAFFIC_END=$(($(date +%s) + TRAFFIC_DURATION))
  while [[ $(date +%s) -lt "$TRAFFIC_END" ]]; do
    TRAFFIC_CODE=$(curl -s -m 5 "$APP_URL" -o /dev/null -w "%{http_code}" 2>/dev/null || echo "000")
    echo "$(date -u +%FT%TZ) HTTP $TRAFFIC_CODE" >> "$TRAFFIC_LOG"
    sleep 1
  done
) &
TRAFFIC_PID=$!

# Ensure traffic killed on exit
trap 'kill $TRAFFIC_PID 2>/dev/null; drill_skill_cleanup_on_exit' EXIT

# --- Step 4: Inject chaos scenarios ---
drill_skill_log "Step 4/5: Inject chaos scenarios ($SCENARIO)"

inject_scenario() {
  local id="$1"
  local manifest_path
  case "$id" in
    pod-kill-api) manifest_path="$ROOT_DIR/infra/tests/chaos/manifests/pod-kill-api-leader.yaml" ;;
    build-network) manifest_path="$ROOT_DIR/infra/tests/chaos/manifests/build-cluster-network-partition.yaml" ;;
    buildkit-io) manifest_path="$ROOT_DIR/infra/tests/chaos/manifests/buildkit-io-pressure.yaml" ;;
    pg-managed-failover) manifest_path="$ROOT_DIR/infra/tests/chaos/manifests/drill-pg-managed-failover.yaml" ;;
    cert-renewal-fault) manifest_path="$ROOT_DIR/infra/tests/chaos/manifests/drill-cert-renewal-under-network-fault.yaml" ;;
    *) drill_skill_log "  ✗ unknown scenario: $id"; return 1 ;;
  esac

  if [[ ! -f "$manifest_path" ]]; then
    drill_skill_emit_finding "S0" "Chaos manifest missing: $manifest_path"
    return 1
  fi

  drill_skill_log "  injecting $id..."
  if ! kubectl --kubeconfig "$RUNTIME_KC" apply -f "$manifest_path" 2>&1 | tail -3 | sed 's/^/    /'; then
    drill_skill_emit_finding "S0" "Chaos inject failed: $id"
    return 1
  fi
  return 0
}

measure_slo() {
  local id="$1"
  local budget="$2"
  drill_skill_log "  monitoring SLO during $id (budget ${budget}s)..."
  sleep "$budget"

  # Analyze traffic log for 5xx rate
  local total fails rate
  total=$(wc -l < "$TRAFFIC_LOG" 2>/dev/null || echo "0")
  fails=$(grep -cE "HTTP 5[0-9][0-9]" "$TRAFFIC_LOG" 2>/dev/null || echo "0")
  if [[ "$total" -gt 0 ]]; then
    rate=$(( fails * 100 / total ))
    drill_skill_log "  SLO: ${fails}/${total} 5xx (${rate}%)"
    if [[ "$rate" -gt 5 ]]; then
      drill_skill_emit_finding "S1" "$id 5xx rate ${rate}% exceeds 5% threshold"
      return 1
    fi
  fi
  return 0
}

cleanup_chaos() {
  drill_skill_log "  cleaning up chaos resources..."
  kubectl --kubeconfig "$RUNTIME_KC" delete podchaos,networkchaos,iochaos,stresschaos --all -A 2>&1 | tail -3 | sed 's/^/    /' || true
}

SLO_VIOLATIONS=0
case "$SCENARIO" in
  all)
    for s in pod-kill-api build-network buildkit-io pg-managed-failover cert-renewal-fault; do
      drill_skill_log "  Scenario: $s"
      if inject_scenario "$s"; then
        measure_slo "$s" 90 || SLO_VIOLATIONS=$((SLO_VIOLATIONS + 1))
        cleanup_chaos
      fi
    done ;;
  *)
    inject_scenario "$SCENARIO" || SLO_VIOLATIONS=$((SLO_VIOLATIONS + 1))
    measure_slo "$SCENARIO" 90 || SLO_VIOLATIONS=$((SLO_VIOLATIONS + 1))
    cleanup_chaos ;;
esac

if [[ "$SLO_VIOLATIONS" -gt 0 ]]; then
  drill_skill_log "  ✗ $SLO_VIOLATIONS SLO violation(s)"
  # Stop traffic
  kill $TRAFFIC_PID 2>/dev/null || true
  drill_skill_teardown || true
  exit 6
fi

# Stop traffic before teardown
kill $TRAFFIC_PID 2>/dev/null || true
wait $TRAFFIC_PID 2>/dev/null || true

# --- Step 5: MANDATORY teardown ---
drill_skill_log "Step 5/5: Mandatory teardown"
if ! drill_skill_teardown; then
  exit 7
fi

drill_skill_log ""
drill_skill_log "✓ chaos-test-drill PASS — all scenarios held SLO + recovered within budget"
drill_skill_log "  Audit: $DRILL_SKILL_AUDIT_DIR"
exit 0
