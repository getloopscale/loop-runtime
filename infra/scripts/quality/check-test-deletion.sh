#!/usr/bin/env bash
# Test deletion audit gate.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T5.3 + EC-6.
#
# Behavior:
#   - Uses `git diff --find-renames=80% --diff-filter=D` to detect TRUE
#     deletions (NOT renames). Per EC-6: file rename = not a deletion.
#   - If TRUE deletions exist, PR must have `test-deletion-approved` label
#     OR PR body must contain "Test deletion justification: <reason>"
#
# Env vars:
#   BASE_REF      — git ref to diff against (default: origin/main)
#   HEAD_REF      — git ref of PR head (default: HEAD)
#   PR_BODY       — PR body text
#   PR_LABELS     — comma-separated label list

set -euo pipefail

BASE_REF="${BASE_REF:-origin/main}"
HEAD_REF="${HEAD_REF:-HEAD}"
PR_BODY="${PR_BODY:-}"
PR_LABELS="${PR_LABELS:-}"

# Custom diff via DIFF_OVERRIDE for hermetic tests
if [ -n "${DIFF_OVERRIDE:-}" ]; then
  DELETIONS="$DIFF_OVERRIDE"
else
  # --find-renames=80% classifies rename pairs as R (not D)
  # --diff-filter=D filters to TRUE deletions only
  DELETIONS=$(git diff --find-renames=80% --diff-filter=D --name-only \
    "$BASE_REF...$HEAD_REF" 2>/dev/null || true)
fi

# Filter to test files only
TEST_DELETIONS=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case "$f" in
    *_test.go|*.test.ts|*.spec.ts|*.test.tsx|*.spec.tsx)
      TEST_DELETIONS="$TEST_DELETIONS$f\n"
      ;;
  esac
done <<< "$DELETIONS"

if [ -z "$TEST_DELETIONS" ]; then
  echo "[test-deletion] no test file deletions — PASS"
  exit 0
fi

echo "[test-deletion] TRUE test deletions detected:"
printf "$TEST_DELETIONS" | sed 's/^/  /'

# Check label allowlist
IFS=',' read -ra LABELS_ARR <<< "$PR_LABELS"
for lbl in "${LABELS_ARR[@]}"; do
  lbl="$(echo "$lbl" | tr -d ' ')"
  if [ "$lbl" = "test-deletion-approved" ]; then
    echo "[test-deletion] PASS — 'test-deletion-approved' label present"
    exit 0
  fi
done

# Check PR body for justification
if echo "$PR_BODY" | grep -qiE "test deletion justification:"; then
  echo "[test-deletion] PASS — 'Test deletion justification:' found in PR body"
  exit 0
fi

echo "[test-deletion] FAIL: test deletions without approval" >&2
echo "[test-deletion] add 'test-deletion-approved' label OR" >&2
echo "[test-deletion] add 'Test deletion justification: <reason>' to PR body" >&2
exit 1
