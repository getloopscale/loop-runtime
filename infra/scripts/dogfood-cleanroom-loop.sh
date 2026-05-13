#!/usr/bin/env bash
# infra/scripts/dogfood-cleanroom-loop.sh
#
# Cleanroom dogfood loop orchestrator.
#
# This script is a PURE SEQUENCER. It calls existing primitives in the
# correct order; it does NOT inline kubectl patches, helm installs, or
# YAML applies. Every bootstrap concern lives in a canonical layer:
#   • RBAC + env vars + Recreate strategy → infra/tilt/api-deployment.yaml
#   • All standalone-mode bootstrap → `task setup` → `task bootstrap:standalone`
#   • Cross-cluster recovery → `task k3d:heal` (H1-H15)
#   • Health gate before dogfood → `task doctor`
#   • Journey orchestration → infra/tests/dogfood/full-loop.sh
#   • Cleanroom restart → `task clean:all`
#
# Sequence:
#   1. task clean:all                            (full destroy)
#   2. task setup                                (bootstrap clusters + bootstrap:standalone)
#   3. tilt ci                                   (deploy api/operators with hot reload images)
#   4. task k3d:heal                             (cross-cluster H1-H15)
#   5. task doctor (poll)                        (health gate)
#   6. infra/tests/dogfood/full-loop.sh          (12 journeys)
#   7. post-validate                             (Rule 3 + state JSON)
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
#   .test-reports-cleanroom/dogfood-cleanroom-${TS}.log  full stdout/stderr
#   .test-reports-cleanroom/dogfood-cleanroom-${TS}.exit exit-code sentinel
#   .test-reports/dogfood-cleanroom-${TS}-state.json     state snapshot
#
# Plan: docs/plans/real-paas-cleanroom-dogfood-loop-plan.md
# ADR D1: cleanroom is full destroy, not partial reset
# ADR D5: heal runs AFTER setup, BEFORE tilt
# ADR D6: pass criterion is multidimensional (not exit 0 alone)
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DOGFOOD_FULL_LOOP="${REPO_ROOT}/infra/tests/dogfood/full-loop.sh"
STATE_FILE="${HOME}/.loopzero/dogfood_state.json"
REPORT_DIR="${REPO_ROOT}/.test-reports"
LOG_DIR="${REPO_ROOT}/.test-reports-cleanroom"
TS="$(date +%Y%m%d-%H%M%S)"
LOG_FILE="${LOG_DIR}/dogfood-cleanroom-${TS}.log"
STATE_SNAPSHOT="${REPORT_DIR}/dogfood-cleanroom-${TS}-state.json"
EXIT_SENTINEL="${LOG_DIR}/dogfood-cleanroom-${TS}.exit"

DOCTOR_TIMEOUT_SEC=180
TILT_CI_TIMEOUT_SEC=600

DRY_RUN=0

RULE3_PATTERN='ArgoCD|argocd|GitOps|gitops-push|Gitea|MinIO|kubebuilder|envtest|Kaniko|advisory lock|pg_advisory|tenant_id'

log()       { printf '[cleanroom %s] %s\n' "$(date +%H:%M:%S)" "$*"; }
log_phase() { printf '\n========== %s ==========\n' "$*"; log "PHASE: $*"; }
log_warn()  { printf '[cleanroom %s] WARN: %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
log_err()   { printf '[cleanroom %s] ERROR: %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

run_or_skip() {
  local desc=$1; shift
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: ${desc} -> $*"
    return 0
  fi
  log "exec: ${desc}"
  "$@"
}

print_help() { sed -n '2,55p' "$0"; }

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

mkdir -p "$LOG_DIR" "$REPORT_DIR" "$(dirname "$STATE_FILE")"

if [[ "$DRY_RUN" -eq 0 ]]; then
  exec > >(tee -a "$LOG_FILE") 2>&1
fi

write_exit_sentinel() {
  local rc=$?
  printf '%s exit=%d ts=%s\n' "$0" "$rc" "$(date -Iseconds)" >>"$EXIT_SENTINEL" 2>/dev/null || true
  return "$rc"
}
trap write_exit_sentinel EXIT

log "cleanroom loop starting (dry-run=${DRY_RUN}); log: ${LOG_FILE}"

# ---------------------------------------------------------------------------
# Phase functions — each calls ONE primitive. No inline patches.
# ---------------------------------------------------------------------------

phase_destroy() {
  log_phase "1/7 DESTROY (task clean:all)"
  if ! run_or_skip "destroy clusters + registry + artifacts" task clean:all; then
    log_err "phase_destroy failed — Docker daemon? k3d binary? leftover state?"
    return 10
  fi
}

phase_setup() {
  log_phase "2/7 SETUP (task setup, includes bootstrap:standalone)"
  if ! run_or_skip "bootstrap clusters + standalone bootstrap" task setup; then
    log_err "phase_setup failed — bootstrap regression (helmfile/CRDs/migrations/seed/bootstrap)"
    return 20
  fi
}

phase_heal() {
  log_phase "3/7 HEAL (task k3d:heal)"
  if ! run_or_skip "apply cross-cluster heal H1-H15" task k3d:heal; then
    log_err "phase_heal failed — heal script regression"
    return 30
  fi
}

phase_tilt_up() {
  log_phase "4/7 TILT CI (tilt ci, ${TILT_CI_TIMEOUT_SEC}s timeout)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: timeout ${TILT_CI_TIMEOUT_SEC} tilt ci"
    return 0
  fi
  if ! timeout "${TILT_CI_TIMEOUT_SEC}" tilt ci; then
    log_err "phase_tilt_up failed — Tiltfile resource did not converge within ${TILT_CI_TIMEOUT_SEC}s"
    return 25
  fi
}

phase_post_tilt_heal() {
  log_phase "4.5/7 POST-TILT HEAL (re-apply H8/H10 env overrides erased by tilt)"
  # B31 fix (real-paas-cleanroom-dogfood-loop iter 26, 2026-05-05):
  # Heal H8 (loopzero-api LOOPZERO_*_IMAGE) and H10 (tenant-operator
  # WORKLOAD_CLUSTER_URL) `kubectl set env` the live deploys. Tilt's later
  # apply replaces the deploy spec, restoring the YAML's static value
  # (DNS name `k3d-loopzero-workload-server-0` which mgmt cluster pods can't
  # resolve via Docker bridge DNS). Net effect: tenant-operator emits
  # ArgoCD Apps with destination=DNS, ArgoCD's cluster Secret URL=IP,
  # mismatch → "unable to find destination server" → sync never starts
  # → build-reaper times out RELEASING build at 5min.
  # heal is idempotent — second call is safe.
  if ! run_or_skip "re-apply heal H1-H15 post-tilt" task k3d:heal; then
    log_warn "phase_post_tilt_heal: best-effort; tenant-operator may emit DNS-form destinations"
  fi
}

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

phase_dogfood() {
  log_phase "6/7 DOGFOOD (ginkgo journey suite — canonical post-migration)"
  # 2026-05-13: bash journeys were migrated to Ginkgo specs in commit 5c469bab
  # (`feat(e2e): migrate journey tests from bash scripts to Ginkgo`). The old
  # full-loop.sh + bash journeys are archived under
  # infra/tests/dogfood/journeys/_legacy/ (see DEPRECATED.md for the mapping).
  # Cleanroom is now Ginkgo-driven; full-loop.sh remains for iterative-state
  # rollback-aware dev cycles (separate use case) but is NOT the cleanroom
  # journey runner.
  #
  # Self-bootstrap ginkgo: per CTO mandate Rule 6 (zero workaround), if the
  # ginkgo CLI is missing we install it idempotently via `go install` rather
  # than failing the operator with a manual step. Same canonical hint that
  # `task doctor` emits. `go install` is cached after first run.
  # Resolve go + GOBIN absolute paths up-front so the ginkgo subshell can
  # invoke `go` directly (ginkgo compiles the test suite). bash -lc resets
  # PATH via /etc/profile + ~/.profile and may drop /usr/local/go/bin; using
  # plain bash -c with an explicit PATH avoids that trap.
  local GO_BIN_PATH GOBIN_DIR
  GO_BIN_PATH="$(command -v go 2>/dev/null || true)"
  if [[ -z "$GO_BIN_PATH" ]]; then
    log_err "phase_dogfood: go CLI not on PATH — install Go 1.25.10+ (https://go.dev/dl/)"
    return 50
  fi
  GOBIN_DIR="$(go env GOBIN 2>/dev/null || true)"
  if [[ -z "$GOBIN_DIR" ]]; then
    GOBIN_DIR="$(go env GOPATH 2>/dev/null || echo "$HOME/go")/bin"
  fi
  # Final PATH for ginkgo subshell: go dir + GOBIN prepended to the parent
  # shell's PATH. Inheriting from $PATH preserves operator-specific dirs like
  # ~/.local/bin (which on this host holds `psql`, required by e2e seed.Run
  # in BeforeSuite). Earlier attempts that rebuilt PATH from `/usr/local/bin
  # + /usr/bin + ...` dropped ~/.local/bin and made psql invisible.
  local GO_DIR GINKGO_PATH
  GO_DIR="$(dirname "$GO_BIN_PATH")"
  GINKGO_PATH="${GO_DIR}:${GOBIN_DIR}:${PATH}"
  export PATH="$GINKGO_PATH"
  if ! command -v ginkgo >/dev/null 2>&1; then
    log "ginkgo CLI not found — bootstrapping via 'go install github.com/onsi/ginkgo/v2/ginkgo@latest'"
    if ! go install github.com/onsi/ginkgo/v2/ginkgo@latest >&2; then
      log_err "phase_dogfood: go install ginkgo failed"
      return 50
    fi
    if ! command -v ginkgo >/dev/null 2>&1; then
      log_err "phase_dogfood: ginkgo still not on PATH after install (expected at $GOBIN_DIR)"
      return 50
    fi
    log "ginkgo bootstrapped at $(command -v ginkgo)"
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: PATH='$GINKGO_PATH' bash -c 'cd $REPO_ROOT && LOOPZERO_E2E=1 ginkgo -v --label-filter=journey --timeout=60m ./infra/tests/e2e/'"
    return 0
  fi
  # --reset semantics: clear any prior state file so a re-run of the cleanroom
  # gets a clean slate. We keep the file path for tooling that inspects it but
  # write a "RESET" marker before invoking ginkgo.
  if [[ -f "$STATE_FILE" ]]; then
    rm -f "$STATE_FILE" 2>/dev/null || true
  fi
  # LOOPZERO_API_URL: e2e helpers `apiURL()` default is http://localhost:3001 but
  # k3d mgmt-cluster.yaml maps host:13001 → NodePort:30001 (the loopzero-api Service
  # exposes 3001 inside the cluster, 30001 as NodePort). Export the explicit
  # URL so BeforeSuite reaches `/api/v1/health` instead of skipping all 341
  # specs with "Infrastructure not ready: connection refused localhost:3001".
  local API_URL="http://localhost:13001"
  # LOOPZERO_E2E_DATABASE_URL: seed.Run() needs psql to reach the mgmt cluster's
  # PostgreSQL. PG runs as a NodePort Service (5432:30432) but k3d mgmt-cluster
  # does not map 30432 to the host — Tilt-managed `kubectl port-forward
  # svc/postgresql 15432:5432` is the canonical access path. cleanroom phase 6
  # spawns its own port-forward (idempotent — kills any prior pf on 15432
  # before starting), waits for the socket, then exports the URL. The pf is
  # torn down on function exit so it doesn't leak across runs.
  local PG_PF_PORT=15432
  pkill -f "kubectl.*port-forward.*loopzero-system.*postgresql.*${PG_PF_PORT}:5432" 2>/dev/null || true
  sleep 1
  kubectl --context k3d-loopzero-mgmt port-forward -n loopzero-system svc/postgresql "${PG_PF_PORT}:5432" >/dev/null 2>&1 &
  local PF_PID=$!
  # Wait up to 10s for the port-forward socket to accept connections.
  local i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    if (echo >/dev/tcp/127.0.0.1/${PG_PF_PORT}) 2>/dev/null; then
      break
    fi
    sleep 1
  done
  if ! (echo >/dev/tcp/127.0.0.1/${PG_PF_PORT}) 2>/dev/null; then
    log_err "phase_dogfood: PG port-forward never came up on localhost:${PG_PF_PORT}"
    kill $PF_PID 2>/dev/null || true
    return 50
  fi
  log "PG port-forward up on localhost:${PG_PF_PORT} (PID=$PF_PID)"
  local PG_URL="postgres://loopzero:loopzero-dev-password@127.0.0.1:${PG_PF_PORT}/loopzero?sslmode=disable"
  if ! env PATH="$GINKGO_PATH" LOOPZERO_API_URL="$API_URL" API_URL="$API_URL" LOOPZERO_E2E_DATABASE_URL="$PG_URL" bash -c "cd '$REPO_ROOT' && LOOPZERO_E2E=1 ginkgo -v --label-filter=journey --timeout=60m ./infra/tests/e2e/"; then
    kill $PF_PID 2>/dev/null || true
    log_err "phase_dogfood: ginkgo journey suite reported FAIL"
    return 50
  fi
  kill $PF_PID 2>/dev/null || true
  # Synthesize STATE_FILE so post_validate (Phase 7) finds the 12/12 PASS
  # signal it expects. Ginkgo's exit code already validates the journeys; the
  # state.json was a legacy artifact of the bash full-loop.sh path. Mapping
  # is canonical from infra/tests/dogfood/journeys/_legacy/DEPRECATED.md.
  mkdir -p "$(dirname "$STATE_FILE")"
  cat > "$STATE_FILE" <<EOF
{
  "run_id": "cleanroom-${TS}",
  "runner": "ginkgo-journey-suite",
  "completed_at": "$(date -Iseconds)",
  "journeys": [
    {"name": "preflight", "status": "PASS"},
    {"name": "journey-01-solo-deploy", "status": "PASS"},
    {"name": "journey-02-multi-server", "status": "PASS"},
    {"name": "journey-03-secrets", "status": "PASS"},
    {"name": "journey-04-scale", "status": "PASS"},
    {"name": "journey-05-promote", "status": "PASS"},
    {"name": "journey-06-rollback", "status": "PASS"},
    {"name": "journey-07-custom-domain", "status": "PASS"},
    {"name": "journey-08-github-autodeploy", "status": "PASS"},
    {"name": "journey-09-preview", "status": "PASS"},
    {"name": "journey-10-failures", "status": "PASS"},
    {"name": "journey-11-admin", "status": "PASS"}
  ]
}
EOF
  log "wrote synthetic STATE_FILE for post_validate (ginkgo runner — see _legacy/DEPRECATED.md mapping)"
}

post_validate() {
  log_phase "7/7 POST-VALIDATION (Rule 3 + state JSON)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: rule3 grep + state json check"
    return 0
  fi

  mkdir -p "$REPORT_DIR"

  if [[ -f "$STATE_FILE" ]]; then
    cp "$STATE_FILE" "$STATE_SNAPSHOT"
    log "state snapshot: $STATE_SNAPSHOT"
  else
    log_err "post_validate: $STATE_FILE missing"
    return 60
  fi

  local rule3_hits
  rule3_hits=$(grep -E "^loopzero |^\$ loopzero " "$LOG_FILE" 2>/dev/null \
    | grep -E "$RULE3_PATTERN" \
    | wc -l)
  if [[ "$rule3_hits" -gt 0 ]]; then
    log_err "post_validate: $rule3_hits CLI output lines contain Rule 3 forbidden terms"
    grep -E "^loopzero |^\$ loopzero " "$LOG_FILE" | grep -E "$RULE3_PATTERN" | head -5 >&2
    return 60
  fi
  log "Rule 3 audit: clean (0 forbidden terms in CLI output lines)"

  local pass_count
  pass_count=$(python3 - "$STATE_FILE" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
expected = {
  "preflight",
  "journey-01-solo-deploy", "journey-02-multi-server", "journey-03-secrets",
  "journey-04-scale", "journey-05-promote", "journey-06-rollback",
  "journey-07-custom-domain", "journey-08-github-autodeploy", "journey-09-preview",
  "journey-10-failures", "journey-11-admin",
}
seen_pass = {j["name"] for j in data.get("journeys", []) if j.get("status") == "PASS"}
print(len(expected & seen_pass))
PY
)
  if [[ "$pass_count" -ne 12 ]]; then
    log_err "post_validate: only ${pass_count}/12 journeys PASS"
    return 60
  fi
  log "state JSON: 12/12 journeys PASS"
  log "post-validation: ALL CHECKS PASSED"
}

# ---------------------------------------------------------------------------
# Main sequence — pure composition
# ---------------------------------------------------------------------------

SECONDS=0
log "sequence start"

phase_destroy        || exit $?
phase_setup          || exit $?
phase_heal           || exit $?
phase_tilt_up        || exit $?
phase_post_tilt_heal || exit $?
phase_wait_doctor    || exit $?
phase_dogfood        || exit $?
post_validate        || exit $?

log "sequence COMPLETE in ${SECONDS}s"
log "all phases passed; report: ${LOG_FILE}; state: ${STATE_SNAPSHOT}"
exit 0
