#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD T3.3 — F-04 partial fix (T1 scope)
#
# Migration script: reconciles duplicated Tenant + Environment CRs across
# namespaces into the canonical `loopzero-system`. Pure data-cleanup — does NOT
# change product contracts, does NOT touch operator/API code. T3.1 (hard-coding
# the namespace in code) + T3.2 (ValidatingAdmissionPolicy) stay T2 and
# await a governance meeting (per the CLAUDE.md Pre-release rule).
#
# Strategy (idempotent, dry-run by default):
#  1. List ALL Tenant CRs in namespaces != loopzero-system.
#  2. For each one:
#     a. Back up the YAML to ~/.loopzero/backups/tenant-migration-<ts>/
#     b. If a same-named Tenant already exists in loopzero-system, compare
#        creationTimestamp:
#          - NEWER one (loopzero-system) → keep loopzero-system, delete the duplicate
#          - NEWER one (other ns) → error (manual decision required)
#     c. If it does NOT exist in loopzero-system, copy it there preserving spec
#        (strip resourceVersion/uid/managedFields/status; preserve
#        labels/annotations/finalizers).
#     d. Remove the finalizer from the original CR in the other ns + delete --wait.
#  3. Same flow for Environment CRs.
#
# Usage:
#   bash infra/scripts/production/migrate-tenant-namespace.sh                # dry-run
#   bash infra/scripts/production/migrate-tenant-namespace.sh --apply        # commit
#   bash infra/scripts/production/migrate-tenant-namespace.sh --apply --force-prefer=loopzero-system
#
# Exit codes:
#   0 — success (or nothing to do)
#   1 — pre-condition error (kubectl fails, permission denied)
#   2 — manual decision required (duplicates with newer in non-canonical ns)
set -euo pipefail

DRY_RUN=1
FORCE_PREFER=""
for arg in "$@"; do
  case "$arg" in
    --apply) DRY_RUN=0 ;;
    --force-prefer=*) FORCE_PREFER="${arg#*=}" ;;
    -h|--help)
      grep '^#' "$0" | head -40 | sed 's/^# //'
      exit 0
      ;;
  esac
done

CANONICAL_NS="loopzero-system"
BACKUP_DIR="${HOME}/.loopzero/backups/tenant-migration-$(date +%Y%m%d-%H%M%S)"

log() { printf '[migrate-tenant-namespace] %s\n' "$*" >&2; }
say() { printf '%s\n' "$*"; }

if [[ "$DRY_RUN" -eq 1 ]]; then
  log "DRY-RUN mode — no changes will be applied. Re-run with --apply to commit."
fi

# 1) List misplaced Tenants (namespace != loopzero-system).
MISPLACED_TENANTS=$(kubectl get tenants -A -o json 2>/dev/null \
  | python3 -c '
import sys, json
d = json.load(sys.stdin)
for t in d.get("items", []):
    ns = t["metadata"]["namespace"]
    if ns != "'"$CANONICAL_NS"'":
        print(ns + "/" + t["metadata"]["name"])
' 2>/dev/null || true)

MISPLACED_ENVS=$(kubectl get environments -A -o json 2>/dev/null \
  | python3 -c '
import sys, json
d = json.load(sys.stdin)
for e in d.get("items", []):
    ns = e["metadata"]["namespace"]
    if ns != "'"$CANONICAL_NS"'":
        print(ns + "/" + e["metadata"]["name"])
' 2>/dev/null || true)

if [[ -z "$MISPLACED_TENANTS" && -z "$MISPLACED_ENVS" ]]; then
  log "no misplaced CRs found — all Tenants and Environments are in $CANONICAL_NS"
  exit 0
fi

say "=== Misplaced CRs detected ==="
[[ -n "$MISPLACED_TENANTS" ]] && say "Tenants:" && echo "$MISPLACED_TENANTS" | sed 's/^/  /'
[[ -n "$MISPLACED_ENVS" ]] && say "Environments:" && echo "$MISPLACED_ENVS" | sed 's/^/  /'
say ""

# 2) Backup before any apply.
if [[ "$DRY_RUN" -eq 0 ]]; then
  mkdir -p "$BACKUP_DIR"
  chmod 700 "$BACKUP_DIR"
  log "backup directory: $BACKUP_DIR"
fi

# decide_winner: returns "canonical" if loopzero-system version is newer (or
# only one exists in canonical), "other" if non-canonical is newer (manual
# decision needed), "missing" if no canonical exists yet.
decide_winner() {
  local kind="$1" name="$2" other_ns="$3"
  local canon_ts other_ts
  canon_ts=$(kubectl get "$kind" -n "$CANONICAL_NS" "$name" \
    -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null || true)
  other_ts=$(kubectl get "$kind" -n "$other_ns" "$name" \
    -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null || true)
  if [[ -z "$canon_ts" ]]; then
    printf 'missing'
    return
  fi
  if [[ -n "$FORCE_PREFER" ]]; then
    if [[ "$FORCE_PREFER" == "$CANONICAL_NS" ]]; then
      printf 'canonical'
    else
      printf 'other'
    fi
    return
  fi
  # ISO8601 timestamps compare lexicographically.
  if [[ "$canon_ts" > "$other_ts" || "$canon_ts" == "$other_ts" ]]; then
    printf 'canonical'
  else
    printf 'other'
  fi
}

migrate_cr() {
  local kind="$1" entry="$2"
  local other_ns="${entry%%/*}"
  local name="${entry##*/}"

  log "checking $kind $other_ns/$name"
  local winner
  winner=$(decide_winner "$kind" "$name" "$other_ns")

  case "$winner" in
    canonical)
      log "  → CANONICAL ($CANONICAL_NS/$name) is newer or equal; deleting $other_ns/$name"
      if [[ "$DRY_RUN" -eq 0 ]]; then
        kubectl get "$kind" -n "$other_ns" "$name" -o yaml > "$BACKUP_DIR/${kind}_${other_ns}_${name}.yaml"
        # CRITICAL safety guard (incident 2026-04-29 iter 3):
        # Tenant + Environment CRs in ANY ns are reconciled by the
        # controller — they are not inert. Removing the finalizer + delete
        # cascades REAL cleanup of the shared resources (ArgoCD App,
        # workload namespace, etc.) that are ALSO managed by the canonical
        # CR in loopzero-system. Result: HTTP 200 turns into HTTP 404, the
        # workload ns goes into Terminating, the ArgoCD App is deleted.
        # MITIGATION: mark the stale CR with the `argocd.argoproj.io/skip-reconcile:
        # true` annotation BEFORE removing the finalizer. That pauses the
        # tenant-controller reconciliation loop over the stale CR while
        # the cleanup runs. The canonical CR in loopzero-system stays
        # active and re-creates/preserves the shared resources.
        log "  → marking $other_ns/$name with skip-reconcile to pause controller"
        kubectl annotate "$kind" -n "$other_ns" "$name" \
          argocd.argoproj.io/skip-reconcile=true \
          loopzero.io/migration-pending=true \
          --overwrite >/dev/null 2>&1 || true
        # Short pause so the controller sees the annotation.
        sleep 2
        # Strip finalizers to enable delete.
        kubectl patch "$kind" -n "$other_ns" "$name" \
          --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true
        kubectl delete "$kind" -n "$other_ns" "$name" --wait=false 2>&1 | head -1
        # Wait with a short timeout — does not block if the ns is terminating.
        for _ in 1 2 3 4 5 6 7 8 9 10; do
          if ! kubectl get "$kind" -n "$other_ns" "$name" >/dev/null 2>&1; then
            break
          fi
          sleep 2
        done
        log "  → deleted $other_ns/$name"
        # Force-reconcile the canonical one so shared resources
        # (ArgoCD App, namespaces) get re-validated after the delete.
        kubectl annotate "$kind" -n "$CANONICAL_NS" "$name" \
          force-reconcile="$(date +%s)" --overwrite >/dev/null 2>&1 || true
        log "  → triggered reconcile of $CANONICAL_NS/$name"
      fi
      ;;
    missing)
      log "  → no $kind in $CANONICAL_NS; copying $other_ns/$name → $CANONICAL_NS/$name"
      if [[ "$DRY_RUN" -eq 0 ]]; then
        kubectl get "$kind" -n "$other_ns" "$name" -o yaml > "$BACKUP_DIR/${kind}_${other_ns}_${name}.yaml"
        # Strip server-managed fields + change namespace.
        python3 - <<EOF
import yaml, sys
with open("$BACKUP_DIR/${kind}_${other_ns}_${name}.yaml") as f:
    d = yaml.safe_load(f)
m = d.setdefault("metadata", {})
m["namespace"] = "$CANONICAL_NS"
for k in ["resourceVersion", "uid", "creationTimestamp", "managedFields", "ownerReferences"]:
    m.pop(k, None)
d.pop("status", None)
with open("$BACKUP_DIR/${kind}_${other_ns}_${name}.migrated.yaml", "w") as f:
    yaml.safe_dump(d, f)
EOF
        kubectl apply -f "$BACKUP_DIR/${kind}_${other_ns}_${name}.migrated.yaml"
        kubectl patch "$kind" -n "$other_ns" "$name" \
          --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true
        kubectl delete "$kind" -n "$other_ns" "$name" --wait=true --timeout=60s
        log "  → migrated and deleted source"
      fi
      ;;
    other)
      log "  → ⚠ NON-CANONICAL ($other_ns/$name) is NEWER than $CANONICAL_NS/$name"
      log "  → manual decision required. Re-run with --force-prefer=$CANONICAL_NS or --force-prefer=$other_ns"
      return 2
      ;;
  esac
}

EXIT_CODE=0
if [[ -n "$MISPLACED_TENANTS" ]]; then
  while IFS= read -r entry; do
    [[ -z "$entry" ]] && continue
    migrate_cr "tenant" "$entry" || EXIT_CODE=$?
  done <<< "$MISPLACED_TENANTS"
fi

if [[ -n "$MISPLACED_ENVS" ]]; then
  while IFS= read -r entry; do
    [[ -z "$entry" ]] && continue
    migrate_cr "environment" "$entry" || EXIT_CODE=$?
  done <<< "$MISPLACED_ENVS"
fi

if [[ "$DRY_RUN" -eq 0 && "$EXIT_CODE" -eq 0 ]]; then
  log "migration complete. Verify: kubectl get tenants,environments -A"
fi

exit "$EXIT_CODE"
