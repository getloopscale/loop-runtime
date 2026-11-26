#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 5 follow-up
#
# Provisions the ESO ClusterSecretStore `loopruntime-core-store` on the runtime cluster
# (parity with Pulumi runtime-bootstrap.ts). Without it, any pod with a
# secret declared in loopzero.yaml stays in CreateContainerConfigError because
# ESO cannot sync Secrets from mgmt into the tenant ns.
#
# Resources created:
#   1. ServiceAccount `eso-secret-reader` in the mgmt cluster (ns loopruntime-system)
#   2. Role + RoleBinding scoped to [secrets get/list/watch] in loopruntime-system
#   3. SA Token Secret (long-lived) — the source of truth for the bearer
#   4. Secret `core-cluster-token` in the runtime cluster, ns external-secrets,
#      holding {token, ca} extracted from the SA Token (via base64 round-trip)
#   5. ClusterSecretStore `loopruntime-core-store` on runtime, pointing at the
#      mgmt API server with bearer + CA
#
# Idempotent — re-running does not change the state (the SA Token is stable).
#
# Usage:
#   bash infra/helmfile/scripts/bootstrap-eso-cluster-secret-store.sh \
#     [<mgmt-context>] [<runtime-context>]
#
#   defaults: k3d-loopruntime-core + k3d-loopruntime-cell-0
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

MGMT_CTX="${1:-k3d-loopruntime-core}"
RUNTIME_CTX="${2:-k3d-loopruntime-cell-0}"
PLATFORM_NS="loopruntime-system"
ESO_NS="external-secrets"
SA_NAME="eso-secret-reader"
TOKEN_SECRET="${SA_NAME}-token"
CSS_NAME="loopruntime-core-store"

log() { printf '[bootstrap-eso-css] %s\n' "$*" >&2; }
fail() { printf '[bootstrap-eso-css] FAIL: %s\n' "$*" >&2; exit 1; }

# ----------------------------------------------------------------------------
# Step 1 — ServiceAccount + RBAC (mgmt cluster, scoped to loopruntime-system secrets)
# ----------------------------------------------------------------------------
log "creating ServiceAccount + Role + RoleBinding on $MGMT_CTX (ns=$PLATFORM_NS)"

kubectl --context "$MGMT_CTX" -n "$PLATFORM_NS" apply -f - <<EOF >/dev/null
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $SA_NAME
  namespace: $PLATFORM_NS
  labels:
    app.kubernetes.io/managed-by: loopruntime-bootstrap
    app.kubernetes.io/component: eso-source
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: $SA_NAME
  namespace: $PLATFORM_NS
  labels:
    app.kubernetes.io/managed-by: loopruntime-bootstrap
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: $SA_NAME
  namespace: $PLATFORM_NS
  labels:
    app.kubernetes.io/managed-by: loopruntime-bootstrap
subjects:
  - kind: ServiceAccount
    name: $SA_NAME
    namespace: $PLATFORM_NS
roleRef:
  kind: Role
  name: $SA_NAME
  apiGroup: rbac.authorization.k8s.io
EOF

# ----------------------------------------------------------------------------
# Step 2 — SA Token Secret (long-lived, k8s manages contents)
# ----------------------------------------------------------------------------
log "creating SA token Secret on $MGMT_CTX (ns=$PLATFORM_NS, name=$TOKEN_SECRET)"

kubectl --context "$MGMT_CTX" -n "$PLATFORM_NS" apply -f - <<EOF >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: $TOKEN_SECRET
  namespace: $PLATFORM_NS
  annotations:
    kubernetes.io/service-account.name: $SA_NAME
type: kubernetes.io/service-account-token
EOF

# Wait briefly for kube-controller-manager to populate token + ca.crt.
for _ in $(seq 1 30); do
  TOKEN=$(kubectl --context "$MGMT_CTX" -n "$PLATFORM_NS" get secret "$TOKEN_SECRET" -o jsonpath='{.data.token}' 2>/dev/null || true)
  if [[ -n "$TOKEN" ]]; then break; fi
  sleep 1
done
[[ -n "$TOKEN" ]] || fail "SA token never populated by k8s"

CA=$(kubectl --context "$MGMT_CTX" -n "$PLATFORM_NS" get secret "$TOKEN_SECRET" -o jsonpath='{.data.ca\.crt}' 2>/dev/null || true)
[[ -n "$CA" ]] || fail "SA ca.crt never populated"

# Decode token (base64-from-secret); CA stays base64 for caBundle field.
DECODED_TOKEN=$(printf '%s' "$TOKEN" | base64 -d)
log "SA token + CA captured ($(printf '%s' "$DECODED_TOKEN" | wc -c) chars in token)"

# ----------------------------------------------------------------------------
# Step 3 — Resolve mgmt API URL reachable FROM runtime cluster
# ----------------------------------------------------------------------------
# In k3d, all clusters share Docker network → use mgmt server container IP.
# In DOKS production, Pulumi uses cluster.endpoint (LB DNS) — same shape.
log "resolving mgmt cluster API URL"

# k3d clusters: $(docker inspect k3d-<name>-server-0 -f '{{.NetworkSettings.Networks.k3d.IPAddress}}'):6443
MGMT_NAME="${MGMT_CTX#k3d-}"
MGMT_IP=$(docker inspect "k3d-${MGMT_NAME}-server-0" -f '{{range .NetworkSettings.Networks}}{{if .IPAddress}}{{.IPAddress}}{{end}}{{end}}' 2>/dev/null | head -1)
if [[ -z "$MGMT_IP" ]]; then
  fail "could not resolve mgmt cluster IP via docker inspect"
fi
MGMT_URL="https://${MGMT_IP}:6443"
log "mgmt URL = $MGMT_URL"

# ----------------------------------------------------------------------------
# Step 4 — Secret core-cluster-token in runtime cluster (ns external-secrets)
# ----------------------------------------------------------------------------
log "ensuring namespace $ESO_NS on $RUNTIME_CTX"
kubectl --context "$RUNTIME_CTX" create namespace "$ESO_NS" --dry-run=client -o yaml | \
  kubectl --context "$RUNTIME_CTX" apply -f - >/dev/null

log "creating core-cluster-token Secret on $RUNTIME_CTX (ns=$ESO_NS)"
kubectl --context "$RUNTIME_CTX" -n "$ESO_NS" apply -f - <<EOF >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: core-cluster-token
  namespace: $ESO_NS
  labels:
    app.kubernetes.io/managed-by: loopruntime-bootstrap
    app.kubernetes.io/component: eso-source
stringData:
  token: $(printf '%s' "$DECODED_TOKEN")
  ca: $(printf '%s' "$CA")
EOF

# ----------------------------------------------------------------------------
# Step 5 — ClusterSecretStore on runtime cluster
# ----------------------------------------------------------------------------
log "creating ClusterSecretStore $CSS_NAME on $RUNTIME_CTX"
kubectl --context "$RUNTIME_CTX" apply -f - <<EOF >/dev/null
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: $CSS_NAME
  labels:
    app.kubernetes.io/managed-by: loopruntime-bootstrap
spec:
  provider:
    kubernetes:
      remoteNamespace: $PLATFORM_NS
      server:
        url: $MGMT_URL
        caBundle: $CA
      auth:
        token:
          bearerToken:
            name: core-cluster-token
            namespace: $ESO_NS
            key: token
EOF

# ----------------------------------------------------------------------------
# Verify Ready condition
# ----------------------------------------------------------------------------
log "waiting for ClusterSecretStore Ready=True (max 60s)"
for _ in $(seq 1 30); do
  STATUS=$(kubectl --context "$RUNTIME_CTX" get clustersecretstore "$CSS_NAME" \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
  if [[ "$STATUS" == "True" ]]; then
    log "ClusterSecretStore Ready=True"
    exit 0
  fi
  sleep 2
done

REASON=$(kubectl --context "$RUNTIME_CTX" get clustersecretstore "$CSS_NAME" \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || echo "no status yet")
fail "ClusterSecretStore not Ready: $REASON"
