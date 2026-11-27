#!/usr/bin/env bash
# dev-public-seed-post-cleanroom-tenant-cr_test.sh
#
# Plan: seed-post-cleanroom-tenant-cr-provisioning (v1.0)
# Closes: Red Team loop-console#118 — QA Red Team identity logs into dev-public but
#         the tenant CR is absent in k3d-loop-runtime-core → deploys fail because
#         tenant-operator cannot reconcile a nonexistent tenant.
#
# PER `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` UNBREAKABLE:
# this LIVE bash is the canonical contract — it MUST reproduce the issue
# verbatim BEFORE the fix lands (RED) and pass AFTER the fix lands (GREEN).
#
# OPERATOR HARD STOP — per .claude/rules/cleanroom-up-live-test-mandate.md.
# Requires:
#   - k3d-loop-runtime-core cluster Ready (Cleanroom TOTAL → UP chain prior)
#   - seed-post-cleanroom-all.sh just succeeded (this test is the post-condition)
#
# Scenarios:
#   A — admin@getloopscale.com's Tenant CR (tn-243c747b30c41e51) present
#   B — paulohenrique.java@gmail.com's Tenant CR (tn-ffdf1c7f4cc0168e) present  ← the bug
#
# Pre-fix expectation: A passes, B fails (NotFound) → 1/2 PASS, exit non-zero.
# Post-fix expectation: A and B pass → 2/2 PASS, exit 0.
#
# Flags:
#   --skip-live          short-circuit before kubectl (CI/hermetic mode)
#   --scenario=A|B       run only the named scenario
#   --json               emit machine-readable result on stdout
#   --help               show this help and exit 0

set -euo pipefail

# --- Constants --------------------------------------------------------------

ISSUE_REF="loop-console#118"
PLAN_SLUG="seed-post-cleanroom-tenant-cr-provisioning"
SCRIPT_NAME="$(basename "$0")"

KUBE_CONTEXT="${KUBE_CONTEXT:-k3d-loop-runtime-core}"
NAMESPACE="${NAMESPACE:-loop-runtime-system}"
APIVERSION_GROUP="tenants.runtime.getloopscale.com"

# Canonical tenant_ids — pinned LIVE 2026-06-29 against dev-public PG state.
# These are the deterministic outputs of seed-admin-loop-runtime-dev.sh for the
# canonical EMAIL=admin@getloopscale.com and EMAIL=paulohenrique.java@gmail.com
# seed runs respectively (per seed-admin script lines 230-280: tenant_id is
# derived from the Kratos identity id via a stable canonical format).
TENANT_ADMIN="tn-243c747b30c41e51"
TENANT_REDTEAM="tn-ffdf1c7f4cc0168e"

SKIP_LIVE=0
SCENARIO=""
JSON_OUTPUT=0

# --- Parse flags ------------------------------------------------------------

show_help() {
  sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//' | sed '$d'
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ;;
    --scenario=A|--scenario=B) SCENARIO="${1#--scenario=}" ;;
    --json) JSON_OUTPUT=1 ;;
    --help|-h) show_help ;;
    *) echo "ERROR: unknown flag: $1" >&2 ; echo "use --help" >&2 ; exit 1 ;;
  esac
  shift
done

# --- Logging ----------------------------------------------------------------

log() {
  if [ "$JSON_OUTPUT" = "0" ]; then
    printf '[%s] %s\n' "$SCRIPT_NAME" "$*" >&2
  fi
}

fail_count=0
pass_count=0
scenarios_run=()
scenarios_failed=()

record_pass() {
  pass_count=$((pass_count + 1))
  scenarios_run+=("$1")
  log "  PASS  $1"
}

record_fail() {
  fail_count=$((fail_count + 1))
  scenarios_run+=("$1")
  scenarios_failed+=("$1")
  log "  FAIL  $1 — $2"
}

# --- Skip-live short-circuit (hermetic CI mode) -----------------------------

if [ "$SKIP_LIVE" = "1" ]; then
  log "[--skip-live] hermetic mode — NOT touching kubectl; structural shape only"
  log "RESULT: PASS (hermetic skip)"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"pass":1,"fail":0,"skipped":true,"reason":"--skip-live"}\n'
  fi
  exit 0
fi

# --- Pre-flight -------------------------------------------------------------

if ! command -v kubectl >/dev/null 2>&1; then
  log "FATAL: kubectl not in PATH"
  exit 2
fi

if ! kubectl config get-contexts "$KUBE_CONTEXT" >/dev/null 2>&1; then
  log "FATAL: kube context '$KUBE_CONTEXT' not configured (run Cleanroom TOTAL first)"
  exit 2
fi

# Probe API resource presence — if CRD itself missing, the cluster isn't
# bootstrapped. Fail fast with operator-actionable message.
if ! kubectl --context "$KUBE_CONTEXT" api-resources 2>/dev/null | grep -q "^tenants[[:space:]].*runtime\.getloopscale\.com"; then
  log "FATAL: tenants.runtime.getloopscale.com CRD not installed on $KUBE_CONTEXT"
  log "       run 'task setup' or 'task k3d:bootstrap-cells' to install CRDs"
  exit 2
fi

# --- Scenario functions -----------------------------------------------------

scenario_a() {
  local tenant="$TENANT_ADMIN"
  local label="A (admin@getloopscale.com tenant CR present)"
  log "Scenario $label — tenant=$tenant"

  if ! kubectl --context "$KUBE_CONTEXT" -n "$NAMESPACE" get tenants.runtime.getloopscale.com "$tenant" >/dev/null 2>&1; then
    record_fail "A" "$tenant NotFound in $KUBE_CONTEXT/$NAMESPACE (seed-admin-loop-runtime-dev.sh did not provision it)"
    return 1
  fi

  local phase
  phase="$(kubectl --context "$KUBE_CONTEXT" -n "$NAMESPACE" get tenants.runtime.getloopscale.com "$tenant" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  case "$phase" in
    Active|Provisioning) record_pass "A (phase=$phase)" ; return 0 ;;
    "") record_fail "A" "$tenant exists but status.phase is empty (controller not reconciling)" ; return 1 ;;
    *) record_fail "A" "$tenant phase=$phase (expected Active|Provisioning)" ; return 1 ;;
  esac
}

scenario_b() {
  local tenant="$TENANT_REDTEAM"
  local label="B (paulohenrique.java@gmail.com tenant CR present — closes #118)"
  log "Scenario $label — tenant=$tenant"

  if ! kubectl --context "$KUBE_CONTEXT" -n "$NAMESPACE" get tenants.runtime.getloopscale.com "$tenant" >/dev/null 2>&1; then
    record_fail "B" "$tenant NotFound in $KUBE_CONTEXT/$NAMESPACE — this is the $ISSUE_REF bug (seed Step 3b did not fire EnsureTenant)"
    return 1
  fi

  local phase
  phase="$(kubectl --context "$KUBE_CONTEXT" -n "$NAMESPACE" get tenants.runtime.getloopscale.com "$tenant" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  case "$phase" in
    Active|Provisioning) record_pass "B (phase=$phase)" ; return 0 ;;
    "") record_fail "B" "$tenant exists but status.phase is empty (controller not reconciling)" ; return 1 ;;
    *) record_fail "B" "$tenant phase=$phase (expected Active|Provisioning)" ; return 1 ;;
  esac
}

# --- Main -------------------------------------------------------------------

log "============================================================"
log "Plan: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Context: $KUBE_CONTEXT  Namespace: $NAMESPACE  APIGroup: $APIVERSION_GROUP"
log "============================================================"

case "$SCENARIO" in
  A) scenario_a || true ;;
  B) scenario_b || true ;;
  "") scenario_a || true ; scenario_b || true ;;
esac

log "============================================================"
log "Result: ${pass_count} PASS, ${fail_count} FAIL"
if [ "${#scenarios_failed[@]}" -gt 0 ]; then
  log "Failed: ${scenarios_failed[*]}"
fi
log "============================================================"

if [ "$JSON_OUTPUT" = "1" ]; then
  # shellcheck disable=SC2034
  local_failed_joined="$(printf '"%s",' "${scenarios_failed[@]:-}" | sed 's/,$//')"
  printf '{"pass":%d,"fail":%d,"failed":[%s],"issue":"%s","plan":"%s"}\n' \
    "$pass_count" "$fail_count" "${local_failed_joined:-}" "$ISSUE_REF" "$PLAN_SLUG"
fi

# Exit code: 0 if all PASS, 1 if any FAIL.
[ "$fail_count" -eq 0 ]
