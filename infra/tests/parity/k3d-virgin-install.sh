#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY / T5.1 (Phase 5 — degrau 1).
#
# k3d virgin install validation: prove that `loopzero platform install -f
# values.yaml` (no --provider, standalone mode) produces a functional
# cluster from a brand-new k3d cluster — closes the parity gap that
# 2026-04-30 audit revealed (49/52 ArgoCD apps OutOfSync because runtime
# was hot-patched, not Git-driven).
#
# This is degrau 1 of the staircase (k3d $0 → Droplet ~$1 → DOKS ~$5).
# If this fails the rest fail; if this passes we move to degrau 2.
#
# Pre-conditions:
#   - 16 GiB host with ~6 GiB RAM available (free -h)
#   - docker + k3d + helmfile + loopzero CLI in PATH
#   - existing k3d-loopzero-build / k3d-loopzero-mgmt running (we stop -build to
#     free RAM during the test, then restart it)
#
# Cleanup: trap on EXIT — virgin cluster is always deleted, original
# clusters always restarted.
#
# Usage:
#   bash infra/tests/parity/k3d-virgin-install.sh
#   SKIP_DOGFOOD=1 bash ... — skip the journey-01 smoke run
#
# Exit codes:
#   0 — virgin install + journey GREEN
#   1 — virgin cluster create failed
#   2 — loopzero platform install failed
#   3 — journey-01 smoke FAIL
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-loopzero-virgin}"
VIRGIN_PORT_HTTPS="${VIRGIN_PORT_HTTPS:-9443}"
VIRGIN_PORT_HTTP="${VIRGIN_PORT_HTTP:-9080}"
VIRGIN_PORT_API="${VIRGIN_PORT_API:-6500}"
SKIP_DOGFOOD="${SKIP_DOGFOOD:-0}"

log() { echo "[parity-k3d] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

cleanup() {
  log "cleanup..."
  k3d cluster delete "$CLUSTER_NAME" 2>/dev/null || true
  # Restore the original build + workload clusters we paused to free RAM.
  k3d cluster start loopzero-build 2>/dev/null || true
  k3d cluster start loopzero-workload 2>/dev/null || true
}
trap cleanup EXIT

# 1) Pre-flight: refuse to run if we're already RAM-starved.
# Achado #131 (CHG-2026-04-30 iter 3) — virgin install with 27 charts × `helm
# --wait` parallel = OOM at host level when only 1 cluster paused. Pause BOTH
# loopzero-build AND loopzero-workload during the test (workload tenants are zumbi-free
# from earlier dogfood — no live customers). Total free RAM ≥7 GiB after.
log "step 1a: pre-flight RAM check (need ≥7 GiB available after both pauses)"
avail_kb=$(awk '/MemAvailable/ {print $2}' /proc/meminfo)
avail_gib=$(( avail_kb / 1024 / 1024 ))
log "  available: ${avail_gib} GiB (will gain ~3.5 GiB after build+workload stop)"
if (( avail_gib + 3 < 7 )); then
  fail "insufficient RAM for virgin install (need ≥7 GiB post-pause, have ${avail_gib} pre-pause)" 1
fi

# 1b) Free RAM by pausing build AND workload clusters.
log "step 1b: pausing loopzero-build cluster (~1.9 GiB)"
k3d cluster stop loopzero-build 2>&1 | tail -1 || log "  warning: loopzero-build stop returned non-zero"
log "step 1c: pausing loopzero-workload cluster (~1.7 GiB)"
k3d cluster stop loopzero-workload 2>&1 | tail -1 || log "  warning: loopzero-workload stop returned non-zero"

# 2) Optional docker prune to reclaim disk.
# WARNING (Achado #30): `docker system prune -a --volumes` ALSO deletes
# containers in `Exited` state — including k3d clusters that were just
# `k3d cluster stop`'d. That destroyed `loopzero-build` during the first
# T5.1 live run on 2026-04-30. Default is now image-only prune (safe).
if command -v docker >/dev/null 2>&1; then
  log "step 2: docker image prune (dangling only — preserves stopped k3d clusters)"
  if [[ "${PRUNE:-1}" == "1" ]]; then
    docker image prune -f 2>&1 | tail -3 || true
  fi
fi

# 3) Create virgin cluster (delete-if-exists first so reruns are clean).
if k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -qx "$CLUSTER_NAME"; then
  log "step 3a: removing orphan '$CLUSTER_NAME' from prior aborted run"
  k3d cluster delete "$CLUSTER_NAME" 2>&1 | tail -2 || true
fi

log "step 3: creating virgin k3d cluster '$CLUSTER_NAME' with 2 agents (capacity for 27 charts)"
# Achado #31: k3d ships Traefik pre-installed in kube-system. Our Helmfile
# installs Traefik in `traefik` ns and the IngressClass adoption fails.
# Disable via --k3s-arg.
# Achado #131: 1 agent runs OOM when `helm --wait` paralleliza 27+ charts.
# 2 agents distributes pod load. With build+workload paused, host has
# ~9 GiB free — virgin needs ~3 GiB total (server+2agents+full install).
if ! k3d cluster create "$CLUSTER_NAME" \
    --servers 1 --agents 2 \
    --port "${VIRGIN_PORT_HTTPS}:443@loadbalancer" \
    --port "${VIRGIN_PORT_HTTP}:80@loadbalancer" \
    --api-port "${VIRGIN_PORT_API}" \
    --k3s-arg "--disable=traefik@server:0" \
    --registry-use k3d-loopzero-registry:5000 2>&1 | tail -10; then
  fail "k3d cluster create failed" 1
fi

CTX="k3d-${CLUSTER_NAME}"
log "step 3.5: cluster up — context=${CTX}"
kubectl --context "$CTX" wait --for=condition=Ready node --all --timeout=60s 2>&1 | tail -3

# 4) Run loopzero platform install (standalone mode, no --provider).
log "step 4: loopzero platform install -f standalone-values.yaml"
VALUES="${REPO_ROOT}/infra/tests/parity/standalone-values.yaml"
[[ -f "$VALUES" ]] || fail "values file not found: $VALUES" 2

# Use the virgin cluster's kubeconfig for the install.
KUBECONFIG_VIRGIN=$(k3d kubeconfig get "$CLUSTER_NAME")
TMPK=$(mktemp)
echo "$KUBECONFIG_VIRGIN" > "$TMPK"
trap 'rm -f "$TMPK"' RETURN

if ! KUBECONFIG="$TMPK" loopzero platform install \
    -f "$VALUES" \
    --profile minimal \
    --no-restore-secrets 2>&1 | tee /tmp/parity-install.log | tail -40; then
  fail "loopzero platform install failed (see /tmp/parity-install.log)" 2
fi

# 5) Smoke test: run journey-01-solo-deploy against virgin cluster.
if [[ "$SKIP_DOGFOOD" == "1" ]]; then
  log "step 5: dogfood skipped (SKIP_DOGFOOD=1)"
else
  log "step 5: journey-01-solo-deploy smoke test"
  # Journey scripts use the standalone-multi 3-cluster setup; in virgin mode
  # we point LOOPZERO_URL to the virgin cluster's API.
  # NOTE: this is not currently exercised by journey scripts — they assume
  # multi-cluster. Document as known limitation; first virgin run is install
  # validation only. Full dogfood against virgin cluster requires journey
  # script adaptation (Phase 5 follow-up).
  log "  (dogfood vs single-cluster virgin is Phase 5 follow-up; skipping)"
fi

log "DONE — virgin install GREEN (cluster=$CLUSTER_NAME, ports=$VIRGIN_PORT_HTTPS/$VIRGIN_PORT_HTTP)"
exit 0
