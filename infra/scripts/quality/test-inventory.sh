#!/usr/bin/env bash
# Catalog all test files in the repo, classified by category per ADR D7.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.3.
#
# Classification (filename convention):
#   *_e2e_test.go, *.e2e.test.ts, *.e2e.spec.ts → e2e
#   *_integration_test.go, *.integration.test.ts → integration
#   *_test.go, *.test.ts, *.spec.ts → unit (default)
#
# Output JSON structure:
#   {
#     "files": [
#       {"path": "pkg/foo/bar_test.go", "category": "unit", "test_count": 3, "lang": "go"},
#       ...
#     ],
#     "summary": {
#       "total_files": N,
#       "total_tests": N,
#       "unit": N,
#       "integration": N,
#       "e2e": N,
#       "ratio_unit_pct": NN.N,
#       "ratio_integration_pct": NN.N,
#       "ratio_e2e_pct": NN.N
#     },
#     "generated_at": "YYYY-MM-DDTHH:MM:SSZ"
#   }
#
# Excludes vendor/, node_modules/, charts/*/charts/ (helm deps),
# .git/, .terraform/, dist/, build/.
#
# Env vars:
#   TARGET_DIR  — directory to scan (default: repo root)
#   OUTPUT_FILE — JSON output path (default: infra/quality/test-inventory.json)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TARGET_DIR="${TARGET_DIR:-$REPO_ROOT}"
OUTPUT_FILE="${OUTPUT_FILE:-$REPO_ROOT/infra/quality/test-inventory.json}"

mkdir -p "$(dirname "$OUTPUT_FILE")"

# Classify a single file given its path
classify_file() {
  local f="$1"
  case "$f" in
    *_e2e_test.go|*.e2e.test.ts|*.e2e.spec.ts) echo "e2e" ;;
    *_integration_test.go|*.integration.test.ts|*.integration.spec.ts) echo "integration" ;;
    *_test.go|*.test.ts|*.spec.ts|*.test.tsx|*.spec.tsx) echo "unit" ;;
    *) echo "unknown" ;;
  esac
}

# Detect language from extension
detect_lang() {
  local f="$1"
  case "$f" in
    *.go) echo "go" ;;
    *.ts|*.tsx) echo "ts" ;;
    *) echo "other" ;;
  esac
}

# Count test functions in a file
count_tests() {
  local f="$1"
  local lang="$2"
  local count=0
  if [ "$lang" = "go" ]; then
    # Go: count `func TestXxx(` or `func BenchmarkXxx(`
    count=$(grep -cE '^func (Test|Benchmark)[A-Z]' "$f" 2>/dev/null || echo 0)
  elif [ "$lang" = "ts" ]; then
    # TS: count `it(`, `test(`, `describe(` at line-start or after whitespace
    # (matches Jest/Vitest/Mocha)
    count=$(grep -cE '^[[:space:]]*(it|test|describe)\(' "$f" 2>/dev/null || echo 0)
  fi
  # Sanitize (grep -c can emit weird output with multi-line)
  echo "$count" | head -1 | tr -d '\n'
}

# Build file list (exclude noise directories)
build_file_list() {
  find "$TARGET_DIR" \
    \( -path '*/vendor/*' -o \
       -path '*/node_modules/*' -o \
       -path '*/.git/*' -o \
       -path '*/.terraform/*' -o \
       -path '*/dist/*' -o \
       -path '*/build/*' -o \
       -path '*/charts/*/charts/*' \) -prune -o \
    \( -name '*_test.go' -o \
       -name '*.test.ts' -o \
       -name '*.spec.ts' -o \
       -name '*.test.tsx' -o \
       -name '*.spec.tsx' \) -type f -print 2>/dev/null
}

echo "[test-inventory] scanning $TARGET_DIR"

# Init counters
TOTAL_FILES=0
TOTAL_TESTS=0
UNIT_COUNT=0
INTEGRATION_COUNT=0
E2E_COUNT=0
UNKNOWN_COUNT=0

# Build temp accumulator (one JSON line per file)
ACC="$(mktemp -t test-inv-XXXXXX)"
trap 'rm -f "$ACC"' EXIT

while IFS= read -r abs_path; do
  [ -z "$abs_path" ] && continue
  # Make path relative to TARGET_DIR
  rel_path="${abs_path#"$TARGET_DIR"/}"

  category=$(classify_file "$rel_path")
  lang=$(detect_lang "$rel_path")
  count=$(count_tests "$abs_path" "$lang")

  case "$category" in
    unit) UNIT_COUNT=$((UNIT_COUNT + 1)) ;;
    integration) INTEGRATION_COUNT=$((INTEGRATION_COUNT + 1)) ;;
    e2e) E2E_COUNT=$((E2E_COUNT + 1)) ;;
    *) UNKNOWN_COUNT=$((UNKNOWN_COUNT + 1)) ;;
  esac

  TOTAL_FILES=$((TOTAL_FILES + 1))
  TOTAL_TESTS=$((TOTAL_TESTS + count))

  printf '    {"path": "%s", "category": "%s", "test_count": %s, "lang": "%s"}\n' \
    "$rel_path" "$category" "$count" "$lang" >> "$ACC"
done < <(build_file_list | sort)

# Compute ratios (avoid div-by-zero)
if [ "$TOTAL_FILES" -gt 0 ]; then
  ratio_unit=$(awk "BEGIN { printf \"%.1f\", ($UNIT_COUNT / $TOTAL_FILES) * 100 }")
  ratio_int=$(awk "BEGIN { printf \"%.1f\", ($INTEGRATION_COUNT / $TOTAL_FILES) * 100 }")
  ratio_e2e=$(awk "BEGIN { printf \"%.1f\", ($E2E_COUNT / $TOTAL_FILES) * 100 }")
else
  ratio_unit=0.0
  ratio_int=0.0
  ratio_e2e=0.0
fi

# Build JSON output
{
  echo "{"
  echo '  "files": ['
  # Join lines with commas (last line has no trailing comma)
  if [ -s "$ACC" ]; then
    awk 'NR > 1 { print prev "," } { prev = $0 } END { print prev }' "$ACC"
  fi
  echo '  ],'
  echo '  "summary": {'
  echo "    \"total_files\": $TOTAL_FILES,"
  echo "    \"total_tests\": $TOTAL_TESTS,"
  echo "    \"unit\": $UNIT_COUNT,"
  echo "    \"integration\": $INTEGRATION_COUNT,"
  echo "    \"e2e\": $E2E_COUNT,"
  echo "    \"unknown\": $UNKNOWN_COUNT,"
  echo "    \"ratio_unit_pct\": $ratio_unit,"
  echo "    \"ratio_integration_pct\": $ratio_int,"
  echo "    \"ratio_e2e_pct\": $ratio_e2e"
  echo '  },'
  echo "  \"generated_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\""
  echo "}"
} > "$OUTPUT_FILE"

echo "[test-inventory] scanned $TOTAL_FILES files / $TOTAL_TESTS tests"
echo "[test-inventory] unit=$UNIT_COUNT ($ratio_unit%)  integration=$INTEGRATION_COUNT ($ratio_int%)  e2e=$E2E_COUNT ($ratio_e2e%)"
echo "[test-inventory] wrote $OUTPUT_FILE"
