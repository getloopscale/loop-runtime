#!/usr/bin/env bash
# Collect TS/JS coverage for cli/ via Jest --coverage with consistent output.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.1.
#
# Env vars:
#   TARGET_DIR  — cli/ directory (default: $REPO_ROOT/cli)
#   OUTPUT_FILE — path to write lcov.info (default: /tmp/cli-coverage.lcov)
#
# Behavior:
#   - Runs `npm test -- --ci --coverage --coverageReporters=lcov`
#   - Copies generated cli/coverage/lcov.info → OUTPUT_FILE
#   - Excludes node_modules from final report
#   - Handles missing cli/ (CI partial-checkout) without error

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TARGET_DIR="${TARGET_DIR:-$REPO_ROOT/cli}"
OUTPUT_FILE="${OUTPUT_FILE:-/tmp/cli-coverage.lcov}"

if [ ! -d "$TARGET_DIR" ]; then
  echo "[coverage-cli] $TARGET_DIR not found — partial checkout, exit 0"
  # Write empty-but-valid lcov so merge step doesn't error
  : > "$OUTPUT_FILE"
  exit 0
fi

if [ ! -f "$TARGET_DIR/package.json" ]; then
  echo "[coverage-cli] no package.json at $TARGET_DIR — exit 0"
  : > "$OUTPUT_FILE"
  exit 0
fi

cd "$TARGET_DIR"

echo "[coverage-cli] running jest with --coverage"
npm test -- --ci --coverage --coverageReporters=lcov --passWithNoTests >/dev/null 2>&1 || {
  # Jest exits 1 if tests fail; coverage may still be written
  echo "[coverage-cli] WARN: jest exited non-zero (some tests may have failed)"
}

LCOV_SRC="$TARGET_DIR/coverage/lcov.info"
if [ -f "$LCOV_SRC" ]; then
  cp "$LCOV_SRC" "$OUTPUT_FILE"
  # Strip node_modules entries (defense in depth — jest's coveragePathIgnorePatterns may not be set)
  if grep -q 'SF:.*node_modules' "$OUTPUT_FILE"; then
    awk '
      /^SF:.*node_modules/ { skip=1; next }
      /^end_of_record/ { if (skip) { skip=0; next } else print }
      !skip { print }
    ' "$OUTPUT_FILE" > "$OUTPUT_FILE.tmp"
    mv "$OUTPUT_FILE.tmp" "$OUTPUT_FILE"
  fi
  echo "[coverage-cli] wrote lcov to $OUTPUT_FILE"
else
  echo "[coverage-cli] WARN: no lcov.info generated — writing empty"
  : > "$OUTPUT_FILE"
fi
