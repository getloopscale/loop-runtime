#!/usr/bin/env bash
# bootstrap-sigstore-local.sh — Idempotent LOCAL Sigstore stack setup for k3d.
#
# Production deploys Sigstore via Pulumi `pulumi-platform/src/services/sigstore.ts`
# (requires offline TUF ceremony per ADR-2026-05-27-T2.2-OFFLINE-TUF). LOCAL k3d
# uses sigstore-scaffold helm chart + sigstore-extras (NetworkPolicies) + dynamic
# Fulcio CACert config.
#
# Run after `task setup` (which provisions the build cluster). Order:
#   1. Helm install scaffold (fulcio + rekor + ctlog + trillian)
#   2. Wait for Fulcio pod Ready
#   3. Fetch K3s server CA from kube-public/cluster-info OR kube-root-ca.crt
#   4. Patch Fulcio ConfigMap with OIDCIssuer.CACert (enables K8s SA OIDC trust)
#   5. Helm install sigstore-extras (NetworkPolicies — kube-router fix)
#   6. K3s --anonymous-auth + ClusterRoleBinding for unauthenticated OIDC discovery
#
# This script is idempotent: re-running on existing state is safe.
#
# 2026-05-28 Ralph iter 5: created to persist M07 fixes pre-cleanroom so that
# `task setup && bash bootstrap-sigstore-local.sh` produces LIVE keyless GREEN
# from scratch (Rule 11 reproducibility).

set -euo pipefail

CONTEXT="${SIGSTORE_CONTEXT:-k3d-loopzero-build}"
SCAFFOLD_VERSION="${SIGSTORE_SCAFFOLD_VERSION:-0.6.108}"
RELEASE_NAME="${SIGSTORE_RELEASE_NAME:-scaffold}"

# Resolve chart dir relative to this script.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
EXTRAS_CHART="$REPO_ROOT/infra/helm/sigstore-extras"

log() { printf '[bootstrap-sigstore-local] %s\n' "$*"; }
die() { printf '[bootstrap-sigstore-local] ERROR: %s\n' "$*" >&2; exit 1; }

kctl() { kubectl --context "$CONTEXT" "$@"; }

# -- Pre-flight --
log "Pre-flight: context=$CONTEXT, scaffold=$SCAFFOLD_VERSION, release=$RELEASE_NAME"
kctl cluster-info >/dev/null || die "Cannot reach context $CONTEXT"
command -v helm >/dev/null || die "helm CLI required"

# -- Step 1: Helm repo + install scaffold --
log "Step 1/6: Ensuring helm repo sigstore"
if ! helm repo list 2>/dev/null | grep -q '^sigstore\s'; then
  helm repo add sigstore https://sigstore.github.io/helm-charts >/dev/null
fi
helm repo update sigstore >/dev/null

log "Step 1/6: Installing scaffold (idempotent)"
if helm --kube-context "$CONTEXT" status "$RELEASE_NAME" --namespace default >/dev/null 2>&1; then
  log "  scaffold release '$RELEASE_NAME' already installed, skipping helm install"
else
  helm --kube-context "$CONTEXT" upgrade --install "$RELEASE_NAME" \
    sigstore/scaffold \
    --version "$SCAFFOLD_VERSION" \
    --namespace default \
    --create-namespace \
    --wait \
    --timeout 10m
fi

# -- Step 2: Wait for Fulcio Ready --
log "Step 2/6: Waiting for Fulcio pod Ready (up to 5min)"
kctl wait --for=condition=Ready pod \
  -l app.kubernetes.io/name=fulcio \
  -n fulcio-system \
  --timeout=5m || die "Fulcio pod did not become Ready"

# -- Step 3: Fetch K3s server CA --
log "Step 3/6: Fetching K3s server CA from kube-root-ca.crt"
CA_PEM="$(kctl get cm kube-root-ca.crt -n default -o jsonpath='{.data.ca\.crt}')"
[ -n "$CA_PEM" ] || die "K3s server CA not found in default/kube-root-ca.crt"
log "  CA fetched (${#CA_PEM} bytes)"

# -- Step 4: Patch Fulcio ConfigMap with OIDCIssuer.CACert --
log "Step 4/6: Patching Fulcio ConfigMap with K8s OIDC CACert"
TMP_CONFIG=$(mktemp)
trap 'rm -f "$TMP_CONFIG"' EXIT

# Build new config with CACert field (escapes embedded newlines for JSON).
CA_ESCAPED=$(printf '%s' "$CA_PEM" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')

python3 <<PY > "$TMP_CONFIG"
import json
config = {
    "OIDCIssuers": {
        "https://kubernetes.default.svc.cluster.local": {
            "IssuerURL": "https://kubernetes.default.svc.cluster.local",
            "ClientID": "sigstore",
            "Type": "kubernetes",
            "CACert": ${CA_ESCAPED}
        },
        "https://kubernetes.default.svc": {
            "IssuerURL": "https://kubernetes.default.svc",
            "ClientID": "sigstore",
            "Type": "kubernetes",
            "CACert": ${CA_ESCAPED}
        }
    },
    "MetaIssuers": {
        "https://kubernetes.*.svc": {
            "ClientID": "sigstore",
            "Type": "kubernetes"
        }
    }
}
print(json.dumps(config, indent=2))
PY

CONFIG_JSON=$(cat "$TMP_CONFIG")
kctl patch cm fulcio-server-config -n fulcio-system \
  --type=merge \
  -p "$(python3 -c "import json,sys; print(json.dumps({'data': {'config.json': sys.argv[1]}}))" "$CONFIG_JSON")"

log "  Restarting Fulcio to pick up new config"
kctl rollout restart deployment/fulcio-server -n fulcio-system
kctl rollout status deployment/fulcio-server -n fulcio-system --timeout=3m

# -- Step 5: Install sigstore-extras (NetworkPolicies) --
log "Step 5/6: Installing sigstore-extras (NetworkPolicy fix for kube-router)"
[ -d "$EXTRAS_CHART" ] || die "Chart not found: $EXTRAS_CHART"
helm --kube-context "$CONTEXT" upgrade --install sigstore-extras "$EXTRAS_CHART" \
  --namespace fulcio-system \
  --wait \
  --timeout 2m

# -- Step 6: K3s anonymous-auth + ClusterRoleBinding --
log "Step 6/6: Ensuring system:unauthenticated → system:public-info-viewer binding"
if ! kctl get clusterrolebinding sigstore-unauthenticated-oidc-discovery >/dev/null 2>&1; then
  kctl create clusterrolebinding sigstore-unauthenticated-oidc-discovery \
    --clusterrole=system:service-account-issuer-discovery \
    --group=system:unauthenticated
else
  log "  ClusterRoleBinding already exists"
fi

log "Bootstrap complete. Sigstore LOCAL stack ready for keyless signing."
log ""
log "Verify with:"
log "  kubectl --context $CONTEXT get pods -n fulcio-system"
log "  kubectl --context $CONTEXT get networkpolicy -A | grep sigstore"
