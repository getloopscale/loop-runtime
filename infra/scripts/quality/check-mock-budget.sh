#!/usr/bin/env bash
# Mock budget gate — max 5 mocks per test file (per ADR D8).
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.4 + EC-9.
#
# What counts as a mock (top-level declarations only per EC-9):
#   Go:  `gomock.NewController(t)` (top-level), `mock.New*` (top-level assignment)
#   TS:  `jest.mock(...)` or `vi.mock(...)` at module top
#
# What does NOT count (EC-9):
#   - Inline `jest.fn()` used as callback (e.g., setInterval(jest.fn(), 100))
#   - Mocks declared inside test functions but NOT at module top
#
# Opt-out: file comment `// mock-budget: N — <reason>` allows up to N mocks
#
# Env vars:
#   TARGET_DIR   — directory to scan (default: repo root)
#   BUDGET       — default max mocks per file (default: 5)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TARGET_DIR="${TARGET_DIR:-$REPO_ROOT}"
BUDGET="${BUDGET:-5}"

VIOLATIONS=0
VIOLATION_DETAILS=""

# Read opt-out budget from file comment
read_opt_out_budget() {
  local f="$1"
  # Look for `// mock-budget: N` in first 50 lines.
  # `|| true` on the trailing pipe to neutralize pipefail when the file
  # has no annotation (grep returns 1 on no-match, which aborts under
  # set -euo pipefail). Empty stdout = "no opt-out".
  { head -50 "$f" 2>/dev/null \
      | grep -oE '//[[:space:]]*mock-budget:[[:space:]]*[0-9]+' \
      | grep -oE '[0-9]+' \
      | head -1; } || true
}

count_go_mocks() {
  local f="$1"
  # Top-level only (column 0, not indented)
  # Match `gomock.NewController` at start of line OR after `=`
  # AND `mock.NewMock*` patterns
  grep -cE '(^var .*= mock\.New|^[[:space:]]+\w+ := gomock\.NewController|^mock[^/]* := |gomock\.NewController)' "$f" 2>/dev/null \
    | head -1 || echo 0
}

count_ts_mocks() {
  local f="$1"
  # Top-level jest.mock / vi.mock calls only (NOT inline jest.fn())
  grep -cE '^[[:space:]]*(jest|vi)\.mock\(' "$f" 2>/dev/null | head -1 || echo 0
}

# Walk repo
while IFS= read -r f; do
  [ -z "$f" ] && continue
  local_budget="$BUDGET"
  opt_out=$(read_opt_out_budget "$f")
  if [ -n "$opt_out" ]; then
    local_budget="$opt_out"
  fi

  case "$f" in
    *_test.go) count=$(count_go_mocks "$f") ;;
    *.test.ts|*.spec.ts|*.test.tsx|*.spec.tsx) count=$(count_ts_mocks "$f") ;;
    *) continue ;;
  esac

  count=$(echo "$count" | tr -d '\n' | head -c 5)
  count="${count:-0}"

  if [ "$count" -gt "$local_budget" ]; then
    VIOLATIONS=$((VIOLATIONS + 1))
    VIOLATION_DETAILS="$VIOLATION_DETAILS$f: $count mocks (budget=$local_budget)\n"
  fi
done < <(find "$TARGET_DIR" \
  \( -path '*/vendor/*' -o -path '*/node_modules/*' -o \
     -path '*/.git/*' -o -path '*/dist/*' -o -path '*/build/*' \) -prune -o \
  \( -name '*_test.go' -o -name '*.test.ts' -o -name '*.spec.ts' \
     -o -name '*.test.tsx' -o -name '*.spec.tsx' \) -type f -print 2>/dev/null)

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[mock-budget] FAIL: $VIOLATIONS file(s) over budget:" >&2
  printf '%b' "$VIOLATION_DETAILS" >&2
  echo "[mock-budget] design smell — consider refactor or split file" >&2
  echo "[mock-budget] OR add comment: // mock-budget: N — <justification>" >&2
  exit 1
fi

echo "[mock-budget] PASS — all test files within budget ($BUDGET mocks/file)"
