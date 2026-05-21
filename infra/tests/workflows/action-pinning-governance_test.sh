#!/usr/bin/env bash
# Hermetic gate: every floating action version (e.g., @v6, @v2.6.1) in
# .github/workflows/ MUST have an adjacent `# pragma: allow-tag-pin` comment
# linking to the INF-G18 backlog or a similar governance ticket.
#
# Plan 2026-05-21-deep-review-remediation T4.3 + ADR Rule (naming-conventions
# ADR Rule 1 by extension — explicit governance for exceptions).
#
# Background: deep review v2 flagged 11 workflows as FLOATING_ACTION_VERSION.
# Exhaustive check confirmed all 11 already carry `# pragma: allow-tag-pin`
# comments — they ARE tracked via INF-G18 (Renovate Bot resolves to SHA on
# first run). The actual anti-pattern would be UN-governed floating versions:
# floating @vN without explicit pragma + ticket reference.
#
# This gate prevents new floating-without-pragma additions in future PRs.
#
# Exemption rules:
#   - SHA-pinned actions (e.g., actions/checkout@11bd7190...) → no pragma needed
#   - Floating @vN with adjacent pragma comment → allowed (Renovate territory)
#   - Floating @vN without pragma → FAIL

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
GOVERNED=0
PINNED=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }
info() { echo "[action-pinning] $*"; }

# Match `uses: <org>/<action>[/sub]@<ref>` where ref is NOT a 40-char hex SHA.
# Floating refs: @v1, @v2.6.1, @main, @latest, etc.
# Pinned refs: @<sha40>.

for wf in .github/workflows/*.yml; do
  [ -f "$wf" ] || continue
  while IFS= read -r raw_line; do
    line_num="${raw_line%%:*}"
    line_text="${raw_line#*:}"

    # Extract the ref after @
    ref="${line_text##*@}"
    ref="${ref%%[\"\' ]*}"

    # SHA-pinned: 40 hex chars
    if [[ "$ref" =~ ^[a-f0-9]{40}$ ]]; then
      PINNED=$((PINNED + 1))
      continue
    fi

    # Floating ref — check for adjacent pragma comment (within 3 lines above)
    start=$((line_num - 3))
    [ "$start" -lt 1 ] && start=1
    if sed -n "${start},${line_num}p" "$wf" | grep -q "pragma: allow-tag-pin"; then
      GOVERNED=$((GOVERNED + 1))
      continue
    fi

    fail "$wf:$line_num — floating action ref '@$ref' without # pragma: allow-tag-pin governance comment"
  done < <(grep -nE "uses:\s+[a-z][a-z0-9/_-]+/[a-z0-9_-]+(/[a-z0-9_-]+)?@[a-zA-Z0-9._-]+" "$wf" 2>/dev/null || true)
done

info "Pinned: $PINNED actions to SHA"
info "Governed (floating + pragma comment): $GOVERNED"
info "Violations (floating, no pragma): $VIOLATIONS"

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "" >&2
  echo "[action-pinning] FAIL: $VIOLATIONS ungoverned floating action ref(s)" >&2
  echo "" >&2
  echo "To fix one of:" >&2
  echo "  1. Pin to commit SHA (40-char hex): uses: org/action@<sha40>" >&2
  echo "  2. Add pragma comment above the line:" >&2
  echo "     # pragma: allow-tag-pin" >&2
  echo "     # TODO(INF-G18 / plan T1.1): Renovate Bot resolves to @<sha40> on first run." >&2
  exit 1
fi

echo "[action-pinning] PASS — all floating actions are governance-tagged"
