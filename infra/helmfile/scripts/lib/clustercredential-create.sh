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
#   MANAGED_BY         app.kubernetes.io/managed-by label value (default: empty)
#   API_SERVER         spec.apiServer (default: clusters[0].cluster.server of
#                      <kubeconfig-path>). Drill passes its stack's endpoint.
#   MGMT_KUBECONFIG    reach MGMT through this kubeconfig file instead of the
#                      KUBECTL_CONTEXT context (drill: the DOKS mgmt kubeconfig)
#   DRY_RUN            "true" renders the Secret + ClusterCredential to stdout
#                      and touches no cluster (default: false)
#
# This is the ONLY constructor of a cell's ClusterCredential. The local and
# drill bootstrap libraries (infra/scripts/lib/bootstrap-cell-cc*.sh) call it;
# they used to carry heredoc copies whose labels and TLS settings disagreed.
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
MGMT_KUBECONFIG="${MGMT_KUBECONFIG:-}"
if [[ -n "$MGMT_KUBECONFIG" ]]; then
  # Drill: MGMT is reached through its own kubeconfig file, pinned to the context
  # that file names. Resolved below, once fail() exists, and never inherited.
  export KUBECONFIG="$MGMT_KUBECONFIG"
  KUBECTL_CONTEXT=""
fi
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

log() { printf '[cc-create] %s\n' "$*" >&2; }
fail() { log "ERROR: $*"; exit "${2:-1}"; }

LOOPZERO_NS="${LOOPZERO_NS:-loopzero-system}"
INGRESS_DOMAIN="${INGRESS_DOMAIN:-}"
MANAGED_BY="${MANAGED_BY:-}"
ARGOCD_SCOPE="${ARGOCD_SCOPE:-true}"
OPERATORS_SCOPE="${OPERATORS_SCOPE:-true}"
TLS_INSECURE="${TLS_INSECURE:-false}"
DRY_RUN="${DRY_RUN:-false}"

# Validate the booleans strictly — anything but "true"/"false" is a caller bug we
# want to surface, not silently coerce.
case "$TLS_INSECURE" in
  true|false) ;;
  *) fail "TLS_INSECURE must be 'true' or 'false' (got: '$TLS_INSECURE')" 1 ;;
esac
case "$DRY_RUN" in
  true|false) ;;
  *) fail "DRY_RUN must be 'true' or 'false' (got: '$DRY_RUN')" 1 ;;
esac

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

if [[ "$DRY_RUN" == "false" ]]; then
  command -v kubectl >/dev/null 2>&1 || fail "kubectl not found in PATH" 2
  if [[ -n "$MGMT_KUBECONFIG" ]]; then
    KUBECTL_CONTEXT="$(command kubectl config current-context 2>/dev/null || true)"
    [[ -n "$KUBECTL_CONTEXT" ]] || fail "MGMT_KUBECONFIG=$MGMT_KUBECONFIG names no current-context" 2
  fi
  if ! kubectl cluster-info --request-timeout=5s >/dev/null 2>&1; then
    fail "mgmt cluster unreachable (context=${MGMT_KUBECONFIG:-$KUBECTL_CONTEXT})" 2
  fi
fi

# Extract API server URL from the target kubeconfig unless the caller supplied
# one. Prefer yq, fallback to Python (matches argocd-register.sh +
# scoped-kubeconfig.sh pattern).
read_apiserver() {
  if command -v yq >/dev/null 2>&1; then
    yq -r '.clusters[0].cluster.server' "$KUBECONFIG_PATH"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "
import yaml, sys
with open(sys.argv[1]) as f:
    doc = yaml.safe_load(f)
print(doc['clusters'][0]['cluster']['server'])
" "$KUBECONFIG_PATH"
  else
    fail "neither yq nor python3 available — cannot parse kubeconfig" 2
  fi
}

APISERVER="${API_SERVER:-$(read_apiserver)}"
[[ -n "$APISERVER" ]] || fail "could not determine apiServer from kubeconfig" 3

# Derived names.
SECRET_NAME="${CLUSTER_NAME}-kubeconfig"
CC_NAME="${CLUSTER_NAME}"

# The canonical label set: loopzero.io/cell always (the multi-cell runbook selects
# on it), the ingress domain and the manager when the caller names them.
LABELS="    loopzero.io/cell: ${CELL_ID}"
if [[ -n "$INGRESS_DOMAIN" ]]; then
  LABELS+=$'\n'"    loopzero.io/ingress-domain: ${INGRESS_DOMAIN}"
fi
if [[ -n "$MANAGED_BY" ]]; then
  LABELS+=$'\n'"    app.kubernetes.io/managed-by: ${MANAGED_BY}"
fi

KUBECONFIG_B64="$(base64 -w0 <"$KUBECONFIG_PATH")" || fail "cannot read $KUBECONFIG_PATH" 3

# Step 1: kubeconfig Secret. K8sCellStore reads it via CC.spec.secretRef.
SECRET_MANIFEST="apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  namespace: ${LOOPZERO_NS}
  labels:
${LABELS}
type: Opaque
data:
  kubeconfig: ${KUBECONFIG_B64}"

# Step 2: ClusterCredential CR. The controller then probes the cluster and
# sets status.connected. We do NOT set status here — that's operator-managed.
#
# spec.scope.argocd: forwards kubeconfig to ArgoCD as a managed cluster.
# spec.scope.operators: makes kubeconfig accessible to operators (the
#   tenant-controller etc. read it to provision tenant resources).
CC_MANIFEST="apiVersion: loopzero.io/v1
kind: ClusterCredential
metadata:
  name: ${CC_NAME}
  namespace: ${LOOPZERO_NS}
  labels:
${LABELS}
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
    insecure: ${TLS_INSECURE}"

if [[ "$DRY_RUN" == "true" ]]; then
  printf '%s\n---\n%s\n' "$SECRET_MANIFEST" "$CC_MANIFEST"
  exit 0
fi

log "applying Secret ${LOOPZERO_NS}/${SECRET_NAME} from ${KUBECONFIG_PATH}"
printf '%s\n' "$SECRET_MANIFEST" | kubectl apply -f - >/dev/null \
  || fail "failed to apply Secret $SECRET_NAME" 4

log "applying ClusterCredential ${LOOPZERO_NS}/${CC_NAME} (cellID=${CELL_ID})"
printf '%s\n' "$CC_MANIFEST" | kubectl apply -f - >/dev/null \
  || fail "failed to apply ClusterCredential $CC_NAME" 4

log "applied — controller will probe within 60s; check via:"
log "  kubectl get clustercredential -n ${LOOPZERO_NS} ${CC_NAME} -o jsonpath='{.status.connected}'"
log "DONE"
exit 0
