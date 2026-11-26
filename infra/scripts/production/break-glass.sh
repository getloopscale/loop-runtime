#!/usr/bin/env bash
# MEET-055 M3: Break-Glass Procedure
#
# Creates a time-boxed cluster-admin ServiceAccount for emergency operations.
# Designed to be the ONLY way to get cluster-admin access in production.
#
# Features:
# - Time-boxed (default 4h, configurable)
# - Audit trail (logged to stdout + K8s events)
# - Auto-expiry via K8s Job that deletes the SA after TTL
# - Prometheus metric emitted via annotation (LoopRuntimeBreakGlassAccess alert fires)
#
# Usage:
#   ./infra/scripts/production/break-glass.sh [context] [ttl_hours] [reason]
#   ./infra/scripts/production/break-glass.sh do-nyc3-loopruntime-infra-homolog-nyc3-mgmt 4 "hotfix: PG connection timeout"
#
# The script outputs a kubeconfig that expires after TTL.
# All actions taken with this kubeconfig should be reconciled into Pulumi within 24h.

set -euo pipefail

CONTEXT=${1:?Usage: break-glass.sh <kubectl-context> [ttl_hours] [reason]}
TTL_HOURS=${2:-4}
REASON=${3:-"no reason provided"}
NAMESPACE="loopruntime-system"
SA_NAME="break-glass-$(date +%s)"
TTL_SECONDS=$((TTL_HOURS * 3600))
OPERATOR=$(whoami)

echo "========================================="
echo "  LOOPRUNTIME BREAK-GLASS ACCESS"
echo "========================================="
echo "  Context:  $CONTEXT"
echo "  TTL:      ${TTL_HOURS}h (${TTL_SECONDS}s)"
echo "  Operator: $OPERATOR"
echo "  Reason:   $REASON"
echo "  Time:     $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "========================================="
echo ""
echo "⚠️  This grants cluster-admin access for ${TTL_HOURS}h."
echo "⚠️  All changes MUST be reconciled into Pulumi within 24h."
echo ""
read -p "Proceed? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Aborted."
  exit 1
fi

# Create ServiceAccount
kubectl --context="$CONTEXT" -n "$NAMESPACE" create serviceaccount "$SA_NAME" \
  --dry-run=client -o yaml | \
  kubectl --context="$CONTEXT" apply -f - 2>&1

# Create ClusterRoleBinding (cluster-admin)
kubectl --context="$CONTEXT" create clusterrolebinding "$SA_NAME" \
  --clusterrole=cluster-admin \
  --serviceaccount="$NAMESPACE:$SA_NAME" \
  --dry-run=client -o yaml | \
  kubectl --context="$CONTEXT" apply -f - 2>&1

# Annotate with metadata for audit
kubectl --context="$CONTEXT" -n "$NAMESPACE" annotate serviceaccount "$SA_NAME" \
  "runtime.getloopscale.com/break-glass-operator=$OPERATOR" \
  "runtime.getloopscale.com/break-glass-reason=$REASON" \
  "runtime.getloopscale.com/break-glass-ttl=${TTL_HOURS}h" \
  "runtime.getloopscale.com/break-glass-expires=$(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v+${TTL_HOURS}H +%Y-%m-%dT%H:%M:%SZ)" \
  --overwrite 2>&1

# Create cleanup Job (auto-deletes SA + CRB after TTL)
cat <<EOF | kubectl --context="$CONTEXT" apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: break-glass-cleanup-${SA_NAME}
  namespace: $NAMESPACE
  labels:
    runtime.getloopscale.com/component: break-glass-cleanup
spec:
  ttlSecondsAfterFinished: 300
  template:
    spec:
      serviceAccountName: $SA_NAME
      restartPolicy: Never
      containers:
      - name: cleanup
        image: bitnami/kubectl:latest
        command:
        - /bin/sh
        - -c
        - |
          echo "Break-glass cleanup: waiting ${TTL_SECONDS}s before revoking access..."
          sleep $TTL_SECONDS
          echo "TTL expired. Revoking break-glass access for $SA_NAME..."
          kubectl -n $NAMESPACE delete serviceaccount $SA_NAME --ignore-not-found
          kubectl delete clusterrolebinding $SA_NAME --ignore-not-found
          echo "Break-glass access revoked."
EOF

# Generate token for the SA
TOKEN=$(kubectl --context="$CONTEXT" -n "$NAMESPACE" create token "$SA_NAME" --duration="${TTL_SECONDS}s" 2>/dev/null || \
  kubectl --context="$CONTEXT" -n "$NAMESPACE" get secret -o jsonpath='{.items[?(@.metadata.annotations.kubernetes\.io/service-account\.name=="'"$SA_NAME"'")].data.token}' | base64 -d)

# Get cluster info
SERVER=$(kubectl --context="$CONTEXT" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
CA_DATA=$(kubectl --context="$CONTEXT" config view --minify --flatten -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')

# Generate kubeconfig
KUBECONFIG_FILE="/tmp/break-glass-${SA_NAME}.kubeconfig"
cat > "$KUBECONFIG_FILE" <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: $CA_DATA
    server: $SERVER
  name: break-glass
contexts:
- context:
    cluster: break-glass
    user: break-glass
    namespace: $NAMESPACE
  name: break-glass
current-context: break-glass
users:
- name: break-glass
  user:
    token: $TOKEN
EOF

echo ""
echo "========================================="
echo "  BREAK-GLASS ACCESS GRANTED"
echo "========================================="
echo "  Kubeconfig: $KUBECONFIG_FILE"
echo "  Expires:    $(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v+${TTL_HOURS}H +%Y-%m-%dT%H:%M:%SZ)"
echo "  SA:         $SA_NAME"
echo ""
echo "  Usage:"
echo "    export KUBECONFIG=$KUBECONFIG_FILE"
echo "    kubectl get pods -n loopruntime-system"
echo ""
echo "  ⚠️  Reconcile ALL changes into Pulumi within 24h"
echo "  ⚠️  Auto-cleanup Job will revoke access in ${TTL_HOURS}h"
echo "========================================="
