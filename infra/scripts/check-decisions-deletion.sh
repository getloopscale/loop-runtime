#!/usr/bin/env bash
# T1.14 — Decisions + Operations Deletion Guard
# Fail CI if any of the following protected artifacts is deleted in a PR
# without a sibling file containing `superseded_by:` reference pointing to
# the deleted file:
#
#   - .claude/knowledge-base/decisions/*.md     (ADRs)
#   - .claude/knowledge-base/meetings/*.md      (MEET DRs)
#   - .claude/knowledge-base/concepts/operations/*-runbook.md  (runbooks)
#   - .claude/knowledge-base/concepts/operations/*-inventory.md (inventories)
#
# Lesson source 1 (decisions/meetings): MEET-2026-05-12 D5 — commit fc1793dd
# silently deleted ADR-0020 and ADR-0021 with an unrelated commit message
# ("Delete meeting minutes for iterations 005-008"), violating Plan Amendment
# Policy. T1.14 prevents recurrence by enforcing the policy mechanically.
#
# Lesson source 2 (runbooks/inventories): same commit fc1793dd ALSO swept
# docs/runbooks/network-policy-egress-inventory.md (T8.5a runbook), breaking
# api/tests/unit/network_policy_egress_test.go::TestNetworkPolicy_DocumentsT85b
# silently. The runbook was restored 2026-05-12 in commit a6e8b895 via
# MEET-ASYNC-AMENDMENT-2026-05-12-001. Glob extended to prevent recurrence.
#
# Per .claude/rules/pre-release-governance.md § Plan Amendment Policy:
#   Deletion of formally ratified ADRs/MEETs/runbooks requires either:
#   (a) A superseding file with `superseded_by: <deleted-file-name>` frontmatter
#   (b) Explicit MEET ratification of the deletion (commit tag [SUPERSEDER-EXEMPT])
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

# Each entry is "dir|glob". The glob narrows which files inside dir are
# protected — decisions/meetings protect ALL *.md, operations protects
# only *-runbook.md and *-inventory.md (not arbitrary notes).
PROTECTED_PATHS=(
  ".claude/knowledge-base/decisions|*.md"
  ".claude/knowledge-base/meetings|*.md"
  ".claude/knowledge-base/concepts/operations|*-runbook.md"
  ".claude/knowledge-base/concepts/operations|*-inventory.md"
)

# Find all matching files deleted between BASE and HEAD.
#
# Implementation note: `git diff` and `git ls-tree` differ in pathspec
# magic support — `:(glob)` works on diff but not ls-tree. To stay
# portable across git versions, we list files at the directory level
# and filter the glob in shell via a `case` statement.
DELETED_FILES=()
for entry in "${PROTECTED_PATHS[@]}"; do
  dir="${entry%%|*}"
  glob="${entry##*|}"
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    # Filter: only keep files whose basename matches the glob.
    base="$(basename "$line")"
    # shellcheck disable=SC2254 # glob is intentionally a pattern
    case "$base" in
      $glob) DELETED_FILES+=("$line") ;;
    esac
  done < <(git diff --name-only --diff-filter=D "${BASE_REF}...${HEAD_REF}" -- "${dir}/" 2>/dev/null || true)
done

if [[ ${#DELETED_FILES[@]} -eq 0 ]]; then
  echo "✓ No protected decisions/meetings/runbooks deleted in PR diff. Stability hook clean."
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

  # Check all .md files in same directory at HEAD for superseded_by reference.
  #
  # NOTE: `git ls-tree` does NOT support `:(glob)` pathspec magic in git
  # <= 2.43 (the version on ubuntu-latest CI runners). Pass the directory
  # as plain pathspec and filter `.md` via shell — works across versions.
  superseder_found=false
  while IFS= read -r candidate; do
    [[ -z "$candidate" ]] && continue
    case "$candidate" in
      *.md) ;;
      *) continue ;;
    esac
    if git show "${HEAD_REF}:${candidate}" 2>/dev/null | grep -E "^superseded_by:.*${basename}|^\s+- ${basename}" >/dev/null; then
      echo "✓ Deletion of '${deleted}' is properly superseded by '${candidate}'"
      superseder_found=true
      break
    fi
  done < <(git ls-tree -r "${HEAD_REF}" --name-only -- "${dir}/" 2>/dev/null || true)

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
