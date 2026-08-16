#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 5 T5.3b — ClusterCredential
# bootstrap. Creates a kubeconfig Secret + ClusterCredential CR for a
# given cell on the mgmt cluster, so K8sCellStore.LoadCells() picks it
# up next refresh and DynamicRouter exposes the cell.
#
# Closes:
#   • T0.1 audit Gap 2 ("wire-clusters.sh registers ArgoCD but does
#     NOT create a ClusterCredential CR")
#   • T0.1 audit Action Items A2 (design lib) + A3 (wire into
#     wire-clusters.sh — done by edit immediately after this file ships)
#
# Why a separate lib script (mirroring scoped-kubeconfig.sh /
# argocd-register.sh): caller signature is uniform across the wiring
# pipeline; each lib does one job; failures are diagnosable per step.
#
# Usage:
#   clustercredential-create.sh <cell-id> <cluster-name> <kubeconfig-path>
#
#   <cell-id>          must match ^cell-[a-z0-9-]+$
#   <cluster-name>     human-friendly identifier, e.g. "runtime" / "runtime-2"
#   <kubeconfig-path>  full kubeconfig file for the target cluster
#                      (typically the scoped kubeconfig produced by
#                      scoped-kubeconfig.sh — least-privilege)
#
# Required env:
#   KUBECONFIG         must point at the MGMT cluster (where the CC + Secret live)
#
# Optional env:
#   LOOPZERO_NS            namespace for ClusterCredential + Secret (default: loopzero-system)
#   INGRESS_DOMAIN     loopzero.io/ingress-domain label value (default: empty)
#   ARGOCD_SCOPE       "true" | "false" (default: true) — enables CC.spec.scope.argocd
#   OPERATORS_SCOPE    "true" | "false" (default: true)
#   TLS_INSECURE       "true" | "false" (default: false). Sets
#                      CC.spec.tls.insecure — required for k3d-local clusters
#                      whose self-signed CAs rotate across container restarts
#                      (issue #67). Production (DOKS, Droplet) must leave it
#                      false to enforce TLS verification with the CA bundled
#                      in the kubeconfig.
#
# Idempotent: re-running with same args produces same end state
# (kubectl apply patches existing).
#
# Exit codes:
#   0 — CC + Secret applied; will be observed by K8sCellStore within 60s
#   1 — usage error
#   2 — kubectl unavailable / mgmt unreachable
#   3 — kubeconfig file missing or unreadable
#   4 — apply failed

set -uo pipefail

CELL_ID="${1:-}"
CLUSTER_NAME="${2:-}"
KUBECONFIG_PATH="${3:-}"

# Issue #117 (generalizing #115) — pin the target cluster BY CONSTRUCTION.
#
# Same asymmetry as argocd-register.sh: $KUBECONFIG_PATH is the credential FOR
# the cell being registered, while the ClusterCredential CR + its Secret must
# be created on MGMT, where the operators that consume them run. The header of
# this script used to merely *document* "KUBECONFIG must point at the MGMT
# cluster" and trust the caller — which is precisely the #117 shape: correct on
# a clean machine, silently wrong on a box carrying context residue. A
# ClusterCredential written to the wrong cluster is invisible to the operator
# reconcile loop, so the cell simply never comes up and nothing reports why.
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-core}"
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

LOOPZERO_NS="${LOOPZERO_NS:-loopzero-system}"
INGRESS_DOMAIN="${INGRESS_DOMAIN:-}"
ARGOCD_SCOPE="${ARGOCD_SCOPE:-true}"
OPERATORS_SCOPE="${OPERATORS_SCOPE:-true}"
TLS_INSECURE="${TLS_INSECURE:-false}"

# Validate TLS_INSECURE strictly — anything but "true"/"false" is a caller bug
# we want to surface, not silently coerce.
case "$TLS_INSECURE" in
  true|false) ;;
  *) fail "TLS_INSECURE must be 'true' or 'false' (got: '$TLS_INSECURE')" 1 ;;
esac

log() { printf '[cc-create] %s\n' "$*" >&2; }
fail() { log "ERROR: $*"; exit "${2:-1}"; }

# Usage gate.
[[ -n "$CELL_ID" && -n "$CLUSTER_NAME" && -n "$KUBECONFIG_PATH" ]] || \
  fail "usage: $0 <cell-id> <cluster-name> <kubeconfig-path>" 1

# Cell ID pattern — same regex as cells.cell_id CHECK constraint and
# ClusterCredentialSpec.CellID kubebuilder validation. Reject early to
# avoid creating CRs the K8s admission controller would reject anyway.
if ! [[ "$CELL_ID" =~ ^cell-[a-z0-9-]+$ ]]; then
  fail "invalid cell-id: $CELL_ID (must match ^cell-[a-z0-9-]+$)" 1
fi

# kubeconfig file must exist + be readable.
[[ -r "$KUBECONFIG_PATH" ]] || fail "kubeconfig not readable: $KUBECONFIG_PATH" 3

# kubectl available?
command -v kubectl >/dev/null 2>&1 || fail "kubectl not found in PATH" 2

# MGMT must be reachable. We do NOT switch KUBECONFIG away from the
# caller-provided env var — caller is expected to have set KUBECONFIG
# to the mgmt cluster before invoking this script (matches
# wire-clusters.sh idiom).
if ! kubectl cluster-info --request-timeout=5s >/dev/null 2>&1; then
  fail "mgmt cluster unreachable (KUBECONFIG=$KUBECONFIG)" 2
fi

# Extract API server URL from the target kubeconfig. Prefer yq, fallback
# to Python (matches argocd-register.sh + scoped-kubeconfig.sh pattern).
read_apiserver() {
  if command -v yq >/dev/null 2>&1; then
    yq -r '.clusters[0].cluster.server' "$KUBECONFIG_PATH"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "
import yaml, sys
with open('$KUBECONFIG_PATH') as f:
    doc = yaml.safe_load(f)
print(doc['clusters'][0]['cluster']['server'])
"
  else
    fail "neither yq nor python3 available — cannot parse kubeconfig" 2
  fi
}
APISERVER="$(read_apiserver)"
[[ -n "$APISERVER" ]] || fail "could not determine apiServer from kubeconfig" 3

# Derived names.
SECRET_NAME="${CLUSTER_NAME}-kubeconfig"
CC_NAME="${CLUSTER_NAME}"
INGRESS_LABEL_LINE=""
if [[ -n "$INGRESS_DOMAIN" ]]; then
  INGRESS_LABEL_LINE="    loopzero.io/ingress-domain: ${INGRESS_DOMAIN}"
fi

log "creating Secret ${LOOPZERO_NS}/${SECRET_NAME} from ${KUBECONFIG_PATH}"

# Step 1: kubeconfig Secret. K8sCellStore reads this Secret via
# CC.spec.secretRef. We use --dry-run=client | kubectl apply for
# idempotency (overwrites on rerun without `kubectl create` failing on
# AlreadyExists).
kubectl create secret generic "$SECRET_NAME" \
  -n "$LOOPZERO_NS" \
  --from-file=kubeconfig="$KUBECONFIG_PATH" \
  --dry-run=client -o yaml \
  | kubectl apply -f - >/dev/null \
  || fail "failed to apply Secret $SECRET_NAME" 4

log "creating ClusterCredential ${LOOPZERO_NS}/${CC_NAME} (cellID=${CELL_ID})"

# Step 2: ClusterCredential CR. The controller then probes the cluster
# and sets status.connected. We do NOT set status here — that's
# operator-managed.
#
# spec.scope.argocd: forwards kubeconfig to ArgoCD as a managed cluster.
# spec.scope.operators: makes kubeconfig accessible to operators (the
#   tenant-controller etc. read it to provision tenant resources).
cat <<EOF | kubectl apply -f - >/dev/null \
  || fail "failed to apply ClusterCredential $CC_NAME" 4
apiVersion: loopzero.io/v1
kind: ClusterCredential
metadata:
  name: ${CC_NAME}
  namespace: ${LOOPZERO_NS}
  labels:
    loopzero.io/cell: ${CELL_ID}
${INGRESS_LABEL_LINE}
spec:
  clusterName: ${CLUSTER_NAME}
  apiServer: ${APISERVER}
  cellID: ${CELL_ID}
  secretRef:
    name: ${SECRET_NAME}
    namespace: ${LOOPZERO_NS}
  scope:
    argocd: ${ARGOCD_SCOPE}
    operators: ${OPERATORS_SCOPE}
  tls:
    insecure: ${TLS_INSECURE}
EOF

log "applied — controller will probe within 60s; check via:"
log "  kubectl get clustercredential -n ${LOOPZERO_NS} ${CC_NAME} -o jsonpath='{.status.connected}'"
log "DONE"
exit 0
