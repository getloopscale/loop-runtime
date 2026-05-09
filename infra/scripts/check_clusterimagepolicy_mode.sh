#!/usr/bin/env bash
#
# T8.6 of deep-review-remediation-plan (find_infra_008 LOW, Sprint 9 scope).
#
# Governance lint: ClusterImagePolicy.mode in admission-policies values must
# stay 'warn' until Sprint 9 hardening flips it to 'enforce' atomically with
# the cosign keyless cutover. This script blocks PRs that flip the mode
# without an accompanying ADR document.
#
# Why this exists: a one-line YAML change ("mode: enforce") in a PR could
# silently start REJECTING all unsigned platform images at admission. Per
# CLAUDE.md Princípio 1 (95%+ confidence) and Princípio 11 (legacy code is
# wrong code), the mode flip must be a deliberate Sprint 9 rollout with a
# rollback runbook.
#
# Exit 0: mode is 'warn' OR mode is 'enforce' AND an ADR exists referencing
#         find_infra_008 + Sprint 9 cutover.
# Exit 1: mode is 'enforce' without ADR — the flip is happening invisibly.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"
VALUES_FILE="$REPO_ROOT/infra/helm/admission-policies/values.yaml"
ADR_DIR="$REPO_ROOT/docs/meetings/adr"

if [ ! -f "$VALUES_FILE" ]; then
    echo "PASS — admission-policies/values.yaml not found (chart not present in this snapshot)."
    exit 0
fi

# Extract clusterImagePolicy.mode value via grep — keep dependency-free.
# Matches "  mode: warn" or "  mode: enforce" inside the clusterImagePolicy block.
mode=$(awk '
    /^clusterImagePolicy:/ { in_block=1; next }
    /^[a-zA-Z]/ && in_block { in_block=0 }
    in_block && /^[[:space:]]+mode:/ { print $2; exit }
' "$VALUES_FILE")

if [ -z "$mode" ]; then
    echo "PASS — clusterImagePolicy.mode field not present (chart upgrade in progress)."
    exit 0
fi

# Strip quotes if any.
mode=$(echo "$mode" | tr -d '"' | tr -d "'")

if [ "$mode" = "warn" ]; then
    echo "PASS — clusterImagePolicy.mode is 'warn' (Sprint 6 default; Sprint 9 cutover pending)."
    exit 0
fi

if [ "$mode" = "enforce" ]; then
    # Look for an ADR referencing the cutover.
    if [ -d "$ADR_DIR" ] && grep -lqr "T8.6\|find_infra_008\|clusterimagepolicy.*enforce\|deny-flip" "$ADR_DIR"/*.md 2>/dev/null; then
        echo "PASS — clusterImagePolicy.mode is 'enforce' with ADR sign-off."
        exit 0
    fi
    cat <<EOF >&2
FAIL — T8.6 governance violation
================================

clusterImagePolicy.mode is set to 'enforce' but no ADR references the
find_infra_008 cutover. The deny-flip is a production-impacting change
(unsigned images get REJECTED at admission) and MUST land with:

  1. ADR in docs/meetings/adr/ referencing T8.6 / find_infra_008 / Sprint 9.
  2. Rollback runbook documenting the revert procedure.
  3. Coordinated dual-signing window (warn + enforce side-by-side ≥ 7 days).

Either:
  - Revert mode to 'warn' until Sprint 9 readiness review approves cutover.
  - Add the ADR to docs/meetings/adr/ with the references above.

See: docs/plans/deep-review-remediation-plan.md (T8.6 + ADR-0049).
EOF
    exit 1
fi

echo "FAIL — clusterImagePolicy.mode is '$mode' — must be 'warn' or 'enforce'." >&2
exit 1
