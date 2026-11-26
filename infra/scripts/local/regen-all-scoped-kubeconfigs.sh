#!/usr/bin/env bash
# regen-all-scoped-kubeconfigs.sh
#
# CORRECTIONS.txt item N enforcement (plan T2.1):
# Regenerate the scoped SA token + kubeconfig for every ClusterCredential
# in the mgmt cluster (EXCEPT cell-0, which is bootstrap-managed by
# bootstrap-cell-0-cc.sh per EC-1 / H13 healing pattern).
#
# Use case: operator changed a node pool, manually rotated SA tokens, or
# is recovering from CORRECTIONS.txt item N symptom ("scoped kubeconfigs
# invalidated by node-pool churn").
#
# Concurrency (EC-3): flock prevents two operators from racing on the
# Secret patch. Optimistic-concurrency via kubectl patch --resource-version
# rejects mid-swap modifications.
#
# Exit codes:
#   0  — all cells regenerated successfully (OR --dry-run completed)
#   1  — usage error / flock contention / pg unreachable / no clusters
#   2  — at least one cell failed (per-cell isolation; remaining cells
#         attempted; non-zero summary at end)
#
# Usage:
#   bash infra/scripts/regen-all-scoped-kubeconfigs.sh
#   DRY_RUN=1 bash infra/scripts/regen-all-scoped-kubeconfigs.sh
#   bash infra/scripts/regen-all-scoped-kubeconfigs.sh --as-of <test-fixture>

set -euo pipefail

set -uo pipefail

CORE_CONTEXT="${CORE_CONTEXT:-k3d-loopruntime-core}"
LOCK_FILE="/tmp/loopruntime-regen-scoped-kubeconfig.lock"
LOCK_FD=200
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LIB_SCOPED_KUBECONFIG="$SCRIPT_DIR/../../helmfile/scripts/lib/scoped-kubeconfig.sh"

# Test hook: --fixture <dir> overrides cell enumeration (used by hermetic tests
# to feed a synthetic ClusterCredential list without touching a real cluster).
FIXTURE=""
DRY_RUN_FLAG="${DRY_RUN:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fixture) FIXTURE="$2"; shift 2 ;;
    --fixture=*) FIXTURE="${1#*=}"; shift ;;
    --dry-run) DRY_RUN_FLAG=1; shift ;;
    -h|--help) sed -n '1,40p' "$0"; exit 0 ;;
    *) echo "usage: $0 [--dry-run] [--fixture <dir>]" >&2; exit 1 ;;
  esac
done

log() { printf '[regen-scoped] %s\n' "$*" >&2; }

# ---------------------------------------------------------------------------
# EC-3: concurrent-regen protection via flock
# ---------------------------------------------------------------------------
exec {LOCK_FD}>"$LOCK_FILE"
if ! flock -n -x $LOCK_FD; then
  log "ERROR: another regen is in progress (lock held: $LOCK_FILE). Retry after current operator finishes."
  exit 1
fi
# Cleanup discipline: accumulate ALL mktemp temps in TMP_FILES[] and clean
# via EXIT trap. Previous design used `trap RETURN` inside the per-cell while
# loop, but RETURN only fires for function returns — NOT loop iterations or
# script exit. Result: every invocation leaked 2 temps per cell. Fix landed
# 2026-05-14 iter-16 (corrections-sprint Ralph loop).
TMP_FILES=()
# Invoked via `trap cleanup_temps EXIT` just below. BOTH codes because the
# diagnostic changed number between shellcheck versions: SC2317 in 0.9.x (the
# runner's) and SC2329 in 0.10+.
# shellcheck disable=SC2317,SC2329
cleanup_temps() {
  rm -f "$LOCK_FILE" 2>/dev/null || true
  if [[ ${#TMP_FILES[@]} -gt 0 ]]; then
    rm -f "${TMP_FILES[@]}" 2>/dev/null || true
  fi
}
trap cleanup_temps EXIT

log "lock acquired ($LOCK_FILE)"

# ---------------------------------------------------------------------------
# Enumerate ClusterCredentials — EXCLUDING cell-0 (EC-1)
# ---------------------------------------------------------------------------
enumerate_cells() {
  if [[ -n "$FIXTURE" ]]; then
    # Test fixture: JSON file with same shape as `kubectl get cc -o json`.
    cat "$FIXTURE"
    return
  fi
  kubectl --context "$CORE_CONTEXT" get clustercredentials -A -o json 2>/dev/null
}

CC_JSON=$(enumerate_cells)
if [[ -z "$CC_JSON" ]]; then
  log "ERROR: could not enumerate ClusterCredentials (mgmt cluster unreachable or no CRD)."
  exit 1
fi

# Filter out cell-0 per EC-1 (bootstrap-cell-0-cc.sh owns it).
CELLS=$(echo "$CC_JSON" | python3 -c '
import json, sys
data = json.load(sys.stdin)
for item in data.get("items", []):
    spec = item.get("spec", {})
    cell_id = spec.get("cellID", "")
    if cell_id == "cell-0":
        continue
    cluster_name = spec.get("clusterName", "")
    api_server = spec.get("apiServer", "")
    secret_name = spec.get("secretRef", {}).get("name", "")
    secret_ns = spec.get("secretRef", {}).get("namespace", "loopruntime-system")
    cr_name = item.get("metadata", {}).get("name", "")
    if not cluster_name or not secret_name:
        continue
    print(f"{cr_name}\t{cell_id}\t{cluster_name}\t{api_server}\t{secret_ns}\t{secret_name}")
')

if [[ -z "$CELLS" ]]; then
  log "no eligible cells found (cell-0 skipped per EC-1; no other active CRs)."
  exit 0
fi

n_cells=$(echo "$CELLS" | wc -l)
log "found $n_cells eligible cell(s) for regen (cell-0 excluded per EC-1)"

# ---------------------------------------------------------------------------
# Per-cell regen — isolated failure per cell
# ---------------------------------------------------------------------------
FAILED=0
SUCCEEDED=0

while IFS=$'\t' read -r cr_name cell_id cluster_name _api_server secret_ns secret_name; do
  log "→ regen: cellID=$cell_id cr=$cr_name cluster=$cluster_name secret=$secret_ns/$secret_name"

  if [[ "$DRY_RUN_FLAG" == "1" ]]; then
    log "  DRY-RUN: would invoke scoped-kubeconfig.sh + patch Secret"
    SUCCEEDED=$((SUCCEEDED+1))
    continue
  fi

  if [[ ! -x "$LIB_SCOPED_KUBECONFIG" ]]; then
    log "  ERROR: $LIB_SCOPED_KUBECONFIG not executable"
    FAILED=$((FAILED+1))
    continue
  fi

  # Mint a fresh scoped kubeconfig for the target cluster. The lib script
  # accepts the target's raw kubeconfig as input; in standalone-multi the
  # raw kubeconfig comes from `k3d kubeconfig get <cluster_name>`. In
  # production it comes from the operator's main DOKS kubeconfig.
  TMP_KC=$(mktemp -t "scoped-${cell_id}.XXXXXX.yaml")
  TMP_FILES+=("$TMP_KC")

  # Resolve raw kubeconfig source (test or live).
  # CR spec.clusterName is the operator-friendly label (e.g., "workload",
  # "runtime-2"). The actual k3d cluster name has a "loopruntime-" prefix
  # (e.g., "loopruntime-cell-0", "loopruntime-cell-1"). For `k3d kubeconfig get`,
  # we need the k3d cluster name (loopruntime-X). For scoped-kubeconfig.sh, we
  # pass the BARE cluster_name (X) — the lib INTERNALLY prepends `k3d-loopruntime-`
  # to find the docker container, per its B81/B82 logic.
  if [[ -n "${RAW_KUBECONFIG_OVERRIDE:-}" ]]; then
    cp "$RAW_KUBECONFIG_OVERRIDE" "$TMP_KC"
  elif command -v k3d >/dev/null 2>&1 && k3d cluster list 2>/dev/null | grep -q "^${cluster_name} "; then
    k3d kubeconfig get "${cluster_name}" > "$TMP_KC" 2>/dev/null || true
  elif command -v k3d >/dev/null 2>&1 && k3d cluster list 2>/dev/null | grep -q "^loopruntime-${cluster_name} "; then
    k3d kubeconfig get "loopruntime-${cluster_name}" > "$TMP_KC" 2>/dev/null || true
  else
    log "  WARN: no raw kubeconfig source for $cluster_name (tried bare + loopruntime-prefix; skipping)"
    FAILED=$((FAILED+1))
    continue
  fi

  SCOPED_OUTPUT=$(mktemp -t "scoped-out-${cell_id}.XXXXXX.yaml")
  TMP_FILES+=("$SCOPED_OUTPUT")

  # IMPORTANT (LIVE bug 2026-05-13): pass cluster_name (bare, e.g., "workload"),
  # NOT cell_id ("cell-0") and NOT "loopruntime-cell-0". The lib internally
  # prepends `k3d-loopruntime-` per its B81/B82 logic — passing "loopruntime-cell-0"
  # would produce double-prefix `k3d-loopruntime-loopruntime-cell-0-server-0` (does not exist).
  if ! bash "$LIB_SCOPED_KUBECONFIG" "$cluster_name" "$TMP_KC" "$SCOPED_OUTPUT" 2>&1 | sed 's/^/    [lib] /'; then
    log "  ERROR: scoped-kubeconfig.sh failed for $cell_id (cluster=$cluster_name)"
    FAILED=$((FAILED+1))
    continue
  fi

  # EC-3: optimistic-concurrency patch with --resource-version
  RV=$(kubectl --context "$CORE_CONTEXT" -n "$secret_ns" get secret "$secret_name" -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null || echo "")
  KC_B64=$(base64 -w0 < "$SCOPED_OUTPUT")

  if [[ -z "$RV" ]]; then
    # EC-6: Secret doesn't exist — create it.
    log "  Secret absent; creating ${secret_ns}/${secret_name}"
    if ! kubectl --context "$CORE_CONTEXT" -n "$secret_ns" create secret generic "$secret_name" \
        --from-file=kubeconfig="$SCOPED_OUTPUT" 2>/dev/null; then
      log "  ERROR: create Secret ${secret_ns}/${secret_name} failed"
      FAILED=$((FAILED+1))
      continue
    fi
  else
    # Patch existing Secret with optimistic-concurrency lock.
    PATCH_BODY=$(printf '{"metadata":{"resourceVersion":"%s"},"data":{"kubeconfig":"%s"}}' "$RV" "$KC_B64")
    if ! kubectl --context "$CORE_CONTEXT" -n "$secret_ns" patch secret "$secret_name" \
        --type=merge -p "$PATCH_BODY" 2>&1 | sed 's/^/    [patch] /'; then
      log "  ERROR: patch failed for ${secret_ns}/${secret_name} (possible optimistic-concurrency conflict — Secret modified mid-swap; operator should re-run)"
      FAILED=$((FAILED+1))
      continue
    fi
  fi

  log "  OK: $cell_id regenerated"
  SUCCEEDED=$((SUCCEEDED+1))
done <<< "$CELLS"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
log "summary: $SUCCEEDED succeeded, $FAILED failed (of $n_cells)"
if [[ "$FAILED" -gt 0 ]]; then
  exit 2
fi
exit 0
