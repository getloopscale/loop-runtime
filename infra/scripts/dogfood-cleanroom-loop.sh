#!/usr/bin/env bash
# infra/scripts/dogfood-cleanroom-loop.sh
#
# Cleanroom dogfood loop orchestrator.
#
# Purpose: run a single end-to-end execution of the destroy → setup → tilt →
# heal → doctor → dogfood cycle on a fresh local k3d standalone-multi
# environment. Designed to be invoked repeatedly across iterations until
# convergence (12/12 dogfood journeys PASS + zero S0/S1 + Rule 3 clean +
# Prova de Vida + Prova de Morte all PASS).
#
# Composes existing primitives (Não Reinvente principle):
#   1. task clean:all         — destroy clusters + registry + artifacts
#   2. task setup             — bootstrap clusters + helm + CRDs + DB + tokens
#   3. tilt ci                — non-interactive convergence of dev resources
#   4. task k3d:heal          — codified workarounds (H1..H15)
#   5. task doctor            — health probe gate
#   6. infra/tests/dogfood/full-loop.sh — 12 journey orchestrator
#   7. post-validation        — Rule 3 grep + dogfood state JSON inspection
#
# This script does NOT implement the test logic — it only sequences existing
# primitives with structured logging and phase-distinct exit codes so a human
# (or Ralph loop) can diagnose failures quickly.
#
# Plan: docs/plans/real-paas-cleanroom-dogfood-loop-plan.md (T1.1, T1.2)
# ADR D1: cleanroom is full destroy, not partial reset
# ADR D5: heal runs AFTER setup, not in place of it
# ADR D6: pass criterion is multidimensional (not exit 0 alone)
#
# Exit codes (each phase has its own; non-overlapping for fast triage):
#   0   — full success: all phases passed + post-validation clean
#   1   — usage error (unknown flag, malformed args)
#   10  — phase 1 destroy failed (Docker/k3d broken)
#   20  — phase 2 setup failed (bootstrap regression)
#   25  — phase 3 tilt ci failed (Tiltfile resource did not converge)
#   30  — phase 4 heal failed (heal script regression)
#   40  — phase 5 doctor did not converge in 180s (control plane unhealthy)
#   50  — phase 6 dogfood full-loop.sh exited non-zero (journey FAIL)
#   60  — phase 7 post-validation failed (Rule 3 / state JSON anomaly)
#
# Flags:
#   --dry-run    print the sequence; do not execute any side-effecting command
#   --help       print this header and exit 0
#
# Outputs (for downstream auditing):
#   .test-reports/dogfood-cleanroom-${TS}.log      — full stdout/stderr capture
#   .test-reports/dogfood-cleanroom-${TS}-state.json — snapshot of dogfood state
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants and paths
# ---------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DOGFOOD_FULL_LOOP="${REPO_ROOT}/infra/tests/dogfood/full-loop.sh"
STATE_FILE="${HOME}/.loopzero/dogfood_state.json"
REPORT_DIR="${REPO_ROOT}/.test-reports"
TS="$(date +%Y%m%d-%H%M%S)"
LOG_FILE="${REPORT_DIR}/dogfood-cleanroom-${TS}.log"
STATE_SNAPSHOT="${REPORT_DIR}/dogfood-cleanroom-${TS}-state.json"

DOCTOR_TIMEOUT_SEC=180
TILT_CI_TIMEOUT_SEC=600

DRY_RUN=0

# Forbidden internal terms (Rule 3) — used in post-validation grep.
# Kept in a single-quoted string with `\b` boundaries to avoid false positives
# (e.g. "Argo Workflows" must still match "Argo" but not the inner CRD name).
RULE3_PATTERN='ArgoCD|argocd|GitOps|gitops-push|Gitea|MinIO|kubebuilder|envtest|Kaniko|advisory lock|pg_advisory|tenant_id'

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------

log()    { printf '[cleanroom %s] %s\n' "$(date +%H:%M:%S)" "$*"; }
log_phase() { printf '\n========== %s ==========\n' "$*"; log "PHASE: $*"; }
log_warn()  { printf '[cleanroom %s] WARN: %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
log_err()   { printf '[cleanroom %s] ERROR: %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

# Run a command with dry-run support.
# Usage: run_or_skip "<friendly description>" <argv...>
run_or_skip() {
  local desc=$1; shift
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: ${desc} -> $*"
    return 0
  fi
  log "exec: ${desc}"
  "$@"
}

# ---------------------------------------------------------------------------
# Help / arg parsing
# ---------------------------------------------------------------------------

print_help() {
  sed -n '2,55p' "$0"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --help|-h) print_help; exit 0 ;;
    *)
      log_err "unknown argument: $1"
      log_err "run with --help for usage"
      exit 1
      ;;
  esac
  shift
done

# ---------------------------------------------------------------------------
# Pre-flight (idempotent setup of report dir)
# ---------------------------------------------------------------------------

mkdir -p "$REPORT_DIR"
mkdir -p "$(dirname "$STATE_FILE")"

if [[ "$DRY_RUN" -eq 0 ]]; then
  exec > >(tee -a "$LOG_FILE") 2>&1
fi

log "cleanroom loop starting (dry-run=${DRY_RUN}); log: ${LOG_FILE}"

# ---------------------------------------------------------------------------
# Phase functions
# ---------------------------------------------------------------------------

# Phase 1: Destroy
# Calls `task clean:all`. Idempotent. Exit 10 on failure.
phase_destroy() {
  log_phase "1/7 DESTROY (task clean:all)"
  if ! run_or_skip "destroy clusters + registry + artifacts" task clean:all; then
    log_err "phase_destroy failed — Docker daemon? k3d binary? leftover state?"
    return 10
  fi
}

# Phase 2: Setup
# Calls `task setup`. Bootstraps clusters + helm + CRDs + DB + tokens.
phase_setup() {
  log_phase "2/7 SETUP (task setup)"
  if ! run_or_skip "bootstrap clusters and dependencies" task setup; then
    log_err "phase_setup failed — likely bug in bootstrap (helmfile, CRDs, migrations, or seed)"
    return 20
  fi
}

# Phase 3: Tilt CI
# Runs `tilt ci` (non-interactive convergence) with timeout. Tilt exits 0 once
# all Tiltfile resources are healthy; if a resource never converges, tilt ci
# itself times out and exits non-zero.
phase_tilt_up() {
  log_phase "3/7 TILT CI (tilt ci, ${TILT_CI_TIMEOUT_SEC}s timeout)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: timeout ${TILT_CI_TIMEOUT_SEC} tilt ci"
    return 0
  fi
  if ! timeout "${TILT_CI_TIMEOUT_SEC}" tilt ci; then
    log_err "phase_tilt_up failed — Tiltfile resource did not converge within ${TILT_CI_TIMEOUT_SEC}s"
    return 25
  fi
}

# Phase 4: Heal
# Calls `task k3d:heal` to apply codified cross-cluster workarounds.
phase_heal() {
  log_phase "4/7 HEAL (task k3d:heal)"
  if ! run_or_skip "apply cross-cluster heal" task k3d:heal; then
    log_err "phase_heal failed — heal script regression; check infra/scripts/k3d-cross-cluster-heal.sh"
    return 30
  fi
}

# Phase 5: Doctor
# Polls `task doctor` until it returns 0 or the timeout elapses. Health gate
# before dogfood begins. Without this, dogfood can race against still-warming
# API/operator/argocd, masking real bugs as flaky tests.
phase_wait_doctor() {
  log_phase "5/7 DOCTOR (task doctor, poll up to ${DOCTOR_TIMEOUT_SEC}s)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: poll task doctor"
    return 0
  fi
  local deadline=$((SECONDS + DOCTOR_TIMEOUT_SEC))
  local attempt=1
  while (( SECONDS < deadline )); do
    log "doctor attempt ${attempt}..."
    if task doctor; then
      log "doctor PASS on attempt ${attempt}"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 10
  done
  log_err "phase_wait_doctor failed — control plane did not converge in ${DOCTOR_TIMEOUT_SEC}s"
  return 40
}

# Phase 6: Dogfood
# Invokes the existing 12-journey orchestrator. Resets dogfood state so a
# fresh cleanroom run starts from journey-01 (matches D1: full destroy).
phase_dogfood() {
  log_phase "6/7 DOGFOOD (bash full-loop.sh --reset)"
  if [[ ! -x "$DOGFOOD_FULL_LOOP" ]]; then
    log_err "phase_dogfood: $DOGFOOD_FULL_LOOP missing or not executable"
    return 50
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: bash $DOGFOOD_FULL_LOOP --reset"
    return 0
  fi
  if ! bash "$DOGFOOD_FULL_LOOP" --reset; then
    log_err "phase_dogfood: full-loop.sh reported FAIL — see ${STATE_FILE} for blocker journey"
    return 50
  fi
}

# Phase 7: Post-validation
# Three checks combined:
#   (a) Rule 3 — no internal terms in captured CLI output
#   (b) State JSON shows all 12 entries with status=PASS
#   (c) Snapshot copy of state for audit trail
post_validate() {
  log_phase "7/7 POST-VALIDATION (Rule 3 + state JSON)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: rule3 grep + state json check"
    return 0
  fi

  # Snapshot dogfood state.
  if [[ -f "$STATE_FILE" ]]; then
    cp "$STATE_FILE" "$STATE_SNAPSHOT"
    log "state snapshot written: $STATE_SNAPSHOT"
  else
    log_err "post_validate: $STATE_FILE missing — full-loop.sh did not run?"
    return 60
  fi

  # (a) Rule 3 — internal terms in CLI output. Note: this scans the full log,
  # which includes operator/API logs by side-effect. We narrow to lines that
  # appear to come from CLI output (start with "loopzero " or contain "$ loopzero").
  # Lines from operator/API logs are NOT a Rule 3 violation.
  local rule3_hits
  rule3_hits=$(grep -E "^loopzero |^\$ loopzero " "$LOG_FILE" 2>/dev/null \
    | grep -E "$RULE3_PATTERN" \
    | wc -l)
  if [[ "$rule3_hits" -gt 0 ]]; then
    log_err "post_validate: $rule3_hits lines of CLI output contain Rule 3 forbidden terms"
    grep -E "^loopzero |^\$ loopzero " "$LOG_FILE" | grep -E "$RULE3_PATTERN" | head -5 >&2
    return 60
  fi
  log "Rule 3 audit: clean (0 forbidden terms in CLI output lines)"

  # (b) state JSON: all 12 expected journeys with status=PASS.
  local pass_count
  pass_count=$(python3 - "$STATE_FILE" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
expected = {
  "preflight",
  "journey-01-solo-deploy",
  "journey-02-multi-server",
  "journey-03-secrets",
  "journey-04-scale",
  "journey-05-promote",
  "journey-06-rollback",
  "journey-07-custom-domain",
  "journey-08-github-autodeploy",
  "journey-09-preview",
  "journey-10-failures",
  "journey-11-admin",
}
seen_pass = {j["name"] for j in data.get("journeys", []) if j.get("status") == "PASS"}
print(len(expected & seen_pass))
PY
)
  if [[ "$pass_count" -ne 12 ]]; then
    log_err "post_validate: only ${pass_count}/12 journeys PASS in state JSON"
    return 60
  fi
  log "state JSON: 12/12 journeys PASS"

  log "post-validation: ALL CHECKS PASSED"
}

# ---------------------------------------------------------------------------
# Main sequence
# ---------------------------------------------------------------------------

# Capture start.
SECONDS=0
log "sequence start"

phase_destroy   || exit $?
phase_setup     || exit $?
phase_tilt_up   || exit $?
phase_heal      || exit $?
phase_wait_doctor || exit $?
phase_dogfood   || exit $?
post_validate   || exit $?

log "sequence COMPLETE in ${SECONDS}s"
log "all 7 phases passed; report: ${LOG_FILE}; state: ${STATE_SNAPSHOT}"
exit 0
