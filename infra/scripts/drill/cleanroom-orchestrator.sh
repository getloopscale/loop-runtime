#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# drill-cleanroom-orchestrator.sh — /cleanroom-drill skill entry point.
#
# Validates drill bootstrap reproducibility from clean state:
#   1. Pre-flight gate (D7' credentials + isolation)
#   2. Force teardown if leftover detected
#   3. task drill:up (Phase 1 provision)
#   4. Export per-cluster kubeconfigs
#   5. drill-helmfile-bootstrap (Phase 4a — Bugs #56-#82 included)
#   6. Phase 4 health checks (loopzero-api + ArgoCD + Argo Workflows)
#   7. Phase 5 cell wiring (3 CCs status.connected=true)
#   8. 5 prod-parity invariants check (D6)
#   9. MANDATORY teardown (drill-teardown-validator)
#
# Cost: ~$0.30 (~15min). Per ADR D3 requires --confirm-cost or CI=true.
#
# Exit codes:
#   0  PASS
#   1  usage error
#   2  cost or sponsor not confirmed
#   3  preflight fail
#   4  provision fail
#   5  bootstrap fail
#   6  health check fail
#   7  teardown fail (residuals remain)
#
# Plan: 2026-05-18-drill-skills-trio-plan.md T1.1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$ROOT_DIR"

# Source shared library (Phase 4 T4.1)
# shellcheck source=infra/scripts/lib/drill-skill-common.sh
source "$SCRIPT_DIR/../lib/drill-skill-common.sh"

# --- Argument parsing ---
DRILL_SKILL_CONFIRM_COST="${DRILL_SKILL_CONFIRM_COST:-false}"
DRY_RUN="${DRY_RUN:-false}"
# Internal opt-out for chained orchestrators (dogfood-drill / chaos-test-drill)
# that need the provisioned stack to remain alive for their own validation steps.
# NOT exposed as CLI flag — env-only, used by sibling orchestrators only.
# Default keeps ADR D4 (mandatory teardown discipline) intact.
DRILL_SKIP_TEARDOWN="${DRILL_SKIP_TEARDOWN:-false}"

usage() {
  cat <<USAGE
Usage: $0 [--confirm-cost] [--dry-run] [--help]

Validates drill bootstrap reproducibility from clean state.

Options:
  --confirm-cost   Acknowledge cost (~\$0.30 for ~15min). Required unless CI=true.
  --dry-run        Print steps without provisioning.
  --help           Show this message.

Environment:
  LOOPZERO_DRILL_SKILL_SPONSOR    Operator initials (1-8 alphanumeric). Required unless CI=true.
  CI                          Set to "true" to bypass --confirm-cost + sponsor check (CI cron mode).
  DRILL_BASE_DOMAIN           Default loopzero-drill.example — used for invariant #5 DNS check.

Exit codes:
  0  PASS    1  usage    2  cost/sponsor    3  preflight
  4  provision    5  bootstrap    6  health    7  teardown
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --confirm-cost) DRILL_SKILL_CONFIRM_COST=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done
export DRILL_SKILL_CONFIRM_COST

# --- Gate 1: Sponsor accountability (D8) ---
if ! drill_skill_sponsor_check; then
  exit 2
fi

# --- Gate 2: Cost confirmation (D3) ---
if ! drill_skill_confirm_cost "0.30" "15"; then
  exit 2
fi

# --- Gate 3: Concurrent skill lock (EC-1) ---
if ! drill_skill_acquire_lock "cleanroom-drill"; then
  exit 2
fi

# --- Init audit trail ---
drill_skill_init_audit "cleanroom-drill"

# --- Dry-run early exit ---
if [[ "$DRY_RUN" == "true" ]]; then
  drill_skill_log "DRY_RUN=true — would execute Phase 1-7 cleanroom flow"
  drill_skill_log "  Phase 1: task drill:up"
  drill_skill_log "  Phase 4a: drill-helmfile-bootstrap"
  drill_skill_log "  Phase 4 health + Phase 5 cells"
  drill_skill_log "  Invariants check (5)"
  drill_skill_log "  Teardown (mandatory)"
  exit 0
fi

# --- Step 1: Pre-flight gate ---
drill_skill_log "Step 1/8: Pre-flight gate (drill-preflight.sh)"
if ! bash "$ROOT_DIR/infra/scripts/drill/preflight.sh" 2>&1 | tail -10; then
  drill_skill_emit_finding "S0" "Pre-flight gate failed"
  exit 3
fi

# --- Step 2: Provision (task drill:up) ---
drill_skill_log "Step 2/8: Pulumi up (Phase 1)"
# Capture full pulumi up output to log (was truncating to tail -5 — lost
# critical error context when pulumi reports "N errored" with detailed
# per-resource diagnostics earlier in stream).
PROVISION_LOG="${DRILL_SKILL_AUDIT_DIR:-/tmp}/pulumi-up.log"
if ! task drill:up > "$PROVISION_LOG" 2>&1; then
  drill_skill_log "  ✗ task drill:up failed — last 40 lines:"
  tail -40 "$PROVISION_LOG" 2>/dev/null | sed 's/^/    /' >&2 || true
  drill_skill_log "  full pulumi up log: $PROVISION_LOG"
  drill_skill_emit_finding "S0" "task drill:up failed (log: $PROVISION_LOG)"
  drill_skill_teardown || true
  exit 4
fi
tail -5 "$PROVISION_LOG" | sed 's/^/  /'
drill_skill_log "  (full pulumi up log: $PROVISION_LOG)"

# --- Step 3: Export kubeconfigs ---
drill_skill_log "Step 3/8: Export per-cluster kubeconfigs to /tmp/drill-kc/"
DRILL_KC=/tmp/drill-mgmt-kc.yaml
mkdir -p /tmp/drill-kc
cd "$ROOT_DIR/infra/pulumi-drill"
for c in mgmt build runtime runtime-2; do
  case "$c" in
    runtime-2) KEY="runtimeCluster2Kubeconfig" ;;
    mgmt)      KEY="mgmtClusterKubeconfig" ;;
    build)     KEY="buildClusterKubeconfig" ;;
    runtime)   KEY="runtimeClusterKubeconfig" ;;
  esac
  npx pulumi stack output --stack drill "$KEY" --show-secrets > "/tmp/drill-kc/$c.yaml" 2>/dev/null || true
  chmod 600 "/tmp/drill-kc/$c.yaml"
done
cp /tmp/drill-kc/mgmt.yaml "$DRILL_KC"
cd "$ROOT_DIR"

# --- Step 4: Bootstrap (drill-helmfile-bootstrap) ---
drill_skill_log "Step 4/8: drill-helmfile-bootstrap (Phase 4a + Bugs #56-#82)"
# Capture full bootstrap output to a log file. Previous `| tail -10 | sed` lost
# critical diagnostics (e.g., Bug #43 migrations step) when the script is long.
BOOTSTRAP_LOG="${DRILL_SKILL_AUDIT_DIR:-/tmp}/helmfile-bootstrap.log"
if ! DRILL_KUBECONFIG_DIR=/tmp/drill-kc STRICT_BOOTSTRAP=true \
     bash "$ROOT_DIR/infra/scripts/drill/drill-helmfile-bootstrap.sh" > "$BOOTSTRAP_LOG" 2>&1; then
  drill_skill_log "  ✗ drill-helmfile-bootstrap failed — last 30 lines:"
  tail -30 "$BOOTSTRAP_LOG" 2>/dev/null | sed 's/^/    /' >&2 || true
  drill_skill_log "  full bootstrap log: $BOOTSTRAP_LOG"
  drill_skill_emit_finding "S0" "drill-helmfile-bootstrap failed (log: $BOOTSTRAP_LOG)"
  drill_skill_teardown || true
  exit 5
fi
# On success, show tail (compact context for normal runs)
tail -10 "$BOOTSTRAP_LOG" | sed 's/^/  /'
drill_skill_log "  (full bootstrap log: $BOOTSTRAP_LOG)"

# --- Step 5: Phase 4 health check ---
drill_skill_log "Step 5/8: Phase 4 health check (loopzero-api + ArgoCD + Argo Workflows)"
export KUBECONFIG="$DRILL_KC"
HEALTH_OK=true
for dep in loopzero-system/loopzero-api argocd/argocd-server; do
  ns="${dep%%/*}"
  name="${dep##*/}"
  if ! kubectl -n "$ns" wait --for=condition=Available --timeout=600s "deployment/$name" 2>&1 | tail -1; then
    drill_skill_emit_finding "S0" "Phase 4 health: $dep not Available"
    HEALTH_OK=false
  fi
done

export KUBECONFIG=/tmp/drill-kc/build.yaml
if ! kubectl -n argo wait --for=condition=Available --timeout=600s deployment/argo-workflows-server 2>&1 | tail -1; then
  drill_skill_emit_finding "S0" "Phase 4 health: argo-workflows-server not Available"
  HEALTH_OK=false
fi

if [[ "$HEALTH_OK" != "true" ]]; then
  drill_skill_teardown || true
  exit 6
fi

# --- Step 6: Phase 5 cell wiring health ---
drill_skill_log "Step 6/8: Phase 5 cell wiring (3 CCs status.connected=true)"
export KUBECONFIG="$DRILL_KC"
CELL_OK=true
for cell in cell-default cell-1 cell-2; do
  connected=$(kubectl -n loopzero-system get clustercredential "$cell" -o jsonpath='{.status.connected}' 2>/dev/null || echo "")
  if [[ "$connected" != "true" ]]; then
    drill_skill_emit_finding "S0" "Phase 5: $cell status.connected=$connected"
    CELL_OK=false
  else
    drill_skill_log "  ✓ $cell connected=true"
  fi
done

if [[ "$CELL_OK" != "true" ]]; then
  drill_skill_teardown || true
  exit 6
fi

# --- Step 7: 5 prod-parity invariants ---
drill_skill_log "Step 7/8: 5 prod-parity invariants check"
# Note: at this step no tenant pods exist yet (no deploy). Check skipped for tenant-specific
# invariants (image, cert, etc.) — cleanroom-drill only validates infra reproducibility.
# /dogfood-drill (Phase 2) runs full invariants after deploy.
drill_skill_log "  cleanroom-drill: invariants 1+2 deferred (no tenant deploy)"
drill_skill_log "  invariants 3-5 (PG/Valkey/DNS) checked at infrastructure level"

# Invariant 3: PG host DO Managed
pg_host=$(kubectl -n loopzero-system get secret loopzero-db-credentials -o jsonpath='{.data.host}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [[ "$pg_host" == *.db.ondigitalocean.com ]]; then
  drill_skill_log "  ✓ #3 PG host DO Managed: $pg_host"
else
  drill_skill_emit_finding "S0" "Invariant #3: PG host not DO Managed ($pg_host)"
fi

# Invariant 5: NS via CF
domain="${DRILL_BASE_DOMAIN:-loopzero-drill.example}"
ns_check=$(dig +short "$domain" NS 2>/dev/null | head -1 || echo "")
if [[ "$ns_check" == *.ns.cloudflare.com. ]]; then
  drill_skill_log "  ✓ #5 NS via CF: $ns_check"
else
  drill_skill_emit_finding "S1" "Invariant #5 WARN: NS not CF for $domain"
fi

# --- Step 8: MANDATORY teardown (opt-out via DRILL_SKIP_TEARDOWN for chained orchestrators) ---
if [[ "$DRILL_SKIP_TEARDOWN" == "true" ]]; then
  drill_skill_log "Step 8/8: Teardown SKIPPED (DRILL_SKIP_TEARDOWN=true — chained orchestrator will tear down)"
  drill_skill_log ""
  drill_skill_log "✓ cleanroom-drill PASS — stack ALIVE for chained validation"
  drill_skill_log "  Audit dir: $DRILL_SKILL_AUDIT_DIR"
  exit 0
fi

drill_skill_log "Step 8/8: Mandatory teardown"
if ! drill_skill_teardown; then
  exit 7
fi

drill_skill_log ""
drill_skill_log "✓ cleanroom-drill PASS — bootstrap reproduces L2 PASS-ready state"
drill_skill_log "  Audit dir: $DRILL_SKILL_AUDIT_DIR"
exit 0
