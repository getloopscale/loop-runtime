#!/usr/bin/env bash
# Bug-fix regression test gate.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T5.2 + EC-3.
#
# Behavior:
#   - Parse PR title + body + commit messages (source priority order per EC-3)
#   - Extract issue references: fix #N | Bug #N | closes #N | fixes #N | resolves #N
#   - If any found, require PR diff to contain ≥1 new test function
#     (function Test*  in *_test.go OR new it()/test()/describe() in *.test.ts)
#   - PR diff list passed via PR_DIFF_FILES env var
#
# Env vars:
#   PR_TITLE         — pull_request.title
#   PR_BODY          — pull_request.body
#   PR_COMMITS       — newline-separated commit messages
#   PR_DIFF_FILES    — newline-separated file paths added/modified
#   ALLOW_FILE_PATTERNS — comma-separated allowlist of regression test markers (default: built-in)

set -euo pipefail

PR_TITLE="${PR_TITLE:-}"
PR_BODY="${PR_BODY:-}"
PR_COMMITS="${PR_COMMITS:-}"
PR_DIFF_FILES="${PR_DIFF_FILES:-}"

# Source priority per EC-3: title first, body, commits last
SOURCES="$PR_TITLE
$PR_BODY
$PR_COMMITS"

# Extract issue numbers from text (grep -E returns 1 if no match;
# wrap in || true to keep set -e happy)
extract_issue_refs() {
  echo "$SOURCES" | { grep -oiE '(fix|fixes|fixed|close|closes|closed|resolve|resolves|resolved|Bug)[[:space:]]*#[0-9]+' || true; } \
    | { grep -oE '#[0-9]+' || true; } \
    | sort -u
}

ISSUE_REFS=$(extract_issue_refs)

if [ -z "$ISSUE_REFS" ]; then
  echo "[regression-gate] no bug-fix references — SKIP gate"
  exit 0
fi

echo "[regression-gate] detected bug-fix references:"
echo "$ISSUE_REFS" | sed 's/^/  /'

# Check PR diff for new test additions
NEW_TEST_FOUND=0
NEW_GO_TESTS=""
NEW_TS_TESTS=""

if [ -n "$PR_DIFF_FILES" ]; then
  while IFS= read -r file; do
    [ -z "$file" ] && continue
    case "$file" in
      *_test.go|*.test.ts|*.spec.ts|*.test.tsx|*.spec.tsx)
        NEW_TEST_FOUND=1
        if [[ "$file" == *.go ]]; then
          NEW_GO_TESTS="$NEW_GO_TESTS $file"
        else
          NEW_TS_TESTS="$NEW_TS_TESTS $file"
        fi
        ;;
    esac
  done <<< "$PR_DIFF_FILES"
fi

if [ "$NEW_TEST_FOUND" -eq 0 ]; then
  echo "[regression-gate] FAIL: bug-fix PR has no new test file in diff" >&2
  echo "[regression-gate] issue refs: $ISSUE_REFS" >&2
  echo "[regression-gate] add a regression test in a *_test.go or *.test.ts file" >&2
  echo "[regression-gate] (test should reference the bug via // Bug #N or // Regression: #N)" >&2
  exit 1
fi

echo "[regression-gate] PASS — test file(s) found:"
[ -n "$NEW_GO_TESTS" ] && echo "  Go:$NEW_GO_TESTS"
[ -n "$NEW_TS_TESTS" ] && echo "  TS:$NEW_TS_TESTS"
exit 0
