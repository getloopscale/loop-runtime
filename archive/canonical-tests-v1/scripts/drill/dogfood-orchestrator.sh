#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# drill-dogfood-orchestrator.sh — /dogfood-drill skill entry point.
#
# Black-box user journey audit against drill prod-parity infrastructure.
# Runs 7 user journeys via $LOOPZERO CLI (REAL deploy E2E) + validates 5 prod-parity invariants
# (D6) post-deploy. MANDATORY teardown.
#
# Cost: ~$2 (~45min). Per ADR D3 requires --confirm-cost or CI=true.
#
# Exit codes:
#   0  PASS
#   1  usage error
#   2  cost or sponsor not confirmed
#   3  cleanroom-drill fail (foundation gate)
#   4  JWT mint fail
#   5  journey fail (S0 finding)
#   6  prod-parity invariant violation (S0 finding)
#   7  teardown fail
#
# Plan: 2026-05-18-drill-skills-trio-plan.md T2.1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$ROOT_DIR"

# shellcheck source=infra/scripts/lib/drill-skill-common.sh
source "$SCRIPT_DIR/../lib/drill-skill-common.sh"

# --- Args ---
DRILL_SKILL_CONFIRM_COST="${DRILL_SKILL_CONFIRM_COST:-false}"
SKIP_CLEANROOM="${SKIP_CLEANROOM:-false}"
DRY_RUN="${DRY_RUN:-false}"

usage() {
  cat <<USAGE
Usage: $0 [--confirm-cost] [--skip-cleanroom] [--dry-run] [--help]

User journey audit against drill prod-parity infrastructure.

Options:
  --confirm-cost     Acknowledge cost (~\$2 for ~45min). Required unless CI=true.
  --skip-cleanroom   Skip cleanroom-drill preceding step (use only when stack already provisioned via CI multi-step).
  --dry-run          Print 7 CLI journeys + 5 invariants without provisioning.
  --help             Show this message.

Environment:
  LOOPZERO_DRILL_SKILL_SPONSOR    Operator initials. Required unless CI=true.
  CI                          true bypasses --confirm-cost + sponsor.

Journeys (7):
  J01 API health (curl)
  J02 loopzero login
  J03 loopzero project create
  J04 loopzero deploy + Prova de Vida
  J05 loopzero scale
  J06 loopzero rollback
  J07 loopzero project delete + Prova de Morte

Invariants (5):
  #1 image registry = DOCR
  #2 TLS cert = Let's Encrypt prod
  #3 PG host = DO Managed + firewall
  #4 Valkey TLS port 25061
  #5 DNS via CF nameservers
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --confirm-cost) DRILL_SKILL_CONFIRM_COST=true; shift ;;
    --skip-cleanroom) SKIP_CLEANROOM=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done
export DRILL_SKILL_CONFIRM_COST

# --- Gates ---
drill_skill_sponsor_check || exit 2
drill_skill_confirm_cost "2.00" "45" || exit 2
drill_skill_acquire_lock "dogfood-drill" || exit 2
drill_skill_init_audit "dogfood-drill"

if [[ "$DRY_RUN" == "true" ]]; then
  drill_skill_log "DRY_RUN=true — would execute 7 CLI journeys (login/create/init/deploy/scale/rollback/delete) + 5 invariants"
  for j in "J01 API health (curl)" "J02 loopzero login" "J03 loopzero project create" \
           "J04 loopzero deploy + Prova de Vida" "J05 loopzero scale" "J06 loopzero rollback" \
           "J07 loopzero project delete + Prova de Morte"; do
    drill_skill_log "  [DRY] $j"
  done
  exit 0
fi

# --- Step 1: Foundation — cleanroom-drill ---
if [[ "$SKIP_CLEANROOM" != "true" ]]; then
  drill_skill_log "Step 1/4: cleanroom-drill (foundation, DRILL_SKIP_TEARDOWN=true to preserve stack)"
  # DRILL_SKIP_TEARDOWN=true keeps the provisioned stack ALIVE so journeys in
  # step 3 have something to invoke against. Dogfood-drill does its OWN
  # teardown after journeys + invariants. ADR D4 (mandatory teardown) is
  # preserved at the dogfood-drill level (always calls drill_skill_teardown).
  # Cleanroom log redirected to own file so failure causes are visible (was
  # losing helmfile bootstrap errors to `| tail -15 | sed` truncation).
  CLEANROOM_LOG="${DRILL_SKILL_AUDIT_DIR:-/tmp}/cleanroom-output.log"
  if ! DRILL_SKILL_CONFIRM_COST=true \
       DRILL_SKIP_TEARDOWN=true \
       bash "$ROOT_DIR/infra/scripts/drill/cleanroom-orchestrator.sh" --confirm-cost \
       > "$CLEANROOM_LOG" 2>&1; then
    drill_skill_log "  ✗ cleanroom-drill foundation step failed — last 40 lines:"
    tail -40 "$CLEANROOM_LOG" 2>/dev/null | sed 's/^/    /' >&2 || true
    drill_skill_log "  full cleanroom log: $CLEANROOM_LOG"
    drill_skill_emit_finding "S0" "cleanroom-drill foundation step failed (log: $CLEANROOM_LOG)"
    exit 3
  fi
  drill_skill_log "  ✓ cleanroom-drill PASS (full log: $CLEANROOM_LOG)"
else
  drill_skill_log "Step 1/4: SKIPPED (--skip-cleanroom)"
fi

# --- Step 2: Mint JWT ---
drill_skill_log "Step 2/4: Mint JWT for journey auth"
DRILL_KC=/tmp/drill-mgmt-kc.yaml
export DRILL_KUBECONFIG="$DRILL_KC"
export KUBECONFIG="$DRILL_KC"
# Export DRILL_PG_HOST + DRILL_PG_PASS + DRILL_PG_PORT from pulumi outputs.
# drill-mint-jwt.sh has its own auto-discovery (Bug #37) but short-circuits
# if DRILL_PG_HOST is set without DRILL_PG_PASS, causing psql to fall back to
# placeholder "loopzero-drill-password" → auth fail. We set ALL THREE explicitly.
DRILL_PG_HOST=$(cd "$ROOT_DIR/infra/pulumi-drill" && npx pulumi stack output --stack drill pgHost 2>/dev/null || echo "")
DRILL_PG_PORT=$(cd "$ROOT_DIR/infra/pulumi-drill" && npx pulumi stack output --stack drill pgPort 2>/dev/null || echo "25060")
DRILL_PG_URI=$(cd "$ROOT_DIR/infra/pulumi-drill" && npx pulumi stack output --stack drill pgUri --show-secrets 2>/dev/null || echo "")
if [[ -n "$DRILL_PG_URI" ]]; then
  # Parse password from postgres://doadmin:PASS@host:port/db
  DRILL_PG_PASS=$(echo "$DRILL_PG_URI" | sed -E 's|^postgres(ql)?://[^:]+:([^@]+)@.*$|\2|')
fi
export DRILL_PG_HOST DRILL_PG_PORT DRILL_PG_PASS
JWT_LOG=/tmp/drill-mint-jwt.log
if ! JWT=$(bash "$ROOT_DIR/infra/scripts/drill/drill-mint-jwt.sh" 2>"$JWT_LOG"); then
  drill_skill_log "  ✗ drill-mint-jwt failed — last 10 lines of stderr:"
  tail -10 "$JWT_LOG" 2>/dev/null | sed 's/^/    /' >&2 || true
  drill_skill_emit_finding "S0" "drill-mint-jwt failed (log: $JWT_LOG)"
  drill_skill_teardown || true
  exit 4
fi
export LOOPZERO_TOKEN="${JWT##*$'\n'}"  # last line
drill_skill_log "  ✓ JWT minted (len=${#LOOPZERO_TOKEN})"

# --- Step 3: 7 CLI journeys (REAL deploy E2E) ---
# Plan: 2026-05-19-drill-skills-rigor-restoration T3.2 + ADR D3 (CLI-First)
drill_skill_log "Step 3/4: Run 7 CLI journeys (REAL deploy E2E via \$LOOPZERO)"

# shellcheck source=infra/scripts/lib/drill-dogfood-journeys.sh
source "$SCRIPT_DIR/../lib/drill-dogfood-journeys.sh"

API_URL="https://api.${DRILL_BASE_DOMAIN:-loopzero-drill.example}/api/v1"
JOURNEYS_PASSED=0
JOURNEYS_FAILED=0

# EC-3: PROJECT_NAME unique per run (no hardcoded "demo-app" — re-runs would conflict)
PROJECT_NAME="dogfood-${LOOPZERO_DRILL_RUN_ID:-$(date +%s)}"
drill_skill_log "  PROJECT_NAME=$PROJECT_NAME"

# Resolve $LOOPZERO CLI (EC-5: validates node ≥ 20 + builds binary)
LOOPZERO=$(resolve_loopzero_cli "$ROOT_DIR") || {
  drill_skill_emit_finding "S0" "resolve_loopzero_cli failed — cannot run CLI-first journeys"
  drill_skill_teardown || true
  exit 5
}
drill_skill_log "  resolved \$LOOPZERO=$LOOPZERO"

# Auto-discover workload context (EC-7: not hardcoded)
WORKLOAD_CTX=$(discover_workload_context "$ROOT_DIR/infra/pulumi-drill")
MGMT_CTX="${MGMT_CONTEXT:-k8s-drill-mgmt}"
TENANT_NS="loopzero-${SEED_TENANT_ID:-tn-e2etest}-staging"
APP_DOMAIN="${PROJECT_NAME}-staging.${DRILL_BASE_DOMAIN:-loopzero-drill.example}"

# Configure $LOOPZERO to point at drill API (otherwise CLI defaults to prod/localhost)
export LOOPZERO_API_URL="$API_URL"

# J01: API health (no auth — pure curl probe; only journey not CLI-native)
J01_STATUS=$(curl -sS -m 30 -k -o /dev/null -w "%{http_code}" "${API_URL}/health" 2>/dev/null || echo "000")
if [[ "$J01_STATUS" == "200" ]]; then
  drill_skill_log "  ✓ J01 API health (HTTP 200)"
  JOURNEYS_PASSED=$((JOURNEYS_PASSED + 1))
else
  drill_skill_log "  ✗ J01 API health: HTTP $J01_STATUS (expected 200)"
  drill_skill_emit_finding "S0" "J01 API health: HTTP $J01_STATUS"
  JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
fi

# J02: loopzero login (REAL CLI auth)
run_cli_journey "J02" "loopzero login" \
  "$LOOPZERO login --token '$LOOPZERO_TOKEN' --json" 0 || true

# J03: loopzero project create (REAL CLI mutation)
run_cli_journey "J03" "loopzero project create $PROJECT_NAME" \
  "$LOOPZERO project create '$PROJECT_NAME' --json" 0 || true

# J04: loopzero deploy (REAL CLI deploy + Prova de Vida)
# Use bundled fixture as source
FIXTURE_DIR="$ROOT_DIR/infra/tests/e2e/fixtures/hello-server"
if [[ -d "$FIXTURE_DIR" ]]; then
  if (cd "$FIXTURE_DIR" && "$LOOPZERO" deploy --env staging --project "$PROJECT_NAME" --json >/dev/null 2>&1); then
    drill_skill_log "  ✓ J04 loopzero deploy (exit 0) — verifying Prova de Vida..."
    if prova_de_vida "$WORKLOAD_CTX" "$TENANT_NS" "$PROJECT_NAME" "https://${APP_DOMAIN}/health"; then
      JOURNEYS_PASSED=$((JOURNEYS_PASSED + 1))
    else
      drill_skill_emit_finding "S0" "J04 deploy: Prova de Vida FAIL (pod or HTTPS not alive)"
      JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
    fi
  else
    drill_skill_log "  ✗ J04 loopzero deploy: CLI exit non-zero"
    drill_skill_emit_finding "S0" "J04 deploy: loopzero deploy returned non-zero"
    JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
  fi
else
  drill_skill_log "  ✗ J04: fixture $FIXTURE_DIR not found"
  drill_skill_emit_finding "S0" "J04 fixture missing: $FIXTURE_DIR"
  JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
fi

# J05: loopzero scale (REAL CLI scale)
run_cli_journey "J05" "loopzero scale --replicas 2" \
  "$LOOPZERO scale --project '$PROJECT_NAME' --env staging --replicas 2 --json" 0 || true

# J06: loopzero rollback (REAL CLI rollback)
run_cli_journey "J06" "loopzero rollback" \
  "$LOOPZERO rollback --project '$PROJECT_NAME' --env staging --json" 0 || true

# J07: loopzero project delete + Prova de Morte
if "$LOOPZERO" project delete "$PROJECT_NAME" --confirm --json >/dev/null 2>&1; then
  drill_skill_log "  ✓ J07 loopzero project delete (exit 0) — verifying Prova de Morte..."
  if prova_de_morte "$WORKLOAD_CTX" "$MGMT_CTX" "$TENANT_NS" "$PROJECT_NAME" "$PROJECT_NAME"; then
    JOURNEYS_PASSED=$((JOURNEYS_PASSED + 1))
  else
    drill_skill_emit_finding "S0" "J07 delete: Prova de Morte FAIL (pod or ArgoCD app remained)"
    JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
  fi
else
  drill_skill_log "  ✗ J07 loopzero project delete: CLI exit non-zero"
  drill_skill_emit_finding "S0" "J07 delete: loopzero project delete returned non-zero"
  JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
fi

drill_skill_log "  Journey summary: $JOURNEYS_PASSED PASS / $JOURNEYS_FAILED FAIL of 7"

# Threshold 1/7 (per ADR D3 + SKILL.md T3.1)
if [[ "$JOURNEYS_FAILED" -gt 1 ]]; then
  drill_skill_emit_finding "S0" "Journey threshold exceeded: $JOURNEYS_FAILED failures (>1 allowed)"
  drill_skill_teardown || true
  exit 5
fi

# --- Step 4: 5 prod-parity invariants check ---
drill_skill_log "Step 4/4: 5 prod-parity invariants check"
# Use runtime cluster kubeconfig + seed tenant ns.
# Bug #27 LIVE FIX (2026-05-19): Bug #23 rewrite removed TENANT_ID variable
# (was "tn-dogfood-drill-..."). Re-derive from SEED_TENANT_ID (matches the
# tenant JWT was minted for + cell wiring).
RUNTIME_KC=/tmp/drill-kc/runtime.yaml
TENANT_NS="loopzero-${SEED_TENANT_ID:-tn-e2etest}-production"
if [[ -f "$RUNTIME_KC" ]]; then
  if drill_skill_check_invariants "$RUNTIME_KC" "$TENANT_NS"; then
    drill_skill_log "  ✓ All 5 invariants PASS"
  else
    violations=$?
    drill_skill_emit_finding "S0" "Invariants check: $violations violations"
    drill_skill_teardown || true
    exit 6
  fi
else
  drill_skill_log "  warn: runtime kubeconfig not found at $RUNTIME_KC — invariants skipped"
fi

# --- MANDATORY teardown ---
drill_skill_log "Mandatory teardown"
if ! drill_skill_teardown; then
  exit 7
fi

drill_skill_log ""
drill_skill_log "✓ dogfood-drill PASS"
drill_skill_log "  Journeys: $JOURNEYS_PASSED/7 PASS"
drill_skill_log "  Audit: $DRILL_SKILL_AUDIT_DIR"
exit 0
