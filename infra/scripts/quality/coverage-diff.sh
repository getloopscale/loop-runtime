#!/usr/bin/env bash
# Coverage diff gate — fail if NEW code (PR-touched lines) has <THRESHOLD% covered.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T2.1.
#
# Per ADR D1: diff-aware (not global %) — enforces discipline on NEW code only.
# Per EC-7: rename-only PRs PASS (empty diff = trivially PASS).
#
# Env vars:
#   DIFF_FILE   — text file with one "path:line" per touched line (default: /tmp/pr-diff.txt)
#   LCOV_FILE   — merged lcov from coverage-merge.sh (default: /tmp/coverage-merged.lcov)
#   THRESHOLD   — minimum coverage % required on touched lines (default: 80)
#   REPORT_FILE — optional path to write summary report
#
# Behavior:
#   - Filters DIFF_FILE to source files only (Go, TS, TSX); ignores .md, .yml, etc.
#   - Skips test files (*_test.go, *.test.ts, *.spec.ts) — test files exempt
#   - For each source line in diff, checks if covered (DA:<line>,N where N>0)
#   - Computes covered / total ratio
#   - PASS if no source lines touched, OR ratio >= THRESHOLD
#   - FAIL otherwise

set -euo pipefail

DIFF_FILE="${DIFF_FILE:-/tmp/pr-diff.txt}"
LCOV_FILE="${LCOV_FILE:-/tmp/coverage-merged.lcov}"
THRESHOLD="${THRESHOLD:-80}"
REPORT_FILE="${REPORT_FILE:-}"

# Empty/missing diff = PASS (EC-7 rename-only PR)
if [ ! -s "$DIFF_FILE" ]; then
  echo "[coverage-diff] empty diff — PASS (rename-only or doc-only PR)"
  exit 0
fi

# Classify files
is_source_file() {
  case "$1" in
    *_test.go|*.test.ts|*.spec.ts|*.test.tsx|*.spec.tsx) return 1 ;;  # test = skip
    *.go|*.ts|*.tsx) return 0 ;;  # source
    *) return 1 ;;
  esac
}

# Build map of covered lines from lcov: file → set of covered lines
# (lcov format: SF:<file>, then DA:<line>,<count> per line, end_of_record)
declare -A COVERED_LINES   # key: "$file:$line" → "covered" if count>0

if [ -s "$LCOV_FILE" ]; then
  current_file=""
  while IFS= read -r line; do
    case "$line" in
      SF:*) current_file="${line#SF:}" ;;
      DA:*)
        # DA:<line>,<count>
        rest="${line#DA:}"
        ln="${rest%%,*}"
        cnt="${rest#*,}"
        if [ -n "$current_file" ] && [ "$cnt" -gt 0 ] 2>/dev/null; then
          COVERED_LINES["$current_file:$ln"]=1
        fi
        ;;
      end_of_record) current_file="" ;;
    esac
  done < "$LCOV_FILE"
fi

# Count source lines + covered source lines from diff
TOTAL_SOURCE=0
COVERED_SOURCE=0
UNCOVERED_LINES=()

while IFS= read -r entry; do
  [ -z "$entry" ] && continue
  # entry format: "path:line"
  path="${entry%:*}"
  line_num="${entry##*:}"

  if ! is_source_file "$path"; then
    continue
  fi

  TOTAL_SOURCE=$((TOTAL_SOURCE + 1))
  if [ -n "${COVERED_LINES[$path:$line_num]:-}" ]; then
    COVERED_SOURCE=$((COVERED_SOURCE + 1))
  else
    UNCOVERED_LINES+=("$path:$line_num")
  fi
done < "$DIFF_FILE"

# If no source lines touched, PASS (test-only PR, doc-only, etc.)
if [ "$TOTAL_SOURCE" -eq 0 ]; then
  echo "[coverage-diff] no source lines in diff — PASS (test-only or non-source PR)"
  exit 0
fi

# Compute ratio
RATIO=$(awk "BEGIN { printf \"%.1f\", ($COVERED_SOURCE / $TOTAL_SOURCE) * 100 }")
echo "[coverage-diff] touched source lines: $TOTAL_SOURCE / covered: $COVERED_SOURCE = $RATIO%"

if [ -n "$REPORT_FILE" ]; then
  {
    echo "# Coverage Diff Report"
    echo ""
    echo "- **Touched source lines:** $TOTAL_SOURCE"
    echo "- **Covered:** $COVERED_SOURCE"
    echo "- **Ratio:** $RATIO%"
    echo "- **Threshold:** $THRESHOLD%"
    echo ""
    if [ "${#UNCOVERED_LINES[@]}" -gt 0 ]; then
      echo "## Uncovered lines"
      echo ""
      printf -- '- `%s`\n' "${UNCOVERED_LINES[@]}"
    fi
  } > "$REPORT_FILE"
fi

# Compare ratio to threshold
if awk "BEGIN { exit !($RATIO >= $THRESHOLD) }"; then
  echo "[coverage-diff] PASS ($RATIO% >= $THRESHOLD%)"
  exit 0
else
  echo "[coverage-diff] FAIL ($RATIO% < $THRESHOLD%)" >&2
  if [ "${#UNCOVERED_LINES[@]}" -gt 0 ]; then
    echo "[coverage-diff] uncovered lines:" >&2
    printf '  %s\n' "${UNCOVERED_LINES[@]}" >&2
  fi
  exit 1
fi
