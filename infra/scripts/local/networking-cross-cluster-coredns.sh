#!/usr/bin/env bash
# T2.3 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# Patches mgmt cluster CoreDNS NodeHosts ConfigMap to resolve cross-cluster
# Service LB hostnames. Closes catch #11 from T6.2 session (memory
# j17-aprendizado-coredns-cross-cluster-build-lb): mgmt CoreDNS cannot
# resolve `k3d-loopzero-build-serverlb:30094` (J-17 Loki cross-cluster path)
# without explicit NodeHosts entries.
#
# Per ADR D2: idempotent — diff-then-patch via `kubectl patch --dry-run=client`
# pre-apply. Re-run = no-op if entries already present.
#
# Mappings (canonical T6.2 session):
#   k3d-loopzero-build-serverlb     → docker IP of k3d-loopzero-build-serverlb container
#   k3d-loopzero-workload-serverlb  → docker IP of k3d-loopzero-workload-server-0
#   k3d-loopzero-vault-serverlb     → docker IP of k3d-loopzero-vault-server-0 (if exists)
#
# Wired in: taskfiles/k3d.yml as `task k3d:networking:cross-cluster-coredns`.

set -uo pipefail

CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
COREDNS_NS="${COREDNS_NS:-kube-system}"
COREDNS_CM="${COREDNS_CM:-coredns}"
DRY_RUN="${DRY_RUN:-0}"

log() { printf '[networking-cross-cluster-coredns] %s\n' "$*" >&2; }

# === Resolve docker container IPs ===
declare -A HOSTS
for container in k3d-loopzero-build-serverlb k3d-loopzero-workload-server-0 k3d-loopzero-vault-server-0; do
  IP=$(docker inspect "$container" -f '{{ range .NetworkSettings.Networks }}{{ .IPAddress }}{{ end }}' 2>/dev/null | head -1)
  if [[ -n "$IP" && "$IP" != "<no value>" ]]; then
    HOSTS["$container"]="$IP"
    # Map common alias (serverlb → server-0 for compat)
    case "$container" in
      k3d-loopzero-workload-server-0)
        HOSTS["k3d-loopzero-workload-serverlb"]="$IP"
        ;;
      k3d-loopzero-vault-server-0)
        HOSTS["k3d-loopzero-vault-serverlb"]="$IP"
        ;;
    esac
  fi
done

if [[ ${#HOSTS[@]} -eq 0 ]]; then
  log "FAIL: no k3d-loopzero-* docker containers found. Run \`task setup\` first."
  exit 1
fi

# === Build canonical NodeHosts block ===
NODEHOSTS_BLOCK=""
for host in "${!HOSTS[@]}"; do
  NODEHOSTS_BLOCK+="${HOSTS[$host]} ${host}"$'\n'
done

log "Computed NodeHosts entries:"
printf '%s' "$NODEHOSTS_BLOCK" >&2

# === Diff-then-patch via Python (CoreDNS Corefile structure preservation) ===
CURRENT=$(kubectl --context "$CONTEXT" -n "$COREDNS_NS" get cm "$COREDNS_CM" -o jsonpath='{.data.NodeHosts}' 2>/dev/null || echo "")

# Merge logic homed in networking_cross_cluster_coredns.py (D2 — kubectl stays here).
HELPER="$(dirname "$0")/networking_cross_cluster_coredns.py"
PATCH=$(NCC_CURRENT="$CURRENT" NCC_TARGET="$NODEHOSTS_BLOCK" python3 "$HELPER" merge)

if [[ "$PATCH" == "$CURRENT" ]]; then
  log "Skip: CoreDNS NodeHosts already canonical (no diff)"
  exit 0
fi

log "Applying CoreDNS NodeHosts patch..."
if [[ "$DRY_RUN" -eq 0 ]]; then
  # Read live ConfigMap → set data.NodeHosts in the .py transform → apply.
  kubectl --context "$CONTEXT" -n "$COREDNS_NS" get cm "$COREDNS_CM" -o yaml | \
    NCC_NODEHOSTS="$PATCH" python3 "$HELPER" set-nodehosts | \
    kubectl --context "$CONTEXT" -n "$COREDNS_NS" apply -f -
  kubectl --context "$CONTEXT" -n "$COREDNS_NS" rollout restart deployment "$COREDNS_CM"
fi

log "OK: CoreDNS NodeHosts patched. Validate: kubectl --context $CONTEXT exec <pod> -- nslookup k3d-loopzero-build-serverlb"
