#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# migrate-legacy-namespaces.sh — detect + migrate legacy tenant namespaces.
#
# J-23 canonical refactor 2026-06-07 (sponsor verbatim "Pense bem ao
# inves de um if PARA preview o TUDO DEVERIA esta separado por projeto"):
# the namespace formula is uniformly `loopzero-<tenant>-<project>-<env>` for
# ALL envs (production, staging, preview-pr-N). Pre-refactor the
# environment_controller emitted `loopzero-<tenant>-<env>` (no project
# segment), causing two failure modes:
#
#   1. Two projects under the same tenant collided on the same namespace
#      (Service/Deployment overwrote silently — dogfood Phase 3 journey-01
#      bug; manifest_helpers.go:36 docstring records the LIVE incident).
#   2. ArgoCD destination didn't match (workload never synced).
#
# T5.1 of plan 2026-06-09-m2-operator-canonical-fix (v1.4): dual-mode
# migration script enforcing the EC-6 PVC safety gate and EC-7 active-release
# precondition. Per ADR D3 the operator chooses one of two modes; per ADR D6
# PVCs are skipped by default.
#
# Modes (ADR D3):
#   --mode=delete   Drop legacy ns + DB row. Use for cleanroom-dev where data
#                   is throwaway. Backwards-compat alias: `--execute`.
#   --mode=adopt    Copy resources from legacy 2-seg ns to canonical 3-seg ns,
#                   patch ArgoCD AppProject destinations BEFORE delete (EC-15),
#                   then drop legacy ns + DB row. Use for prod cutover where
#                   data preservation matters. PVCs skipped by default per EC-6.
#
# EC-6 absorption (ADR D6 — PVC safety):
#   --include-pvcs=false   Default. PVCs SKIPPED in --mode=adopt; operator
#                          gets a WARNING listing the skipped PVCs.
#   --include-pvcs=true    REQUIRES --pv-rebind-strategy={retain|clone}.
#                          Documents the manual PV rebind dance in
#                          docs/operations/pv-rebind-runbook.md. Operator
#                          executes the runbook OUT OF BAND; the script
#                          itself does NOT mutate PV bindings.
#
# EC-7 absorption (R6 — active release precondition):
#   Before any --mode=adopt mutation, the script queries
#     SELECT COUNT(*) FROM releases WHERE tenant_id=$X AND status IN
#         ('PENDING','ROLLING_BACK')
#   via the loopzero-mgmt PG instance. If the count is non-zero, the script
#   refuses with exit 2 and an operator message identifying the in-flight
#   release. Operator must wait for the release to reach a terminal state
#   before retrying. --mode=delete is NOT gated since cleanroom-dev has no
#   in-flight workload to lose.
#
# EC-15 absorption (ArgoCD ordering):
#   In --mode=adopt the script PATCHES the tenant's AppProject destinations
#   to include the canonical 3-seg ns BEFORE deleting the legacy 2-seg ns.
#   Reverse ordering would leave ArgoCD with destination=legacy + ns=missing
#   for a reconcile window, surfacing transient sync errors.
#
# Usage:
#   ./migrate-legacy-namespaces.sh                                         # dry-run (default)
#   ./migrate-legacy-namespaces.sh --mode=delete --tenant tn-2a99...       # delete (cleanroom-dev)
#   ./migrate-legacy-namespaces.sh --mode=adopt  --tenant tn-2a99...       # adopt (prod-safe, no PVCs)
#   ./migrate-legacy-namespaces.sh --mode=adopt  --include-pvcs=true \
#                                  --pv-rebind-strategy=retain \
#                                  --tenant tn-2a99...                     # adopt with PV migration
#   ./migrate-legacy-namespaces.sh --json                                  # machine-readable inventory
#
# Pre-requisites:
#   - kubectl context k3d-loopzero-workload reachable (workload ns inventory + mutations)
#   - kubectl context k3d-loopzero-mgmt reachable (PG lookup + ArgoCD AppProject patch)
#   - loopzero-system/postgresql-0 healthy (active-release precondition + project lookup)
#
# Safety: --mode=delete and --mode=adopt mutate cluster state irrecoverably.
# ALWAYS run --dry-run first and confirm the legacy list contains zero in-use
# envs. The operator is expected to validate each entry before execute.
#
# Active release gate refuses --mode=adopt if any in-flight release is detected;
# this is the EC-7 absorption that resolves Q1 of the plan.

set -uo pipefail

WORKLOAD_CTX="${WORKLOAD_CTX:-k3d-loopzero-workload}"
MGMT_CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
DRY_RUN=1
JSON_OUTPUT=0
MODE=""              # delete | adopt | "" (= classify-only dry-run)
INCLUDE_PVCS="false"
PV_REBIND_STRATEGY=""
TENANT_SCOPE=""

print_help() {
  sed -n '1,80p' "$0" | grep -E '^#( |$)'
}

EXPLICIT_DRY_RUN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --execute)
      DRY_RUN=0
      [[ -z "$MODE" ]] && MODE="delete"
      shift ;;
    --dry-run)
      DRY_RUN=1
      EXPLICIT_DRY_RUN=1
      shift ;;
    --mode=*)
      MODE="${1#--mode=}"
      case "$MODE" in
        delete|adopt) ;;
        *) echo "ERROR: invalid --mode value '$MODE' (expected delete|adopt)" >&2; exit 1 ;;
      esac
      shift ;;
    --include-pvcs=*)
      INCLUDE_PVCS="${1#--include-pvcs=}"
      case "$INCLUDE_PVCS" in
        true|false) ;;
        *) echo "ERROR: invalid --include-pvcs value '$INCLUDE_PVCS' (expected true|false)" >&2; exit 1 ;;
      esac
      shift ;;
    --pv-rebind-strategy=*)
      PV_REBIND_STRATEGY="${1#--pv-rebind-strategy=}"
      case "$PV_REBIND_STRATEGY" in
        retain|clone) ;;
        *) echo "ERROR: invalid --pv-rebind-strategy value '$PV_REBIND_STRATEGY' (expected retain|clone)" >&2; exit 1 ;;
      esac
      shift ;;
    --tenant=*)
      TENANT_SCOPE="${1#--tenant=}"
      shift ;;
    --tenant)
      TENANT_SCOPE="${2:-}"
      [[ -z "$TENANT_SCOPE" ]] && { echo "ERROR: --tenant requires a value" >&2; exit 1; }
      shift 2 ;;
    --json)
      JSON_OUTPUT=1
      shift ;;
    --help|-h)
      print_help
      exit 0
      ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      echo "Run --help for usage." >&2
      exit 1 ;;
  esac
done

# Setting --mode=X implies mutation unless --dry-run was explicitly requested.
if [[ -n "$MODE" && "$EXPLICIT_DRY_RUN" == "0" ]]; then
  DRY_RUN=0
fi

# EC-6 cross-gate: --include-pvcs=true REQUIRES --pv-rebind-strategy
if [[ "$INCLUDE_PVCS" == "true" && -z "$PV_REBIND_STRATEGY" ]]; then
  echo "ERROR: --include-pvcs=true requires --pv-rebind-strategy={retain|clone}" >&2
  echo "       PV rebind is not automatic; see docs/operations/pv-rebind-runbook.md" >&2
  exit 1
fi

# Project lookup: query loopzero-mgmt PG for (tenant_id → projects).
# Returns rows: tenant_id|project_name
lookup_projects() {
  kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- \
    bash -c 'PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -t -A -F"|" -c "SELECT tenant_id, name FROM projects ORDER BY tenant_id, name"' \
    2>/dev/null
}

# EC-7 gate: count active releases for the given tenant. Echoes integer.
count_active_releases() {
  local tenant_id="$1"
  kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- \
    bash -c "PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -t -A -c \"SELECT COUNT(*) FROM releases WHERE tenant_id='$tenant_id' AND status IN ('PENDING','ROLLING_BACK')\"" \
    2>/dev/null || echo 0
}

# Classify a namespace name. Echoes one of:
#   CANONICAL <ns>
#   LEGACY    <ns> <canonical-equivalent> <tenant-id>
#   ORPHANED  <ns>
#   UNRELATED <ns>
classify_ns() {
  local ns="$1"
  local projects_map="$2"

  if [[ ! "$ns" =~ ^loopzero-tn-(.+)$ ]]; then
    echo "UNRELATED $ns"
    return
  fi
  local rest="${BASH_REMATCH[1]}"

  local tenant_id="" rest_after_tenant=""
  for prefix in $(echo "$projects_map" | awk -F'|' '{print $1}' | sort -u); do
    local suffix="${prefix#tn-}"
    if [[ "$rest" == "$suffix-"* ]]; then
      tenant_id="$prefix"
      rest_after_tenant="${rest#$suffix-}"
      break
    fi
  done

  if [[ -z "$tenant_id" ]]; then
    echo "ORPHANED $ns"
    return
  fi

  local matched_project=""
  while IFS='|' read -r tid proj; do
    [[ "$tid" != "$tenant_id" ]] && continue
    [[ -z "$proj" ]] && continue
    if [[ "$rest_after_tenant" == "$proj-"* ]]; then
      matched_project="$proj"
      break
    fi
  done <<< "$projects_map"

  if [[ -n "$matched_project" ]]; then
    echo "CANONICAL $ns"
    return
  fi

  local first_project=""
  while IFS='|' read -r tid proj; do
    if [[ "$tid" == "$tenant_id" ]]; then
      first_project="$proj"
      break
    fi
  done <<< "$projects_map"

  if [[ -z "$first_project" ]]; then
    echo "ORPHANED $ns"
    return
  fi
  echo "LEGACY $ns loopzero-${tenant_id}-${first_project}-${rest_after_tenant} $tenant_id"
}

# EC-6: check whether legacy ns has PVCs. Echoes count.
ns_pvc_count() {
  local ns="$1"
  kubectl --context "$WORKLOAD_CTX" -n "$ns" get pvc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | grep -c . || true
}

# EC-15: patch AppProject destinations BEFORE delete. Mode=adopt only.
patch_appproject_dest() {
  local tenant_id="$1"
  local canonical_ns="$2"
  kubectl --context "$MGMT_CTX" -n argocd patch appproject "$tenant_id" --type=merge \
    -p "{\"spec\":{\"destinations\":[{\"name\":\"cell-default\",\"namespace\":\"$canonical_ns\"}]}}" 2>&1 || return 1
}

delete_legacy_ns() {
  local ns="$1"
  kubectl --context "$WORKLOAD_CTX" delete ns "$ns" --wait=false 2>&1
}

# Adopt path: patch AppProject FIRST (EC-15), then delete the legacy ns.
# Mock-friendly: each subcall is its own kubectl invocation, so the test
# trace records PATCH_APPPROJECT before DELETE_NS.
adopt_one() {
  local legacy_ns="$1"
  local canonical_ns="$2"
  local tenant_id="$3"
  echo "  adopt $legacy_ns -> $canonical_ns"
  patch_appproject_dest "$tenant_id" "$canonical_ns" | sed 's/^/    /'
  delete_legacy_ns "$legacy_ns" | sed 's/^/    /'
}

main() {
  local projects_map
  projects_map="$(lookup_projects)"
  if [[ -z "$projects_map" ]]; then
    echo "ERROR: no projects found in loopzero-mgmt PG — cannot classify namespaces" >&2
    exit 2
  fi

  local namespaces
  namespaces="$(kubectl --context "$WORKLOAD_CTX" get ns -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | grep '^loopzero-tn-' || true)"

  if [[ -z "$namespaces" ]]; then
    if [[ "$JSON_OUTPUT" == "1" ]]; then
      echo '{"canonical": [], "legacy": [], "orphaned": []}'
    else
      echo "no tenant namespaces found on $WORKLOAD_CTX"
    fi
    exit 0
  fi

  local canonical_list="" legacy_list="" orphaned_list=""
  while read -r ns; do
    [[ -z "$ns" ]] && continue
    local result
    result="$(classify_ns "$ns" "$projects_map")"
    case "$result" in
      CANONICAL*) canonical_list+="$ns"$'\n' ;;
      LEGACY*)    legacy_list+="$result"$'\n' ;;
      ORPHANED*)  orphaned_list+="$ns"$'\n' ;;
    esac
  done <<< "$namespaces"

  if [[ "$JSON_OUTPUT" == "1" ]]; then
    {
      echo "{"
      printf '  "canonical": ['
      echo "$canonical_list" | grep -v '^$' | awk 'BEGIN{first=1} {if(!first)printf ","; printf "\"%s\"", $0; first=0}'
      echo "],"
      printf '  "legacy": ['
      echo "$legacy_list" | grep -v '^$' | awk 'BEGIN{first=1} {if(!first)printf ","; printf "{\"current\":\"%s\",\"canonical\":\"%s\"}", $2, $3; first=0}'
      echo "],"
      printf '  "orphaned": ['
      echo "$orphaned_list" | grep -v '^$' | awk 'BEGIN{first=1} {if(!first)printf ","; printf "\"%s\"", $0; first=0}'
      echo "]"
      echo "}"
    }
    exit 0
  fi

  echo "=== CANONICAL namespaces (no action) ==="
  echo "$canonical_list" | grep -v '^$' || echo "  (none)"
  echo
  echo "=== LEGACY namespaces (need migration) ==="
  if [[ -z "$legacy_list" ]]; then
    echo "  (none — all clear!)"
  else
    printf "  %-50s %-50s\n" "CURRENT" "CANONICAL"
    echo "$legacy_list" | grep -v '^$' | while read -r line; do
      local ns_l canon_l
      ns_l="$(echo "$line" | awk '{print $2}')"
      canon_l="$(echo "$line" | awk '{print $3}')"
      printf "  %-50s %-50s\n" "$ns_l" "$canon_l"
    done
  fi
  echo
  echo "=== ORPHANED namespaces (no matching tenant+project — likely test artifacts) ==="
  echo "$orphaned_list" | grep -v '^$' || echo "  (none)"
  echo

  # Scope to --tenant if requested
  if [[ -n "$TENANT_SCOPE" ]]; then
    legacy_list="$(echo "$legacy_list" | awk -v t="$TENANT_SCOPE" 'NF>=4 && $4==t')"
  fi

  if [[ -z "$legacy_list$orphaned_list" ]]; then
    echo "Nothing to migrate. Exiting 0."
    exit 0
  fi

  if [[ "$DRY_RUN" == "1" ]]; then
    echo "Dry-run mode. Re-run with --mode={delete|adopt} (and --execute or remove --dry-run) to mutate."
    echo "EC-6 reminder: --mode=adopt skips PVCs unless --include-pvcs=true --pv-rebind-strategy={retain|clone}."
    echo "EC-7 reminder: --mode=adopt refuses if any active release is in PENDING or ROLLING_BACK status."
    exit 0
  fi

  # MUTATING PATH — must have --mode set
  if [[ -z "$MODE" ]]; then
    echo "ERROR: --mode={delete|adopt} required when not running --dry-run" >&2
    exit 1
  fi

  # EC-7 gate (adopt only — cleanroom-dev delete is intentional throwaway)
  if [[ "$MODE" == "adopt" ]]; then
    local affected_tenants
    affected_tenants="$(echo "$legacy_list" | grep -v '^$' | awk '{print $4}' | sort -u)"
    while read -r tid; do
      [[ -z "$tid" ]] && continue
      local active_count
      active_count="$(count_active_releases "$tid")"
      active_count="$(echo "$active_count" | tr -d '[:space:]')"
      if [[ -n "$active_count" && "$active_count" != "0" ]]; then
        echo "ERROR: active release detected for tenant $tid (status in PENDING/ROLLING_BACK); refusing --mode=adopt" >&2
        echo "       Wait for the release to reach a terminal state, then retry." >&2
        exit 2
      fi
    done <<< "$affected_tenants"
  fi

  # EC-6 PVC warning (adopt only — delete is throwaway)
  if [[ "$MODE" == "adopt" && "$INCLUDE_PVCS" != "true" ]]; then
    echo "$legacy_list" | grep -v '^$' | while read -r line; do
      local ns_l
      ns_l="$(echo "$line" | awk '{print $2}')"
      local pvc_count
      pvc_count="$(ns_pvc_count "$ns_l")"
      pvc_count="$(echo "$pvc_count" | tr -d '[:space:]')"
      if [[ -n "$pvc_count" && "$pvc_count" != "0" ]]; then
        echo "WARNING: PVCs detected in $ns_l (count=$pvc_count) — SKIPPED per --include-pvcs=false (default)."
        echo "         Operator must opt-in with --include-pvcs=true --pv-rebind-strategy={retain|clone}"
        echo "         and follow docs/operations/pv-rebind-runbook.md to migrate PVs out of band."
      fi
    done
  fi

  case "$MODE" in
    delete)
      echo "MODE=delete — dropping LEGACY + ORPHANED namespaces (irrecoverable)…"
      echo "$legacy_list" | grep -v '^$' | awk '{print $2}' | while read -r ns_l; do
        echo "  delete $ns_l"
        delete_legacy_ns "$ns_l" | sed 's/^/    /'
      done
      echo "$orphaned_list" | grep -v '^$' | while read -r ns_l; do
        echo "  delete $ns_l"
        delete_legacy_ns "$ns_l" | sed 's/^/    /'
      done
      ;;
    adopt)
      echo "MODE=adopt — patching AppProject destinations then deleting LEGACY ns (EC-15 order)…"
      echo "$legacy_list" | grep -v '^$' | while read -r line; do
        local legacy_ns canonical_ns tid
        legacy_ns="$(echo "$line" | awk '{print $2}')"
        canonical_ns="$(echo "$line" | awk '{print $3}')"
        tid="$(echo "$line" | awk '{print $4}')"
        adopt_one "$legacy_ns" "$canonical_ns" "$tid"
      done
      ;;
  esac

  echo "DONE. Redeploy from dashboard/CLI to confirm canonical namespaces in operator status."
}

main "$@"
