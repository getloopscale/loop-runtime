#!/usr/bin/env bash
# vault-sync-check.sh — Operator validation for Phase 9 T9.1 acceptance criteria.
#
# D11 ESO supersedes original Provider-chain metric
# `loopruntime_config_secret_source{source="vault"}` from plan v1.0-v1.6 (which depended
# on pkg/secrets Provider chain that was never created per D11). The ESO-native
# equivalent for "is loopruntime-api consuming from Vault SoT?" is the per-CR status
# condition `Ready=True` + `Reason=SecretSynced` on each ExternalSecret.
#
# This script enumerates ExternalSecret CRs from the canonical inventory
# (`infra/helm/loopruntime-external-secrets/values.yaml` via yq) and asserts each is
# Ready=True. Provides Phase 9 LIVE evidence that >= N keys are syncing from
# Vault.
#
# Usage:
#   bash infra/scripts/openbao/vault-sync-check.sh \
#     --context k3d-loopruntime-core \
#     [--namespace loopruntime-system] \
#     [--threshold 10] \
#     [--inventory <path>] \
#     [--json]
#
# Exit codes:
#   0 — all ExternalSecrets Ready=True (or --threshold met)
#   1 — usage error / preflight failure
#   2 — sync threshold NOT met (operator decision: investigate)
#
# Pre-flight:
#   - kubectl + yq in PATH
#   - kubectl context reachable
#   - external-secrets operator installed in cluster
#
# Plan: 2026-06-02-loopruntime-api-vault-integration v1.8 Phase 9 T9.1.
# Replaces (per D11): loopruntime_config_secret_source metric concept.
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

# ----------------------------------------------------------------------------
# CLI args
# ----------------------------------------------------------------------------
CONTEXT="${KUBECTL_CONTEXT:-k3d-loopruntime-core}"
NAMESPACE="loopruntime-system"
THRESHOLD=10
INVENTORY="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}/infra/helm/loopruntime-external-secrets/values.yaml"
JSON_OUTPUT=0

usage() {
  cat <<EOF
vault-sync-check.sh — Phase 9 T9.1 LIVE evidence: ExternalSecret CR sync status.

USAGE
  $0 [options]

OPTIONS
  --context <ctx>      kubectl context (default: k3d-loopruntime-core; env KUBECTL_CONTEXT)
  --namespace <ns>     ExternalSecret CR namespace (default: loopruntime-system)
  --threshold <N>      Min synced count to PASS (default: 10 — per Phase 9 acceptance "5+/10 keys")
  --inventory <path>   Override inventory path (default: loopruntime-external-secrets/values.yaml)
  --json               Emit JSON summary to stdout (instead of human-readable)
  --help               Show this help

EXAMPLES
  # Standalone-multi (local k3d) sync check, threshold 10/13:
  $0 --context k3d-loopruntime-core --threshold 10

  # Prod-like smoke (T9.3): assert all 13 Ready=True (strict prod gate):
  $0 --context loopruntime-core-prod --threshold 13

  # JSON for CI pipeline ingestion:
  $0 --threshold 5 --json | jq '.synced'

REPLACES (per D11 ESO supersedes original Provider-chain design):
  Original plan v1.0-v1.6 acceptance: loopruntime_config_secret_source{source="vault"}>0
  ESO equivalent: per-CR status.conditions[Ready]=True (counted here).
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --context) CONTEXT="$2"; shift 2 ;;
    --namespace) NAMESPACE="$2"; shift 2 ;;
    --threshold) THRESHOLD="$2"; shift 2 ;;
    --inventory) INVENTORY="$2"; shift 2 ;;
    --json) JSON_OUTPUT=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------
for tool in kubectl yq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool not in PATH (required)" >&2
    exit 1
  fi
done

if [ ! -f "$INVENTORY" ]; then
  echo "ERROR: inventory not found: $INVENTORY" >&2
  exit 1
fi

if ! [[ "$THRESHOLD" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --threshold must be a non-negative integer, got: $THRESHOLD" >&2
  exit 1
fi

# Sanity-check kubectl reachability
if [ "${SKIP_PREFLIGHT:-0}" != "1" ]; then
  if ! kubectl --context "$CONTEXT" version --request-timeout=5s >/dev/null 2>&1; then
    echo "ERROR: kubectl context '$CONTEXT' not reachable" >&2
    exit 1
  fi
fi

# ----------------------------------------------------------------------------
# Enumerate expected ExternalSecret target names from inventory
# (NOT the K8s Secret targetSecret — the ExternalSecret CR name == targetSecret
#  per chart template convention in templates/externalsecrets.yaml)
# ----------------------------------------------------------------------------
EXPECTED_NAMES=$(yq '.externalSecrets[].targetSecret' "$INVENTORY" 2>/dev/null | grep -v '^$' | sort -u)
EXPECTED_COUNT=$(printf '%s\n' "$EXPECTED_NAMES" | grep -c -v '^$' || true)

if [ "$EXPECTED_COUNT" -lt 1 ]; then
  echo "ERROR: inventory parse produced 0 ExternalSecret targets" >&2
  exit 1
fi

# ----------------------------------------------------------------------------
# Query cluster state per ExternalSecret CR
# ----------------------------------------------------------------------------
SYNCED=0
NOT_READY=0
MISSING=0
declare -a STATUS_LINES=()
declare -a NOT_READY_NAMES=()
declare -a MISSING_NAMES=()

while IFS= read -r name; do
  [ -z "$name" ] && continue

  # Existence check first — if CR not found, treat as MISSING (don't query status).
  # The `|| echo` trick zeroes $? so we use explicit if-rc instead.
  if ! kubectl --context "$CONTEXT" -n "$NAMESPACE" get externalsecret "$name" >/dev/null 2>&1; then
    STATUS_LINES+=("MISSING: $name")
    MISSING_NAMES+=("$name")
    MISSING=$((MISSING + 1))
    continue
  fi

  status=$(kubectl --context "$CONTEXT" -n "$NAMESPACE" get externalsecret "$name" \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)

  reason=$(kubectl --context "$CONTEXT" -n "$NAMESPACE" get externalsecret "$name" \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}' 2>/dev/null)

  if [ "$status" = "True" ]; then
    STATUS_LINES+=("READY:   $name (reason=$reason)")
    SYNCED=$((SYNCED + 1))
  else
    STATUS_LINES+=("PENDING: $name (status=$status reason=$reason)")
    NOT_READY_NAMES+=("$name")
    NOT_READY=$((NOT_READY + 1))
  fi
done <<< "$EXPECTED_NAMES"

# ----------------------------------------------------------------------------
# Report
# ----------------------------------------------------------------------------
if [ "$JSON_OUTPUT" -eq 1 ]; then
  # Build JSON arrays manually (no jq dependency on the host beyond what yq pulls)
  not_ready_json=$(printf '"%s",' "${NOT_READY_NAMES[@]}" 2>/dev/null | sed 's/,$//')
  missing_json=$(printf '"%s",' "${MISSING_NAMES[@]}" 2>/dev/null | sed 's/,$//')
  cat <<EOF
{
  "context": "$CONTEXT",
  "namespace": "$NAMESPACE",
  "expected": $EXPECTED_COUNT,
  "synced": $SYNCED,
  "not_ready": $NOT_READY,
  "missing": $MISSING,
  "threshold": $THRESHOLD,
  "pass": $( [ "$SYNCED" -ge "$THRESHOLD" ] && echo true || echo false ),
  "not_ready_names": [$not_ready_json],
  "missing_names": [$missing_json]
}
EOF
else
  for line in "${STATUS_LINES[@]}"; do
    echo "  $line"
  done
  echo ""
  echo "═══════════════════════════════════════════════════════════"
  printf "  context=%s namespace=%s\n" "$CONTEXT" "$NAMESPACE"
  printf "  Expected: %d  Synced: %d  Pending: %d  Missing: %d\n" \
    "$EXPECTED_COUNT" "$SYNCED" "$NOT_READY" "$MISSING"
  printf "  Threshold (Phase 9 T9.1 acceptance): %d\n" "$THRESHOLD"
  if [ "$SYNCED" -ge "$THRESHOLD" ]; then
    printf "  Verdict: PASS (synced=%d >= threshold=%d)\n" "$SYNCED" "$THRESHOLD"
  else
    printf "  Verdict: FAIL (synced=%d < threshold=%d)\n" "$SYNCED" "$THRESHOLD"
  fi
  echo "═══════════════════════════════════════════════════════════"
fi

# Exit code per acceptance criteria
if [ "$SYNCED" -ge "$THRESHOLD" ]; then
  exit 0
else
  exit 2
fi
