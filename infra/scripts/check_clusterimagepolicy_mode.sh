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
# Canonical ADR location post-2026-05-11 docs cleanup: knowledge-base.
ADR_DIR="$REPO_ROOT/.claude/knowledge-base/decisions"
MEETING_DIR="$REPO_ROOT/.claude/knowledge-base/meetings"
# Legacy fallback during transition (will be removed once all gates migrate).
LEGACY_ADR_DIR="$REPO_ROOT/docs/meetings/adr"

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
    # Look for an ADR OR async-amendment record referencing the cutover.
    # Patterns: legacy T8.6/find_infra_008 (ADR-0050), MEET-2026-05-12 D2
    # mitigation #4, or any MEET-ASYNC-AMENDMENT referencing T1.2 #4.
    if [ -d "$ADR_DIR" ] && grep -lqr "T8.6\|find_infra_008\|clusterimagepolicy.*enforce\|deny-flip\|deny-mode\|T1.2.*#4\|mitigation #4" "$ADR_DIR"/*.md 2>/dev/null; then
        echo "PASS — clusterImagePolicy.mode is 'enforce' with ADR sign-off (knowledge-base/decisions)."
        exit 0
    fi
    if [ -d "$MEETING_DIR" ] && grep -lqr "deny-mode\|policyMode.*enforce\|T1.2.*#4\|mitigation #4" "$MEETING_DIR"/*.md 2>/dev/null; then
        echo "PASS — clusterImagePolicy.mode is 'enforce' with MEET sign-off (knowledge-base/meetings)."
        exit 0
    fi
    if [ -d "$LEGACY_ADR_DIR" ] && grep -lqr "T8.6\|find_infra_008" "$LEGACY_ADR_DIR"/*.md 2>/dev/null; then
        echo "PASS — clusterImagePolicy.mode is 'enforce' with legacy ADR sign-off (docs/meetings/adr/)."
        echo "       Recommend migrating to .claude/knowledge-base/decisions/ per 2026-05-11 doc cleanup."
        exit 0
    fi
    cat <<EOF >&2
FAIL — clusterImagePolicy.mode governance violation
====================================================

clusterImagePolicy.mode is set to 'enforce' but no ADR or MEET record
references the cutover. The deny-flip is a production-impacting change
(unsigned images get REJECTED at admission) and MUST land with:

  1. ADR in .claude/knowledge-base/decisions/ referencing:
       - T8.6 / find_infra_008 (legacy keyword), OR
       - "deny-mode" / "policyMode enforce", OR
       - "T1.2 #4" / "mitigation #4" (MEET-2026-05-12 D2)
  2. OR a MEET-ASYNC-AMENDMENT record in
     .claude/knowledge-base/meetings/ with the same keywords.
  3. Rollback runbook documenting the revert procedure.
  4. Coordinated dual-signing window (warn + enforce side-by-side ≥ 7 days)
     OR explicit MEET sponsor authorization waiving the window.

Either:
  - Revert mode to 'warn' until governance sign-off lands.
  - Add the ADR / MEET record with the references above.

See: MEET-2026-05-12 D2 mitigation #4 and
     concepts/operations/admission-deny-mode-cutover.md (runbook).
EOF
    exit 1
fi

echo "FAIL — clusterImagePolicy.mode is '$mode' — must be 'warn' or 'enforce'." >&2
exit 1
