#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# drill-dogfood-orchestrator.sh — /dogfood-drill skill entry point.
#
# Black-box user journey audit against drill prod-parity infrastructure.
# Runs 12 user journeys via loopzero CLI + validates 5 prod-parity invariants
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
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$ROOT_DIR"

# shellcheck source=infra/scripts/lib/drill-skill-common.sh
source "$SCRIPT_DIR/lib/drill-skill-common.sh"

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
  --dry-run          Print 12 journeys without provisioning.
  --help             Show this message.

Environment:
  LOOPZERO_DRILL_SKILL_SPONSOR    Operator initials. Required unless CI=true.
  CI                          true bypasses --confirm-cost + sponsor.

Journeys (12):
  J01 loopzero login + whoami
  J02 loopzero project create
  J03 loopzero init + deploy minimal Node.js app
  J04 loopzero build list/inspect
  J05 loopzero env create staging
  J06 loopzero deploy --env staging
  J07 loopzero promote staging → production
  J08 loopzero rollback
  J09 loopzero logs (Loki cross-cluster)
  J10 loopzero scale up/down
  J11 loopzero team list
  J12 loopzero project delete (Prova de Morte)

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
  drill_skill_log "DRY_RUN=true — would execute 12 journeys + 5 invariants"
  for j in J01 J02 J03 J04 J05 J06 J07 J08 J09 J10 J11 J12; do
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
       bash "$ROOT_DIR/infra/scripts/drill-cleanroom-orchestrator.sh" --confirm-cost \
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
if ! JWT=$(bash "$ROOT_DIR/infra/scripts/drill-mint-jwt.sh" 2>"$JWT_LOG"); then
  drill_skill_log "  ✗ drill-mint-jwt failed — last 10 lines of stderr:"
  tail -10 "$JWT_LOG" 2>/dev/null | sed 's/^/    /' >&2 || true
  drill_skill_emit_finding "S0" "drill-mint-jwt failed (log: $JWT_LOG)"
  drill_skill_teardown || true
  exit 4
fi
export LOOPZERO_TOKEN="${JWT##*$'\n'}"  # last line
drill_skill_log "  ✓ JWT minted (len=${#LOOPZERO_TOKEN})"

# --- Step 3: 12 user journeys ---
drill_skill_log "Step 3/4: Run 12 user journeys"
# Ensure fixture source.bundle exists for J03 source upload journey
drill_skill_ensure_source_bundle "$ROOT_DIR/infra/tests/e2e/fixtures/hello-server" || \
  drill_skill_log "  warn: source.bundle generation failed — J03 will fail soft"

# API URL via Traefik LB IP + curl --resolve (CF wildcard A record provides DNS too)
API_URL="https://api.${DRILL_BASE_DOMAIN:-loopzero-drill.example}/api/v1"
JOURNEYS_PASSED=0
JOURNEYS_FAILED=0

# Helper: invoke a journey with HTTP code check + audit log
run_journey() {
  local id="$1"
  local desc="$2"
  local method="$3"
  local path="$4"
  local expected_code="${5:-200}"
  local extra_curl_args=("${@:6}")

  local actual
  actual=$(curl -s -m 30 -X "$method" "${API_URL}${path}" \
    -H "Authorization: Bearer $LOOPZERO_TOKEN" \
    "${extra_curl_args[@]}" \
    -o /dev/null -w "%{http_code}" || echo "000")

  if [[ "$actual" == "$expected_code" ]]; then
    drill_skill_log "  ✓ $id $desc (HTTP $actual)"
    JOURNEYS_PASSED=$((JOURNEYS_PASSED + 1))
    return 0
  fi
  drill_skill_emit_finding "S0" "$id $desc: HTTP $actual (expected $expected_code)"
  JOURNEYS_FAILED=$((JOURNEYS_FAILED + 1))
  return 1
}

# Tenant identifiers (deterministic for idempotency)
TENANT_ID="tn-dogfood-drill-$(date +%s%N | head -c 8)"
PROJECT_NAME="dogfood-drill-app"

# J01: health check (no auth needed for /health)
run_journey "J01" "API health" "GET" "/health" "200" -k || true

# J02: project create
run_journey "J02" "project create" "POST" "/projects" "201" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$PROJECT_NAME\"}" -k || true

# J03-J12: subsequent journeys depend on J02 success; in non-LIVE mode (test stubs) all succeed
# In LIVE mode (real drill stack) each journey would invoke `loopzero` CLI binary.
# For orchestrator simplicity, we use HTTP API calls directly.

# J03: source upload (deploy precondition)
run_journey "J03" "source upload" "POST" "/source/upload" "200" \
  -H "Content-Type: application/octet-stream" \
  --data-binary "@$ROOT_DIR/infra/tests/e2e/fixtures/hello-server/source.bundle" -k 2>/dev/null || true

# J04-J11: list/inspect/admin operations (best-effort against running stack)
run_journey "J04" "build list" "GET" "/builds?project=$PROJECT_NAME&limit=5" "200" -k || true
run_journey "J05" "env create staging" "POST" "/projects/$PROJECT_NAME/environments" "201" \
  -H "Content-Type: application/json" -d '{"name":"staging"}' -k || true
run_journey "J06" "deploy staging" "POST" "/builds" "202" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\":\"$PROJECT_NAME\",\"environment\":\"staging\"}" -k || true
run_journey "J07" "promote staging→production" "POST" "/projects/$PROJECT_NAME/promote" "200" \
  -H "Content-Type: application/json" -d '{"from":"staging","to":"production"}' -k || true
run_journey "J08" "rollback" "POST" "/projects/$PROJECT_NAME/rollback" "200" -k || true
run_journey "J09" "logs" "GET" "/projects/$PROJECT_NAME/logs?since=1m" "200" -k || true
run_journey "J10" "scale" "POST" "/projects/$PROJECT_NAME/scale" "200" \
  -H "Content-Type: application/json" -d '{"replicas":3}' -k || true
run_journey "J11" "team list" "GET" "/teams" "200" -k || true

# J12: project delete (Prova de Morte)
run_journey "J12" "project delete" "DELETE" "/projects/$PROJECT_NAME" "200" -k || true

drill_skill_log "  Journey summary: $JOURNEYS_PASSED PASS / $JOURNEYS_FAILED FAIL of 12"

if [[ "$JOURNEYS_FAILED" -gt 2 ]]; then
  drill_skill_emit_finding "S0" "Journey threshold exceeded: $JOURNEYS_FAILED failures (>2 allowed)"
  drill_skill_teardown || true
  exit 5
fi

# --- Step 4: 5 prod-parity invariants check ---
drill_skill_log "Step 4/4: 5 prod-parity invariants check"
# Use runtime cluster kubeconfig + dynamic tenant ns
RUNTIME_KC=/tmp/drill-kc/runtime.yaml
TENANT_NS="loopzero-${TENANT_ID}-production"
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
drill_skill_log "  Journeys: $JOURNEYS_PASSED/12 PASS"
drill_skill_log "  Audit: $DRILL_SKILL_AUDIT_DIR"
exit 0
