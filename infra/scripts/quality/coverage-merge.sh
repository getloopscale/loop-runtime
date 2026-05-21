#!/usr/bin/env bash
# Merge Go cover profile + TS lcov into single unified lcov.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.1.
#
# Env vars:
#   GO_PROFILE   — Go coverage profile (default: /tmp/coverage.out)
#   CLI_LCOV     — CLI lcov from coverage-cli.sh (default: /tmp/cli-coverage.lcov)
#   OUTPUT_FILE  — merged lcov (default: /tmp/coverage-merged.lcov)
#
# Behavior:
#   - Converts Go profile → lcov via `gcov2lcov` (or inline awk fallback)
#   - Concatenates Go lcov + CLI lcov
#   - Handles missing inputs gracefully (one of them empty → use the other)

set -euo pipefail

GO_PROFILE="${GO_PROFILE:-/tmp/coverage.out}"
CLI_LCOV="${CLI_LCOV:-/tmp/cli-coverage.lcov}"
OUTPUT_FILE="${OUTPUT_FILE:-/tmp/coverage-merged.lcov}"

: > "$OUTPUT_FILE"

# Convert Go profile to lcov format (minimal awk-based converter)
# Format expected by downstream tools (codecov, diff-cover):
#   TN:<test_name>
#   SF:<source_file>
#   DA:<line>,<hit_count>
#   LF:<lines_found>
#   LH:<lines_hit>
#   end_of_record
go_profile_to_lcov() {
  local profile="$1"
  local out="$2"

  if [ ! -s "$profile" ]; then
    echo "[coverage-merge] Go profile $profile missing/empty — skipping"
    return 0
  fi

  # Skip the mode header (first line)
  # Each line format: <file>:<startLine>.<startCol>,<endLine>.<endCol> <numStatements> <count>
  awk '
    NR == 1 { next }   # skip "mode: atomic"
    {
      # Parse: file.go:1.2,3.4 N C
      split($1, fp, ":")
      file = fp[1]
      split(fp[2], range, ",")
      split(range[1], start, ".")
      split(range[2], end, ".")
      stmts = $2
      count = $3

      if (file != last_file) {
        if (last_file != "") {
          print "LF:" lf > out
          print "LH:" lh > out
          print "end_of_record" > out
        }
        print "SF:" file > out
        last_file = file
        lf = 0; lh = 0
      }
      for (line = start[1]; line <= end[1]; line++) {
        print "DA:" line "," count > out
        lf++
        if (count > 0) lh++
      }
    }
    END {
      if (last_file != "") {
        print "LF:" lf > out
        print "LH:" lh > out
        print "end_of_record" > out
      }
    }
  ' out="$out" "$profile"
}

GO_LCOV="$(mktemp -t go-lcov-XXXXXX)"
trap 'rm -f "$GO_LCOV"' EXIT

go_profile_to_lcov "$GO_PROFILE" "$GO_LCOV"

# Concatenate Go + CLI lcov
if [ -s "$GO_LCOV" ]; then
  cat "$GO_LCOV" >> "$OUTPUT_FILE"
fi

if [ -s "$CLI_LCOV" ]; then
  cat "$CLI_LCOV" >> "$OUTPUT_FILE"
fi

if [ ! -s "$OUTPUT_FILE" ]; then
  echo "[coverage-merge] WARN: merged output empty (no Go or CLI coverage available)"
  exit 0
fi

LINE_COUNT=$(wc -l < "$OUTPUT_FILE")
FILE_COUNT=$(grep -c '^SF:' "$OUTPUT_FILE" || echo 0)
echo "[coverage-merge] merged: $FILE_COUNT files / $LINE_COUNT lines → $OUTPUT_FILE"
