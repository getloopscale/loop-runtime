#!/usr/bin/env bash
# T1.14 — Decisions Deletion Guard
# Fail CI if any .claude/knowledge-base/decisions/*.md OR meetings/*.md is
# deleted in a PR without a sibling file containing `superseded_by:` reference
# pointing to the deleted decision/meeting.
#
# Lesson source: MEET-2026-05-12 D5 — commit fc1793dd silently deleted ADR-0020
# and ADR-0021 with an unrelated commit message ("Delete meeting minutes for
# iterations 005-008"), violating Plan Amendment Policy. T1.14 prevents
# recurrence by enforcing the policy mechanically.
#
# Per .claude/rules/pre-release-governance.md § Plan Amendment Policy:
#   Deletion of formally ratified ADRs/MEETs requires either:
#   (a) A superseding ADR with `superseded_by: <deleted-file-name>` frontmatter
#   (b) Explicit MEET ratification of the deletion
#
# Usage:
#   ./check-decisions-deletion.sh BASE_REF HEAD_REF
#   ./check-decisions-deletion.sh origin/main HEAD
#
# Exit codes:
#   0 — No violations OR all deletions properly superseded
#   1 — Violation: deletion without superseder
#   2 — Usage error

set -euo pipefail

BASE_REF="${1:-origin/main}"
HEAD_REF="${2:-HEAD}"

if [[ -z "${BASE_REF}" || -z "${HEAD_REF}" ]]; then
  echo "Usage: $0 BASE_REF HEAD_REF" >&2
  echo "Example: $0 origin/main HEAD" >&2
  exit 2
fi

PROTECTED_DIRS=(
  ".claude/knowledge-base/decisions"
  ".claude/knowledge-base/meetings"
)

# Find all .md files deleted in protected dirs between BASE and HEAD
DELETED_FILES=()
for dir in "${PROTECTED_DIRS[@]}"; do
  while IFS= read -r line; do
    [[ -n "$line" ]] && DELETED_FILES+=("$line")
  done < <(git diff --name-only --diff-filter=D "${BASE_REF}...${HEAD_REF}" -- "${dir}/*.md" 2>/dev/null || true)
done

if [[ ${#DELETED_FILES[@]} -eq 0 ]]; then
  echo "✓ No protected decisions/meetings deleted in PR diff. Stability hook clean."
  exit 0
fi

echo "Protected files deleted in PR diff:"
printf '  - %s\n' "${DELETED_FILES[@]}"
echo ""

# For each deleted file, look for a sibling with superseded_by pointing to it
VIOLATIONS=()
for deleted in "${DELETED_FILES[@]}"; do
  basename=$(basename "$deleted")
  dir=$(dirname "$deleted")

  # Check all files in same directory at HEAD for superseded_by reference
  superseder_found=false
  while IFS= read -r candidate; do
    [[ -z "$candidate" ]] && continue
    if git show "${HEAD_REF}:${candidate}" 2>/dev/null | grep -E "^superseded_by:.*${basename}|^\s+- ${basename}" >/dev/null; then
      echo "✓ Deletion of '${deleted}' is properly superseded by '${candidate}'"
      superseder_found=true
      break
    fi
  done < <(git ls-tree -r "${HEAD_REF}" --name-only -- "${dir}/*.md" 2>/dev/null || true)

  if ! $superseder_found; then
    VIOLATIONS+=("$deleted")
  fi
done

if [[ ${#VIOLATIONS[@]} -eq 0 ]]; then
  echo ""
  echo "✓ All ${#DELETED_FILES[@]} deletions are properly superseded. Stability hook clean."
  exit 0
fi

echo ""
echo "✗ VIOLATION: ${#VIOLATIONS[@]} file(s) deleted without superseding record:"
printf '  - %s\n' "${VIOLATIONS[@]}"
echo ""
echo "Per .claude/rules/pre-release-governance.md § Plan Amendment Policy,"
echo "deletion of formally ratified ADRs/MEETs requires either:"
echo "  (a) A superseding file with 'superseded_by: <deleted-file-name>' in frontmatter, OR"
echo "  (b) An explicit MEET ratification of the deletion (referenced in log.md)"
echo ""
echo "To fix:"
echo "  1. Restore the deleted file(s), OR"
echo "  2. Add a superseding file with 'superseded_by:' frontmatter, OR"
echo "  3. Convene a meeting to ratify the deletion, then this script can be bypassed"
echo "     via [SUPERSEDER-EXEMPT] tag in commit message after manual review."
echo ""
echo "Reference: MEET-2026-05-12 D5 (audit trail repair after fc1793dd silent deletion)"
exit 1
