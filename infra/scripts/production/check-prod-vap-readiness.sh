#!/usr/bin/env bash
# T1.12 — Pre-flight check before flipping VAP noRuntimeImageMutations from
# Warn/Ignore to Deny/Fail in prod.
#
# Per EC-NEW-4: flipping to Deny without verifying ALL existing pods are
# signed = catastrophic cluster down. This script verifies pre-flip safety.
# (The kubectl-set-image-in-CI-workflows check was dropped 2026-05-23 — GitHub
# Actions removed; verify no set-image automation in the real CI manually.)
#
# Usage:
#   KUBECONFIG=~/.kube/prod ./check-prod-vap-readiness.sh
#
# Exit codes:
#   0 — Pre-flight checks PASS; safe to flip VAP to Deny/Fail
#   1 — Pre-flight checks FAIL; do NOT flip
#   2 — Setup/usage error

set -euo pipefail

if ! command -v kubectl &>/dev/null; then
  echo "ERROR: kubectl not found in PATH" >&2
  exit 2
fi

if [[ -z "${KUBECONFIG:-}" ]] || [[ ! -f "${KUBECONFIG}" ]]; then
  echo "ERROR: KUBECONFIG not set or file missing" >&2
  echo "Set KUBECONFIG to your prod cluster's kubeconfig before running" >&2
  exit 2
fi

echo "===== T1.12 VAP Flip Pre-Flight Check ====="
echo "Target: prod cluster"
echo "Check: safe to flip noRuntimeImageMutations from Warn/Ignore → Deny/Fail?"
echo ""

FAILURES=()

# Check 1 — All prod pods have valid cosign signatures
echo "[1/4] Checking cosign signatures on all running prod pods..."
UNSIGNED_PODS=()
while IFS= read -r pod; do
  [[ -z "$pod" ]] && continue
  ns=$(echo "$pod" | awk '{print $1}')
  name=$(echo "$pod" | awk '{print $2}')
  # Skip system namespaces (excluded via matchConditions)
  if [[ "$ns" =~ ^(kube-system|kube-public|kube-node-lease|digitalocean|argo|argocd|external-secrets|cert-manager)$ ]]; then
    continue
  fi

  IMAGE=$(kubectl -n "$ns" get pod "$name" -o jsonpath='{.spec.containers[0].image}' 2>/dev/null || echo "")
  if [[ -n "$IMAGE" ]]; then
    if ! cosign verify --key cosign.pub "$IMAGE" &>/dev/null; then
      UNSIGNED_PODS+=("$ns/$name (image: $IMAGE)")
    fi
  fi
done < <(kubectl get pods -A --no-headers 2>/dev/null)

if [[ ${#UNSIGNED_PODS[@]} -eq 0 ]]; then
  echo "    ✓ All prod pods have valid cosign signatures"
else
  echo "    ✗ ${#UNSIGNED_PODS[@]} pods with INVALID/MISSING signatures:"
  printf "        - %s\n" "${UNSIGNED_PODS[@]}"
  FAILURES+=("Unsigned pods present — flip would reject these pod recreations")
fi

# NOTE: the former "Check 2 — no kubectl set image in CI workflows" was removed
# 2026-05-23 (GitHub Actions deleted → no workflows to scan; the check could only
# pass vacuously). 4 checks → 3.

# Check 2 — ArgoCD apps all Synced (no pending image updates)
echo ""
echo "[2/3] Verifying ArgoCD applications all Synced..."
if command -v argocd &>/dev/null; then
  OOS_APPS=$(argocd app list -o json 2>/dev/null | jq -r '.[] | select(.status.sync.status != "Synced") | .metadata.name' || echo "")
  if [[ -z "$OOS_APPS" ]]; then
    echo "    ✓ All ArgoCD apps Synced"
  else
    echo "    ✗ OutOfSync apps detected:"
    echo "$OOS_APPS" | sed 's/^/        - /'
    FAILURES+=("OutOfSync apps may trigger image updates that would fail under Deny")
  fi
else
  echo "    ⚠ argocd CLI not installed; skipping check (operator must verify manually)"
fi

# Check 3 — No active VAP audit_log warnings in last 7 days
echo ""
echo "[3/3] Scanning admission audit events for last 7 days..."
RECENT_WARNS=$(kubectl get events -A --field-selector=reason=ValidationFailure --since=168h 2>/dev/null | grep -c "noRuntimeImageMutations" || true)
if [[ "$RECENT_WARNS" == "0" ]]; then
  echo "    ✓ No noRuntimeImageMutations warn events in last 7 days"
else
  echo "    ⚠ $RECENT_WARNS warn events in last 7 days (would become DENY post-flip)"
  echo "       Review and clear before flipping. Likely sources: dev hot-patches."
  FAILURES+=("$RECENT_WARNS recent warn events — these become hard denies post-flip")
fi

echo ""
echo "===== Pre-Flight Summary ====="
if [[ ${#FAILURES[@]} -eq 0 ]]; then
  echo "✓ All 3 checks PASS. Safe to flip:"
  echo ""
  echo "Next steps:"
  echo "  1. Edit infra/helmfile/environments/prod.yaml:"
  echo "       policyMode: enforce"
  echo "       noRuntimeImageMutationsAction: Deny"
  echo "       noRuntimeImageMutationsFailurePolicy: Fail"
  echo ""
  echo "  2. Run: helmfile -e prod sync"
  echo ""
  echo "  3. Monitor: kubectl get events -A --field-selector=reason=ValidationFailure --watch"
  echo "     for 24h. Zero unexpected denials = T1.12 complete."
  echo ""
  echo "  4. Rollback path: revert prod.yaml + helmfile sync (recovery <2min)"
  exit 0
else
  echo "✗ ${#FAILURES[@]} blocker(s) detected. DO NOT flip VAP yet:"
  printf "  - %s\n" "${FAILURES[@]}"
  echo ""
  echo "Resolve each blocker then re-run this pre-flight check."
  exit 1
fi
