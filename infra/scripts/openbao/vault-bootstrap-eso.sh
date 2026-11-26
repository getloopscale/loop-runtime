#!/usr/bin/env bash
# vault-bootstrap-eso.sh — Bootstrap ESO ClusterSecretStore against loopruntime-secrets.
#
# Runbook P1 steps 2-4 codified: apply SA + ClusterRoleBinding + extract Vault
# CA cert from openbao-tls Secret + create loopruntime-secrets-ca ConfigMap em
# external-secrets NS + apply ClusterSecretStore CR + wait for status=Ready.
#
# Failure modes prevented:
# - CA cert truncation (base64 -d round-trip without quoting → newline corruption)
# - Wrong namespace for ConfigMap (must be external-secrets where ESO operator
#   reads caProvider.configMap.name; D11 architecture)
# - ClusterSecretStore applied BEFORE ConfigMap → CR stuck NotReady "configmap
#   loopruntime-secrets-ca not found"
# - Idempotent ConfigMap create (dry-run=client | apply -f -)
#
# Usage:
#   bash infra/scripts/openbao/vault-bootstrap-eso.sh \
#     --mgmt-context k3d-loopruntime-core \
#     --vault-context k3d-loopruntime-secrets \
#     [--vault-secret-name openbao-tls] \
#     [--vault-secret-namespace secrets-system] \
#     [--eso-namespace external-secrets] \
#     [--manifest-dir infra/manifests/cluster-secret-stores] \
#     [--wait-timeout 60] \
#     [--dry-run]
#
# Exit codes:
#   0 — bootstrap complete + ClusterSecretStore Ready
#   1 — usage / preflight failure
#   2 — apply failure (kubectl returned non-zero)
#   3 — ClusterSecretStore did NOT reach Ready=True within timeout
#
# Plan: 2026-06-02-loopruntime-api-vault-integration v1.8 P1 codification (iter 13).
# Runbook reference: docs/runbooks.md P1.
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

# ----------------------------------------------------------------------------
# CLI args
# ----------------------------------------------------------------------------
CORE_CONTEXT="${LOOPRUNTIME_CORE_CONTEXT:-k3d-loopruntime-core}"
SECRETS_CONTEXT="${LOOPRUNTIME_SECRETS_CONTEXT:-k3d-loopruntime-secrets}"
VAULT_SECRET_NAME="openbao-tls"
VAULT_SECRET_NAMESPACE="secrets-system"
ESO_NAMESPACE="external-secrets"
MANIFEST_DIR="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}/infra/manifests/cluster-secret-stores"
WAIT_TIMEOUT=60
DRY_RUN=0

usage() {
  cat <<EOF
vault-bootstrap-eso.sh — Bootstrap ESO ClusterSecretStore (Runbook P1 codification).

USAGE
  $0 [options]

OPTIONS
  --mgmt-context <ctx>           kubectl context for mgmt cluster (default: k3d-loopruntime-core)
  --vault-context <ctx>          kubectl context for vault cluster (default: k3d-loopruntime-secrets)
  --vault-secret-name <name>     Vault TLS Secret name (default: openbao-tls)
  --vault-secret-namespace <ns>  Vault TLS Secret NS (default: secrets-system)
  --eso-namespace <ns>           ESO operator namespace (default: external-secrets)
  --manifest-dir <path>          Manifests dir (default: infra/manifests/cluster-secret-stores)
  --wait-timeout <sec>           ClusterSecretStore Ready timeout (default: 60)
  --dry-run                      Print planned actions without executing
  --help                         Show this help

WORKFLOW (per Runbook P1 steps 2-4)
  1. Apply eso-vault-reader SA + ClusterRoleBinding em mgmt
  2. Extract Vault CA cert from openbao-tls Secret em vault cluster
  3. Create/update ConfigMap loopruntime-secrets-ca em mgmt:external-secrets NS
  4. Apply ClusterSecretStore CR em mgmt
  5. Wait for ClusterSecretStore status.conditions[Ready]=True

EXAMPLES
  # Standalone-multi (local k3d):
  $0 --mgmt-context k3d-loopruntime-core --vault-context k3d-loopruntime-secrets

  # Production (DOKS):
  $0 --mgmt-context loopruntime-core-prod --vault-context loopruntime-secrets-prod

  # Dry-run preflight check:
  $0 --dry-run
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --mgmt-context) CORE_CONTEXT="$2"; shift 2 ;;
    --vault-context) SECRETS_CONTEXT="$2"; shift 2 ;;
    --vault-secret-name) VAULT_SECRET_NAME="$2"; shift 2 ;;
    --vault-secret-namespace) VAULT_SECRET_NAMESPACE="$2"; shift 2 ;;
    --eso-namespace) ESO_NAMESPACE="$2"; shift 2 ;;
    --manifest-dir) MANIFEST_DIR="$2"; shift 2 ;;
    --wait-timeout) WAIT_TIMEOUT="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------
if ! command -v kubectl >/dev/null 2>&1; then
  echo "ERROR: kubectl not in PATH" >&2; exit 1
fi

if ! [[ "$WAIT_TIMEOUT" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --wait-timeout must be a non-negative integer" >&2; exit 1
fi

SA_MANIFEST="$MANIFEST_DIR/eso-vault-reader-sa.yaml"
CSS_MANIFEST="$MANIFEST_DIR/loopruntime-secrets-secrets.yaml"

for f in "$SA_MANIFEST" "$CSS_MANIFEST"; do
  if [ ! -f "$f" ]; then
    echo "ERROR: manifest not found: $f" >&2; exit 1
  fi
done

if [ "${SKIP_PREFLIGHT:-0}" != "1" ] && [ "$DRY_RUN" -eq 0 ]; then
  for ctx in "$CORE_CONTEXT" "$SECRETS_CONTEXT"; do
    if ! kubectl --context "$ctx" version --request-timeout=5s >/dev/null 2>&1; then
      echo "ERROR: kubectl context '$ctx' not reachable" >&2
      exit 1
    fi
  done
fi

echo "[INFO] mgmt=$CORE_CONTEXT vault=$SECRETS_CONTEXT eso-ns=$ESO_NAMESPACE dry-run=$DRY_RUN" >&2
echo "" >&2

# ----------------------------------------------------------------------------
# Step 1: Apply SA + ClusterRoleBinding em mgmt
# ----------------------------------------------------------------------------
echo "[STEP 1/4] Apply eso-vault-reader SA + ClusterRoleBinding em mgmt" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] kubectl --context $CORE_CONTEXT apply -f $SA_MANIFEST" >&2
else
  if kubectl --context "$CORE_CONTEXT" apply -f "$SA_MANIFEST" >/dev/null 2>&1; then
    echo "  [OK] SA + ClusterRoleBinding applied" >&2
  else
    echo "  [FAIL] kubectl apply -f $SA_MANIFEST" >&2
    exit 2
  fi
fi

# ----------------------------------------------------------------------------
# Step 2: Extract Vault CA cert from openbao-tls Secret em vault cluster
# ----------------------------------------------------------------------------
echo "" >&2
echo "[STEP 2/4] Extract Vault CA cert from $VAULT_SECRET_NAMESPACE/$VAULT_SECRET_NAME" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] kubectl --context $SECRETS_CONTEXT -n $VAULT_SECRET_NAMESPACE get secret $VAULT_SECRET_NAME -o jsonpath='{.data.ca\.crt}' | base64 -d" >&2
  VAULT_CA="MOCK_CA_CERT_DRY_RUN"
else
  VAULT_CA=$(kubectl --context "$SECRETS_CONTEXT" -n "$VAULT_SECRET_NAMESPACE" \
    get secret "$VAULT_SECRET_NAME" -o jsonpath='{.data.ca\.crt}' 2>/dev/null | base64 -d 2>/dev/null)
  if [ -z "$VAULT_CA" ]; then
    echo "  [FAIL] CA cert empty — Secret $VAULT_SECRET_NAME may not exist" >&2
    echo "         Verify openbao-cert chart deployed em vault cluster:" >&2
    echo "         kubectl --context $SECRETS_CONTEXT -n $VAULT_SECRET_NAMESPACE get secret" >&2
    exit 2
  fi
  # Sanity check: should start with -----BEGIN CERTIFICATE-----
  if ! echo "$VAULT_CA" | head -1 | grep -q "BEGIN CERTIFICATE"; then
    echo "  [FAIL] extracted CA does not start with PEM header" >&2
    exit 2
  fi
  ca_lines=$(echo "$VAULT_CA" | wc -l)
  echo "  [OK] CA cert extracted ($ca_lines lines)" >&2
fi

# ----------------------------------------------------------------------------
# Step 3: Create/update ConfigMap loopruntime-secrets-ca em mgmt
# ----------------------------------------------------------------------------
echo "" >&2
echo "[STEP 3/4] Create/update ConfigMap loopruntime-secrets-ca em $CORE_CONTEXT:$ESO_NAMESPACE" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] kubectl --context $CORE_CONTEXT -n $ESO_NAMESPACE create configmap loopruntime-secrets-ca --from-literal=ca.crt=<CA> --dry-run=client -o yaml | kubectl apply -f -" >&2
else
  # Ensure namespace exists (idempotent)
  kubectl --context "$CORE_CONTEXT" create namespace "$ESO_NAMESPACE" \
    --dry-run=client -o yaml | kubectl --context "$CORE_CONTEXT" apply -f - >/dev/null 2>&1 || true

  # Apply ConfigMap via dry-run | apply -f - pattern (idempotent update)
  if echo "$VAULT_CA" | kubectl --context "$CORE_CONTEXT" -n "$ESO_NAMESPACE" \
      create configmap loopruntime-secrets-ca --from-file=ca.crt=/dev/stdin \
      --dry-run=client -o yaml \
      | kubectl --context "$CORE_CONTEXT" apply -f - >/dev/null 2>&1; then
    echo "  [OK] ConfigMap loopruntime-secrets-ca synced" >&2
  else
    echo "  [FAIL] ConfigMap apply failed" >&2
    exit 2
  fi
fi

# ----------------------------------------------------------------------------
# Step 4: Apply ClusterSecretStore CR em mgmt
# ----------------------------------------------------------------------------
echo "" >&2
echo "[STEP 4/4] Apply ClusterSecretStore loopruntime-secrets-secrets em mgmt" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] kubectl --context $CORE_CONTEXT apply -f $CSS_MANIFEST" >&2
  echo "  [DRY-RUN] kubectl wait clustersecretstore loopruntime-secrets-secrets --for=condition=Ready --timeout=${WAIT_TIMEOUT}s" >&2
else
  if kubectl --context "$CORE_CONTEXT" apply -f "$CSS_MANIFEST" >/dev/null 2>&1; then
    echo "  [OK] ClusterSecretStore applied" >&2
  else
    echo "  [FAIL] kubectl apply -f $CSS_MANIFEST" >&2
    exit 2
  fi

  # Wait for Ready=True (status condition reconcile)
  echo "  [INFO] waiting up to ${WAIT_TIMEOUT}s for status.conditions[Ready]=True" >&2
  if kubectl --context "$CORE_CONTEXT" wait clustersecretstore loopruntime-secrets-secrets \
      --for=condition=Ready --timeout="${WAIT_TIMEOUT}s" >/dev/null 2>&1; then
    echo "  [OK] ClusterSecretStore Ready=True" >&2
  else
    echo "  [FAIL] ClusterSecretStore did NOT reach Ready within ${WAIT_TIMEOUT}s" >&2
    echo "  [DIAG] kubectl --context $CORE_CONTEXT describe clustersecretstore loopruntime-secrets-secrets" >&2
    exit 3
  fi
fi

# ----------------------------------------------------------------------------
# Final report
# ----------------------------------------------------------------------------
echo "" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "[DRY-RUN COMPLETE] no changes made" >&2
else
  echo "[COMPLETE] ESO ClusterSecretStore bootstrapped + Ready" >&2
  echo "" >&2
  echo "Next step (P1 step 5): seed Vault with initial secrets" >&2
  echo "  bash infra/scripts/openbao/seed-loopruntime-secrets.sh \\" >&2
  echo "    --env <env> --secret-values-file <path>" >&2
fi
exit 0
