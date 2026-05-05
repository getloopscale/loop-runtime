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
#   35  — phase 2.5 RBAC patch failed (B3 fix; mgmt cluster context broken)
#   38  — phase 2.6 bootstrap extras failed (B9/B10/B11/B12/B13)
#   45  — phase 3.5 post-tilt env failed (B14; deploy/loopzero-api missing)
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
# B1 FIX (iter 1, 2026-05-05): log MUST live OUTSIDE .test-reports/ because
# `task clean` (called inside Phase 1 destroy via `task clean:all`) does
# `rm -rf .test-reports/`, which orphans the tee FD and silently drops every
# byte of subsequent output. Discovered LIVE during Phase 2 first-execution:
# orchestrator died in setup/tilt boundary with zero log evidence.
# Diagnosis from /proc/<tee-pid>/fd showed `(deleted)` on the log file ~1min
# into the run. Cleanroom logs go to .test-reports-cleanroom/ which `task
# clean` does NOT touch. State snapshots (post_validate) go to .test-reports/
# but the dir is re-created defensively in post_validate.
REPORT_DIR="${REPO_ROOT}/.test-reports"
LOG_DIR="${REPO_ROOT}/.test-reports-cleanroom"
TS="$(date +%Y%m%d-%H%M%S)"
LOG_FILE="${LOG_DIR}/dogfood-cleanroom-${TS}.log"
STATE_SNAPSHOT="${REPORT_DIR}/dogfood-cleanroom-${TS}-state.json"
EXIT_SENTINEL="${LOG_DIR}/dogfood-cleanroom-${TS}.exit"

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

mkdir -p "$LOG_DIR"
mkdir -p "$REPORT_DIR"
mkdir -p "$(dirname "$STATE_FILE")"

if [[ "$DRY_RUN" -eq 0 ]]; then
  exec > >(tee -a "$LOG_FILE") 2>&1
fi

# B1 FIX: write exit code to sentinel on every termination so failure is
# visible even if the log itself was somehow lost. EXIT_SENTINEL lives in
# LOG_DIR (safe from `task clean`).
write_exit_sentinel() {
  local rc=$?
  printf '%s exit=%d ts=%s\n' "$0" "$rc" "$(date -Iseconds)" >>"$EXIT_SENTINEL" 2>/dev/null || true
  return "$rc"
}
trap write_exit_sentinel EXIT

log "cleanroom loop starting (dry-run=${DRY_RUN}); log: ${LOG_FILE}; sentinel: ${EXIT_SENTINEL}"

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

# Phase 2.5: RBAC patch (B3 fix, iter 3, 2026-05-05)
#
# Tilt's `infra/tilt/api-deployment.yaml` ships a narrow ClusterRole for the
# loopzero-api SA — only `tenants` and `loopzeroapplications` from loopzero.io. Real
# loopzero-api needs `secrets` (for argocd-refresh-credentials, ESO, etc.) and
# `clustercredentials.loopzero.io` (for K8sCellStore.LoadCells, fatal at boot).
# Discovered LIVE during cleanroom Phase 3 iter 2: loopzero-api CrashLoopBackOff,
# tilt ci hit 600s timeout and exited 25.
#
# The proper fix is to update infra/tilt/api-deployment.yaml, but that path
# is OUT OF the current governance approval scope. Instead, this phase
# applies an ADDITIVE ClusterRole + Binding under a different name
# (loopzero-api-cleanroom-extra) so it does NOT conflict with Tilt's apply.
# RBAC bindings are additive across multiple ClusterRoleBindings — the SA
# gets the union of all rules from all bindings that target it.
#
# This is a workaround per ADR D2 of the plan. ADR-0022 D7 amendment
# (Trivial / Scope refinement): adds new phase to orchestrator, in-scope
# `infra/scripts/**`, no DoD impact. The canonical fix tracked separately;
# when in-scope governance includes infra/tilt/, the additive ClusterRole
# can be removed in favour of fixing api-deployment.yaml directly.
phase_rbac_patch() {
  log_phase "2.5/7 RBAC PATCH (B3 — additive ClusterRole for loopzero-api)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: kubectl apply loopzero-api-cleanroom-extra ClusterRole+Binding"
    return 0
  fi
  if ! kubectl --context k3d-loopzero-mgmt apply -f - <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: loopzero-api
  namespace: loopzero-system
  labels:
    app.kubernetes.io/name: loopzero-api
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: loopzero-api-cleanroom-extra
  labels:
    app.kubernetes.io/managed-by: dogfood-cleanroom-loop
    app.kubernetes.io/part-of: loopzero
rules:
  # K8sCellStore.LoadCells() polls every 60s; missing perms → fatal boot.
  - apiGroups: ["loopzero.io"]
    resources: ["clustercredentials"]
    verbs: ["get", "list", "watch"]
  # KubeMintingTokenProvider (self-healing ArgoCD token), ESO sync,
  # secret rotation, loopzero-cluster-kubeconfigs read/write.
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: loopzero-api-cleanroom-extra
  labels:
    app.kubernetes.io/managed-by: dogfood-cleanroom-loop
    app.kubernetes.io/part-of: loopzero
subjects:
  - kind: ServiceAccount
    name: loopzero-api
    namespace: loopzero-system
roleRef:
  kind: ClusterRole
  name: loopzero-api-cleanroom-extra
  apiGroup: rbac.authorization.k8s.io
YAML
  then
    log_err "phase_rbac_patch failed — kubectl apply error; mgmt cluster context broken?"
    return 35
  fi
  log "rbac patch applied: loopzero-api-cleanroom-extra"
}

# Phase 2.6: Bootstrap extras (B9/B10/B11/B12 fix, iter 9-11, 2026-05-05)
#
# `task setup` does NOT invoke the helmfile-side bootstrap scripts. Without
# these:
#   - B9: loopzero-db-credentials Secret missing → seed-local-user.sh fails with
#     empty PGPASS → preflight T0.2 fails
#   - B10: loopzero-mgmt-store ClusterSecretStore not Ready → preflight T0.9 fails
#   - B11: ESO CRDs absent in workload → bootstrap-eso-cluster-secret-store.sh
#     fails with `no matches for kind "ClusterSecretStore"`. Iter 10.
#   - B12: loopzero-bootstrap-credentials Secret missing (Hydra OAuth2 client
#     not registered) → seed-local-user.sh succeeds for DB inserts but
#     fails at JWT mint with empty `client_secret`. Iter 11. The token
#     remains stale → preflight T0.2 still FAIL even after B9 fixed.
#
# Sequence:
#   1. generate-secrets.sh (mgmt Secrets — fixes B9)
#   2. task k3d:bootstrap-workload (ESO in workload — fixes B11)
#   3. bootstrap-eso-cluster-secret-store.sh (ClusterSecretStore — B10)
#   4. bootstrap-oauth-clients.sh (Hydra OAuth2 clients + loopzero-bootstrap-
#      credentials Secret — fixes B12)
phase_bootstrap_extras() {
  log_phase "2.6/7 BOOTSTRAP EXTRAS (generate-secrets + workload ESO + ClusterSecretStore)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: generate-secrets + task k3d:bootstrap-workload + bootstrap-eso"
    return 0
  fi
  if ! bash "${REPO_ROOT}/infra/helmfile/scripts/generate-secrets.sh" standalone mgmt; then
    log_err "phase_bootstrap_extras: generate-secrets.sh failed"
    return 38
  fi
  log "generate-secrets.sh OK (loopzero-db-credentials, loopzero-api-tokens, etc. applied)"
  # B20 fix (iter 17, 2026-05-05): generate-secrets.sh OVERWRITES the real
  # Gitea token in loopzero-api-tokens.gitea-token with a random unregistered
  # value. provision-api-tokens.sh creates a token via Gitea API
  # (POST /users/.../tokens) and stores the real value. We must re-run it
  # after generate-secrets.sh to restore the registered token. Without this,
  # source upload fails with `gitea source: ensure repo: create repo:
  # status 401`.
  if ! bash "${REPO_ROOT}/infra/scripts/provision-api-tokens.sh" >/dev/null 2>&1; then
    log_err "phase_bootstrap_extras: provision-api-tokens.sh re-run failed"
    return 38
  fi
  log "provision-api-tokens.sh re-run OK (real Gitea + ArgoCD tokens restored)"
  # B21 fix (iter 18, 2026-05-05): generate cosign-signing-key Secret in
  # BUILD cluster (argo namespace). The script uses kubectl current
  # context, so we explicitly switch first. Without this Secret, the
  # sign-image step in Argo Workflow fails with `MountVolume.SetUp failed
  # for volume "cosign-signing-key"` and the workflow hangs forever.
  # generate-build-secrets.sh is idempotent (preserves existing key on
  # re-run).
  local _saved_ctx
  _saved_ctx=$(kubectl config current-context 2>/dev/null || echo "")
  kubectl config use-context k3d-loopzero-build >/dev/null 2>&1 || true
  if ! bash "${REPO_ROOT}/infra/helmfile/scripts/generate-build-secrets.sh" standalone; then
    [[ -n "$_saved_ctx" ]] && kubectl config use-context "$_saved_ctx" >/dev/null 2>&1 || true
    log_err "phase_bootstrap_extras: generate-build-secrets.sh failed"
    return 38
  fi
  [[ -n "$_saved_ctx" ]] && kubectl config use-context "$_saved_ctx" >/dev/null 2>&1 || true
  log "generate-build-secrets.sh OK (cosign-signing-key in build cluster argo ns)"
  if ! task k3d:bootstrap-workload; then
    log_err "phase_bootstrap_extras: task k3d:bootstrap-workload failed (ESO install in workload)"
    return 38
  fi
  log "k3d:bootstrap-workload OK (cert-manager + traefik + ESO in workload)"
  if ! bash "${REPO_ROOT}/infra/helmfile/scripts/bootstrap-eso-cluster-secret-store.sh"; then
    log_err "phase_bootstrap_extras: bootstrap-eso-cluster-secret-store.sh failed"
    return 38
  fi
  log "bootstrap-eso-cluster-secret-store.sh OK (loopzero-mgmt-store Ready)"
  # B15 fix (iter 12): bootstrap-oauth-clients.sh defaults
  # HYDRA_NAMESPACE=loopzero-auth, but standalone-multi installs Hydra in
  # loopzero-system. Override env var so the readiness probe targets the
  # right Service. Without this, script hits 120s timeout polling a
  # namespace that doesn't exist.
  if ! HYDRA_NAMESPACE=loopzero-system bash "${REPO_ROOT}/infra/helmfile/scripts/bootstrap-oauth-clients.sh"; then
    log_err "phase_bootstrap_extras: bootstrap-oauth-clients.sh failed"
    return 38
  fi
  log "bootstrap-oauth-clients.sh OK (loopzero-bootstrap-credentials present, Hydra clients registered)"
  # B16 fix (iter 12, 2026-05-05): install sigstore policy-controller chart
  # FIRST so its CRD `policy.sigstore.dev/v1beta1` ClusterImagePolicy is
  # available for the admission-policies chart to reference. Otherwise
  # `helm install admission-policies` fails with `no matches for kind
  # "ClusterImagePolicy"`.
  if ! helm repo list 2>/dev/null | grep -q '^sigstore'; then
    helm repo add sigstore https://sigstore.github.io/helm-charts >/dev/null 2>&1 || true
    helm repo update >/dev/null 2>&1 || true
  fi
  if ! helm upgrade --install policy-controller sigstore/policy-controller \
       --version 0.8.1 \
       --kube-context k3d-loopzero-workload \
       --namespace cosign-system --create-namespace \
       -f "${REPO_ROOT}/infra/helm/policy-controller/values.yaml" \
       --wait --timeout 3m; then
    log_err "phase_bootstrap_extras: policy-controller helm install failed"
    return 38
  fi
  log "policy-controller installed in workload (CRDs ClusterImagePolicy ready)"
  # B13/B18 fix (iter 11/14, 2026-05-05): install admission-policies chart in
  # workload cluster. B18: VAP loopzero-restrict-image-registries needs
  # additionalPrefixes to include k3d-loopzero-registry:5000/<tenant>/ so locally
  # built images pass admission. Without this, preflight T0.3.vap-restrict
  # fails with "VAP missing k3d-loopzero-registry:5000/ prefix".
  if ! helm upgrade --install admission-policies "${REPO_ROOT}/infra/helm/admission-policies" \
       --kube-context k3d-loopzero-workload \
       --namespace cosign-system --create-namespace \
       --set 'registries.additionalPrefixes[0]=k3d-loopzero-registry:5000/' \
       --wait --timeout 2m; then
    log_err "phase_bootstrap_extras: admission-policies helm install failed"
    return 38
  fi
  log "admission-policies installed in workload (VAPs + k3d-loopzero-registry prefix)"
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

# Phase 3.5: Post-tilt env (B14 fix, iter 11, 2026-05-05)
#
# preflight T0.7 expects LOOPZERO_SERVICE_AUTH_SECRET env var on the loopzero-api
# Deployment (consumed by api/internal/middleware/auth.go for service-auth
# routes; journey-11-admin requires it). The env var is NOT set in
# infra/tilt/api-deployment.yaml (Tiltfile YAML, out of governance scope
# for this plan). Workaround: kubectl set env after tilt creates the
# Deployment. Pod restarts with the env in place; preflight T0.7 then
# observes it.
phase_post_tilt_env() {
  log_phase "5.5/7 POST-TILT ENV (B14 — set LOOPZERO_SERVICE_AUTH_SECRET on loopzero-api)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: kubectl set env deploy/loopzero-api LOOPZERO_SERVICE_AUTH_SECRET=..."
    return 0
  fi
  # Stable cleanroom-only secret value. journey-11-admin reads from .env or
  # similar; this needs to match. Fallback to a deterministic placeholder.
  local secret="${LOOPZERO_SERVICE_AUTH_SECRET:-cleanroom-service-auth-secret-do-not-use-in-prod}"
  if ! kubectl --context k3d-loopzero-mgmt -n loopzero-system set env deploy/loopzero-api \
       "LOOPZERO_SERVICE_AUTH_SECRET=${secret}" >/dev/null 2>&1; then
    log_err "phase_post_tilt_env: kubectl set env failed (deploy/loopzero-api absent?)"
    return 45
  fi
  log "LOOPZERO_SERVICE_AUTH_SECRET set on deploy/loopzero-api"
  # Wait for rollout so pod has env when preflight inspects.
  kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status deploy/loopzero-api --timeout=60s >/dev/null 2>&1 || true
  # B17 fix (iter 14): pre-mint local-dogfood-token via seed-local-user.sh
  # with HYDRA_NAMESPACE=loopzero-system override. preflight.sh hardcodes
  # `port-forward -n loopzero-auth` and won't pass the env, so the token must
  # be fresh on disk before preflight runs (preflight skips re-mint when
  # token is valid). seed-local-user.sh is idempotent — DB inserts use
  # ON CONFLICT, JWT mint is fresh per call.
  if HYDRA_NAMESPACE=loopzero-system bash "${REPO_ROOT}/infra/helmfile/scripts/seed-local-user.sh" >/dev/null 2>&1; then
    log "seed-local-user.sh OK (~/.loopzero/local-dogfood-token.txt fresh)"
  else
    log_warn "seed-local-user.sh failed (preflight T0.2 may report stale token)"
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
# Invokes the existing 12-journey orchestrator.
#
# B5 FIX (iter 5, 2026-05-05): full-loop.sh --reset ONLY resets state and
# exits. A second invocation (without --reset) is required to actually run
# the journeys. Discovered LIVE during cleanroom iter 5: phase_dogfood took
# ~0s, post_validate found 0/12 journeys, exit code 60. Fix: call twice.
phase_dogfood() {
  log_phase "6/7 DOGFOOD (bash full-loop.sh — reset then run)"
  if [[ ! -x "$DOGFOOD_FULL_LOOP" ]]; then
    log_err "phase_dogfood: $DOGFOOD_FULL_LOOP missing or not executable"
    return 50
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "DRY-RUN: bash $DOGFOOD_FULL_LOOP --reset && bash $DOGFOOD_FULL_LOOP"
    return 0
  fi
  # Step 1: reset state (--reset exits 0 immediately after clearing state).
  if ! bash "$DOGFOOD_FULL_LOOP" --reset; then
    log_err "phase_dogfood: full-loop.sh --reset failed"
    return 50
  fi
  # Step 2: actually run the 12 journeys. Exit non-zero on any FAIL.
  if ! bash "$DOGFOOD_FULL_LOOP"; then
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

  # B1 FIX: re-create REPORT_DIR defensively — Phase 1 destroyed it.
  mkdir -p "$REPORT_DIR"

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

# B4 FIX (iter 4 → 5, 2026-05-05): phase_heal moved BEFORE phase_tilt_up.
# Heal H12/H13/H14/H15 create the cell-default/cell-1/cell-2 ClusterCredential
# CRs that loopzero-api reads at boot via K8sCellStore.LoadCells. With the old
# order (heal AFTER tilt), tilt ci called loopzero-api which failed fatally with
# `cell router: 0 ClusterCredentials discovered — fatal, no fallback by
# design`, and tilt ci hit 600s timeout before heal ever ran.
# Heal H8/H10 (loopzero-api Deployment env override) silently no-op when the
# Deployment doesn't yet exist (set -uo pipefail without -e; kubectl errors
# redirected to /dev/null). Tilt's later apply re-establishes correct env.
# ADR-0022 D7 amendment: DoD refinement of plan ADR D5 — heal still runs
# AFTER setup, just BEFORE tilt instead of after. Documented in plan.
phase_destroy           || exit $?
phase_setup             || exit $?
phase_rbac_patch        || exit $?
phase_bootstrap_extras  || exit $?
phase_heal              || exit $?
phase_tilt_up           || exit $?
phase_post_tilt_env     || exit $?
phase_wait_doctor       || exit $?
phase_dogfood           || exit $?
post_validate           || exit $?

log "sequence COMPLETE in ${SECONDS}s"
log "all phases passed; report: ${LOG_FILE}; state: ${STATE_SNAPSHOT}"
exit 0
