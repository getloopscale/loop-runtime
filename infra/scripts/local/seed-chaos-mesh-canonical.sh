#!/usr/bin/env bash
# seed-chaos-mesh-canonical.sh
#
# T1.1 of plan m2-promote-11-canonical-seeds (v1.1 SHIPPABLE 95.2).
# Canonical chaos-mesh helm install in k3d-loopruntime-core cluster.
#
# Idempotent: helm upgrade --install is upgrade-no-op when chart already
# installed at same version. Pre-check `helm list` avoids round-trip on warm.
# `values-k3d.yaml` override provides socketPath = /run/k3s/containerd/
# containerd.sock per `CLAUDE.md § Known issues` (chaos-mesh 2.7.0 LIVE-validated
# 2026-05-23 6/6 pods Running on all 3 k3d clusters).
#
# Flags:
#   --verify   probe 6/6 pods Running post-install
#   --dry-run  print intended actions without mutation
#   --help|-h  print this header
#
# Hermetic: NO network beyond helm + kubectl. Idempotent on warm cluster.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
CORE_CONTEXT="${KUBE_CONTEXT:-k3d-loopruntime-core}"
VERIFY=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-chaos-mesh] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

# Idempotency pre-check: existing release detection
if helm list -n chaos-mesh --kube-context "$CORE_CONTEXT" 2>/dev/null | grep -q '^chaos-mesh'; then
  log "chaos-mesh release already exists (upgrade-no-op semantic for same chart version)"
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY: helm upgrade --install chaos-mesh $REPO_ROOT/infra/helm/chaos-mesh --kube-context $CORE_CONTEXT -n chaos-mesh --create-namespace -f values.yaml -f values-k3d.yaml"
  exit 0
fi

helm upgrade --install chaos-mesh "$REPO_ROOT/infra/helm/chaos-mesh" \
  --kube-context "$CORE_CONTEXT" \
  -n chaos-mesh \
  --create-namespace \
  --wait --timeout 5m \
  -f "$REPO_ROOT/infra/helm/chaos-mesh/values.yaml" \
  -f "$REPO_ROOT/infra/helm/chaos-mesh/values-k3d.yaml"

if [[ "$VERIFY" == "1" ]]; then
  log "verifying 6/6 pods Running..."
  running=$(kubectl get pods -n chaos-mesh --context "$CORE_CONTEXT" -o jsonpath='{.items[*].status.phase}' 2>/dev/null | tr ' ' '\n' | grep -c "^Running$" || true)
  if [[ "$running" -lt 6 ]]; then
    log "FAIL — only $running pods Running (expected ≥ 6 per CLAUDE.md § Known issues)"
    exit 1
  fi
  log "PASS — $running pods Running"
fi
