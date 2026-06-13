#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# preflight-t1.1-no-legacy-envs.sh — ADR D5 enforcement gate.
#
# Refuses CRD upgrade (Phase 1 of plan 2026-06-09-m2-operator-canonical-fix)
# when legacy CRs with empty `project` exist on the target cluster, because:
#
#   - Pre-Phase-1: `TenantEnvironmentEntry.Project` was optional with empty
#     fallback to two-segment naming (the J-23 #69 backwards-compat path).
#   - Post-Phase-1 (after T1.1 deploy): `project` is required + RFC 1123 Pattern
#     at the CRD level. Existing CRs with empty `project` bypass admission
#     validation on writes but fail on Status update (heartbeat). The Tenant
#     controller then reconcile-loops forever.
#
# EC-1 absorption (resolves the existing-CR stuck-loop): T5.1 of the same
# plan (migrate-legacy-namespaces.sh) is the canonical tool to drain legacy
# envs BEFORE T1.1 deploys. This script is the hard gate that enforces the
# ADR D5 ordering — Phase 5 ships before Phase 1, period.
#
# Usage:
#   ./preflight-t1.1-no-legacy-envs.sh                  # check mgmt context
#   CONTEXT=dev-public ./preflight-t1.1-no-legacy-envs.sh
#   ./preflight-t1.1-no-legacy-envs.sh --json
#
# Exit codes:
#   0 = no legacy envs detected → safe to apply T1.1 CRD bump.
#   1 = at least one tenant CR has empty `project` on an Environments entry.
#       Operator MUST run migrate-legacy-namespaces.sh before retrying.
#   2 = kubectl unavailable OR cluster unreachable. Operator MUST fix env.

set -uo pipefail

CONTEXT="${CONTEXT:-k3d-loopzero-mgmt}"
JSON_OUTPUT=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context=*) CONTEXT="${1#--context=}"; shift ;;
    --context)   CONTEXT="${2:-}"; shift 2 ;;
    --json)      JSON_OUTPUT=1; shift ;;
    --help|-h)
      grep -E '^#( |$)' "$0" | head -50
      exit 0
      ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

if ! command -v kubectl >/dev/null 2>&1; then
  echo "ERROR: kubectl not in PATH" >&2
  exit 2
fi

if ! kubectl --context "$CONTEXT" cluster-info >/dev/null 2>&1; then
  echo "ERROR: cluster context '$CONTEXT' unreachable" >&2
  exit 2
fi

# Query Tenant CRs and extract any Environments entries whose Project is
# null OR empty. jq's `[]` over a missing array yields no results so this
# correctly handles tenants with zero environments (skipped).
LEGACY_JSON="$(
  kubectl --context "$CONTEXT" get tenants.loopzero.io -A -o json 2>/dev/null \
    | jq -c '
        [ .items[]
          | { tenant: .metadata.name, namespace: .metadata.namespace,
              legacy_envs: ( .spec.environments // []
                             | map(select(.project == null or .project == "")) ) }
          | select((.legacy_envs | length) > 0)
        ]'
)"

LEGACY_COUNT="$(echo "$LEGACY_JSON" | jq 'length')"

if [[ "$JSON_OUTPUT" == "1" ]]; then
  echo "$LEGACY_JSON"
else
  if [[ "$LEGACY_COUNT" == "0" ]]; then
    echo "PASS — no legacy Tenant CRs detected on context $CONTEXT."
    echo "Safe to apply T1.1 CRD bump (project required + Pattern)."
  else
    echo "FAIL — $LEGACY_COUNT tenant(s) have Environments entries with empty Project on context $CONTEXT:"
    echo "$LEGACY_JSON" | jq -r '.[] | "  - \(.namespace)/\(.tenant): \(.legacy_envs | map(.name) | join(", "))"'
    echo
    echo "ADR D5 ordering blocks Phase 1 CRD deploy until Phase 5 migration completes."
    echo "Run: bash infra/scripts/local/migrate-legacy-namespaces.sh --mode=delete --tenant <tn-id>"
    echo "     (or --mode=adopt for prod tenants with data to preserve)"
  fi
fi

if [[ "$LEGACY_COUNT" == "0" ]]; then
  exit 0
fi
exit 1
