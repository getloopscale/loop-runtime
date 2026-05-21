#!/usr/bin/env bash
# Skip-link enforcement: every t.Skip() / it.skip() / etc. MUST have
# issue URL OR be in a `_skip_test.go` / `.skip.test.ts` filename.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.5 + EC-8.
#
# Skip API variants covered (per EC-8):
#   Go:  t.Skip(), t.SkipNow(), t.Skipf(), b.Skip(), b.SkipNow(), b.Skipf()
#   TS:  it.skip(...), describe.skip(...), test.skip(...), xit(...), xdescribe(...)
#
# Env vars:
#   TARGET_DIR  — directory to scan (default: repo root)
#   ISSUE_URL_PATTERN — regex matching valid issue URL (default: github.com/loopzero/loopzero/issues/N)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TARGET_DIR="${TARGET_DIR:-$REPO_ROOT}"
ISSUE_URL_PATTERN="${ISSUE_URL_PATTERN:-https://github\.com/[^/]+/[^/]+/issues/[0-9]+}"

VIOLATIONS=0
VIOLATION_DETAILS=""

# Skip files matching naming convention (intentional permanent skip)
skip_file_by_name() {
  case "$1" in
    *_skip_test.go|*.skip.test.ts|*.skip.spec.ts) return 0 ;;
    *) return 1 ;;
  esac
}

# Find all skip calls in a file with their context line
# Returns lines containing skip but WITHOUT issue URL nearby
scan_go_file() {
  local f="$1"
  # Match: t.Skip( | t.SkipNow( | t.Skipf( | b.Skip etc.
  while IFS=: read -r line_num line_content; do
    [ -z "$line_content" ] && continue
    # Check this line AND previous/next 3 lines for issue URL
    context=$(sed -n "$((line_num - 3)),$((line_num + 3))p" "$f")
    if ! echo "$context" | grep -qE "$ISSUE_URL_PATTERN"; then
      VIOLATIONS=$((VIOLATIONS + 1))
      VIOLATION_DETAILS="$VIOLATION_DETAILS$f:$line_num: $line_content\n"
    fi
  done < <(grep -nE '\b[tb]\.(Skip|SkipNow|Skipf)\(' "$f" 2>/dev/null || true)
}

scan_ts_file() {
  local f="$1"
  while IFS=: read -r line_num line_content; do
    [ -z "$line_content" ] && continue
    context=$(sed -n "$((line_num - 3)),$((line_num + 3))p" "$f")
    if ! echo "$context" | grep -qE "$ISSUE_URL_PATTERN"; then
      VIOLATIONS=$((VIOLATIONS + 1))
      VIOLATION_DETAILS="$VIOLATION_DETAILS$f:$line_num: $line_content\n"
    fi
  done < <(grep -nE '(\b(it|describe|test)\.skip\(|^[[:space:]]*x(it|describe)\()' "$f" 2>/dev/null || true)
}

# Walk repo
while IFS= read -r f; do
  [ -z "$f" ] && continue
  if skip_file_by_name "$f"; then
    continue
  fi
  case "$f" in
    *_test.go) scan_go_file "$f" ;;
    *.test.ts|*.spec.ts|*.test.tsx|*.spec.tsx) scan_ts_file "$f" ;;
  esac
done < <(find "$TARGET_DIR" \
  \( -path '*/vendor/*' -o -path '*/node_modules/*' -o \
     -path '*/.git/*' -o -path '*/dist/*' -o -path '*/build/*' \) -prune -o \
  \( -name '*_test.go' -o -name '*.test.ts' -o -name '*.spec.ts' \
     -o -name '*.test.tsx' -o -name '*.spec.tsx' \) -type f -print 2>/dev/null)

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[skip-link] FAIL: $VIOLATIONS skip(s) without issue URL:" >&2
  # Use printf '%b' to interpret \n in VIOLATION_DETAILS without
  # interpreting unrelated backslash sequences in matched lines
  printf '%b' "$VIOLATION_DETAILS" >&2
  echo "[skip-link] fix: add // ${ISSUE_URL_PATTERN} on or near the Skip call," >&2
  echo "[skip-link] OR rename file to *_skip_test.go (intentional permanent skip)" >&2
  exit 1
fi

echo "[skip-link] PASS — all skip calls have issue URL (or file is *_skip_test.go)"
