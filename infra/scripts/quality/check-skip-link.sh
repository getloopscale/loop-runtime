#!/usr/bin/env bash
# Skip-link enforcement: every t.Skip() / it.skip() / etc. MUST have
# issue URL OR be in a `_skip_test.go` / `.skip.test.ts` filename.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.5 + EC-8.
#
# Skip API variants covered (per EC-8):
#   Go:  t.Skip(), t.SkipNow(), t.Skipf(), b.Skip(), b.SkipNow(), b.Skipf()
#   TS:  it.skip(...), describe.skip(...), test.skip(...), xit(...), xdescribe(...)
#
# Baseline-aware blocking mode:
#   - If `infra/quality/skip-link-baseline.txt` exists, violations listed there
#     are grandfathered (do NOT fail the gate). Lines starting with `#` and
#     blank lines are ignored. Baseline entries are `relative/path:LINE`.
#   - NEW violations (path:line NOT in baseline) FAIL the gate.
#   - This lets us graduate from advisory to blocking mode while paying down
#     legacy debt incrementally.
#
# Env vars:
#   TARGET_DIR        — directory to scan (default: repo root)
#   ISSUE_URL_PATTERN — regex matching valid issue URL
#                       (default: https://github.com/<owner>/<repo>/issues/N)
#   BASELINE_FILE     — path to baseline (default: infra/quality/skip-link-baseline.txt)
#   IGNORE_BASELINE   — if "1", skip baseline subtraction (used by tests + audit)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TARGET_DIR="${TARGET_DIR:-$REPO_ROOT}"
ISSUE_URL_PATTERN="${ISSUE_URL_PATTERN:-https://github\.com/[^/]+/[^/]+/issues/[0-9]+}"
BASELINE_FILE="${BASELINE_FILE:-$REPO_ROOT/infra/quality/skip-link-baseline.txt}"
IGNORE_BASELINE="${IGNORE_BASELINE:-0}"

# Violation collector: each entry is "RELPATH:LINE\tFULL_LINE_DETAIL"
# Using a delimiter that cannot appear in path:line so the baseline check
# operates on the leading key only.
VIOLATIONS_RAW=""

# Skip files matching naming convention (intentional permanent skip)
skip_file_by_name() {
  case "$1" in
    *_skip_test.go|*.skip.test.ts|*.skip.spec.ts) return 0 ;;
    *) return 1 ;;
  esac
}

emit_violation() {
  local abs_path="$1" line_num="$2" content="$3"
  # Keys are relative to TARGET_DIR so the baseline file is portable across
  # checkouts (production case: TARGET_DIR==REPO_ROOT → REPO_ROOT-relative).
  local rel_path="${abs_path#"$TARGET_DIR"/}"
  # Tab-separated: key<TAB>display
  VIOLATIONS_RAW="${VIOLATIONS_RAW}${rel_path}:${line_num}"$'\t'"${abs_path}:${line_num}: ${content}"$'\n'
}

# Find skip calls without nearby issue URL
scan_go_file() {
  local f="$1"
  while IFS=: read -r line_num line_content; do
    [ -z "$line_content" ] && continue
    context=$(sed -n "$((line_num - 3)),$((line_num + 3))p" "$f")
    if ! echo "$context" | grep -qE "$ISSUE_URL_PATTERN"; then
      emit_violation "$f" "$line_num" "$line_content"
    fi
  done < <(grep -nE '\b[tb]\.(Skip|SkipNow|Skipf)\(' "$f" 2>/dev/null || true)
}

scan_ts_file() {
  local f="$1"
  while IFS=: read -r line_num line_content; do
    [ -z "$line_content" ] && continue
    context=$(sed -n "$((line_num - 3)),$((line_num + 3))p" "$f")
    if ! echo "$context" | grep -qE "$ISSUE_URL_PATTERN"; then
      emit_violation "$f" "$line_num" "$line_content"
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

# Filter through baseline if it exists and IGNORE_BASELINE != "1"
NEW_VIOLATIONS=0
NEW_DETAILS=""
BASELINE_USED=0

if [ -n "$VIOLATIONS_RAW" ]; then
  if [ "$IGNORE_BASELINE" != "1" ] && [ -f "$BASELINE_FILE" ]; then
    BASELINE_USED=1
    # Build baseline set: strip comments + blank lines
    BASELINE_KEYS="$(grep -vE '^[[:space:]]*(#|$)' "$BASELINE_FILE" || true)"
    # Iterate violations, skip ones whose key matches baseline
    while IFS=$'\t' read -r key display; do
      [ -z "$key" ] && continue
      if printf '%s\n' "$BASELINE_KEYS" | grep -Fxq "$key"; then
        continue
      fi
      NEW_VIOLATIONS=$((NEW_VIOLATIONS + 1))
      NEW_DETAILS="${NEW_DETAILS}${display}"$'\n'
    done < <(printf '%s' "$VIOLATIONS_RAW")
  else
    # No baseline → all violations are new
    while IFS=$'\t' read -r key display; do
      [ -z "$key" ] && continue
      NEW_VIOLATIONS=$((NEW_VIOLATIONS + 1))
      NEW_DETAILS="${NEW_DETAILS}${display}"$'\n'
    done < <(printf '%s' "$VIOLATIONS_RAW")
  fi
fi

if [ "$NEW_VIOLATIONS" -gt 0 ]; then
  echo "[skip-link] FAIL: $NEW_VIOLATIONS NEW skip(s) without issue URL:" >&2
  printf '%b' "$NEW_DETAILS" >&2
  echo "[skip-link] fix: add // ${ISSUE_URL_PATTERN} on or near the Skip call," >&2
  echo "[skip-link] OR rename file to *_skip_test.go (intentional permanent skip)" >&2
  if [ "$BASELINE_USED" = "1" ]; then
    echo "[skip-link] note: $(grep -vcE '^[[:space:]]*(#|$)' "$BASELINE_FILE") grandfathered violations live in $BASELINE_FILE" >&2
    echo "[skip-link] do NOT add new entries to the baseline — fix or move to *_skip_test.go" >&2
  fi
  exit 1
fi

if [ "$BASELINE_USED" = "1" ]; then
  BASELINE_COUNT=$(grep -vcE '^[[:space:]]*(#|$)' "$BASELINE_FILE" || echo 0)
  echo "[skip-link] PASS — no NEW violations (${BASELINE_COUNT} grandfathered from $BASELINE_FILE)"
else
  echo "[skip-link] PASS — all skip calls have issue URL (or file is *_skip_test.go)"
fi
