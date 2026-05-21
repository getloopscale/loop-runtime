#!/usr/bin/env bash
# Collect Go coverage across all packages in a target directory.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.1.
#
# Env vars:
#   TARGET_DIR    — directory containing go.mod to test (default: repo root module)
#   OUTPUT_FILE   — path to write merged coverage profile (default: /tmp/coverage.out)
#   COVERAGE_MODE — atomic|count|set (default: atomic)
#
# Behavior:
#   - Excludes vendor/ paths from coverage report (EC: vendor leak)
#   - Handles empty packages gracefully (exit 0)
#   - Handles doc-only packages (no testable statements) without error (EC-10)
#   - Merges per-package profiles via gocovmerge into single OUTPUT_FILE
#
# Used by:
#   - task test:coverage:go
#   - .github/workflows/coverage-report.yml
#   - Phase 2 gates (coverage-diff, baseline-compare)

set -euo pipefail

TARGET_DIR="${TARGET_DIR:-$(pwd)}"
OUTPUT_FILE="${OUTPUT_FILE:-/tmp/coverage.out}"
COVERAGE_MODE="${COVERAGE_MODE:-atomic}"

# Sanity: TARGET_DIR must contain go.mod
if [ ! -f "$TARGET_DIR/go.mod" ]; then
  echo "[coverage-go] no go.mod at $TARGET_DIR — nothing to test, exit 0"
  # Write empty profile with mode header so downstream tools don't error
  echo "mode: $COVERAGE_MODE" > "$OUTPUT_FILE"
  exit 0
fi

cd "$TARGET_DIR"

# Discover packages, excluding vendor/
mapfile -t PACKAGES < <(go list ./... 2>/dev/null | grep -v '/vendor/' || true)

if [ "${#PACKAGES[@]}" -eq 0 ]; then
  echo "[coverage-go] no Go packages discovered in $TARGET_DIR (doc-only or empty)"
  # EC-10: doc-only package — exit 0 with empty profile
  echo "mode: $COVERAGE_MODE" > "$OUTPUT_FILE"
  exit 0
fi

echo "[coverage-go] discovered ${#PACKAGES[@]} packages in $TARGET_DIR"

# Build comma-separated -coverpkg list (excludes vendor explicitly so test
# output cannot contain vendor file paths even if dep is exercised).
COVERPKG_LIST=$(IFS=,; echo "${PACKAGES[*]}")

# Run go test with coverage per-package, collect profiles into tmp dir
PROFILE_DIR="$(mktemp -d -t coverage-go-XXXXXX)"
trap 'rm -rf "$PROFILE_DIR"' EXIT

PROFILE_COUNT=0
PROFILE_WITH_STATEMENTS=0
for pkg in "${PACKAGES[@]}"; do
  # Sanitize package path to filename
  safe_name="${pkg//\//_}"
  profile="$PROFILE_DIR/${safe_name}.out"

  # `go test` may exit non-zero on no-test packages; tolerate.
  # -coverpkg explicitly enumerates non-vendor packages → vendor cannot leak
  if go test \
       -coverprofile="$profile" \
       -covermode="$COVERAGE_MODE" \
       -coverpkg="$COVERPKG_LIST" \
       -timeout 60s \
       "$pkg" >/dev/null 2>&1; then
    if [ -s "$profile" ]; then
      PROFILE_COUNT=$((PROFILE_COUNT + 1))
      # Count profiles with actual statements (not just header line)
      if [ "$(wc -l < "$profile")" -gt 1 ]; then
        PROFILE_WITH_STATEMENTS=$((PROFILE_WITH_STATEMENTS + 1))
      fi
    fi
  fi
  # Non-zero exit (no tests, build errors in test-only context) tolerated
done

echo "[coverage-go] collected $PROFILE_COUNT profiles ($PROFILE_WITH_STATEMENTS with statements)"

# EC-10: doc-only packages produce header-only profiles. Merge would yield
# empty file. Detect + write a valid header-only profile instead.
if [ "$PROFILE_WITH_STATEMENTS" -eq 0 ]; then
  echo "mode: $COVERAGE_MODE" > "$OUTPUT_FILE"
  echo "[coverage-go] all profiles header-only (doc-only or no-test packages)"
  exit 0
fi

if command -v gocovmerge >/dev/null 2>&1; then
  # shellcheck disable=SC2046
  gocovmerge $(find "$PROFILE_DIR" -name "*.out" -size +0c) > "$OUTPUT_FILE"
else
  # Fallback: manual merge (less rigorous but no extra deps)
  echo "mode: $COVERAGE_MODE" > "$OUTPUT_FILE"
  find "$PROFILE_DIR" -name "*.out" -size +0c -exec grep -hv '^mode:' {} \; >> "$OUTPUT_FILE"
fi

# Sanity: output must have mode header
if [ ! -s "$OUTPUT_FILE" ]; then
  # gocovmerge produces empty when all inputs are header-only — write header
  echo "mode: $COVERAGE_MODE" > "$OUTPUT_FILE"
fi

if ! head -1 "$OUTPUT_FILE" | grep -q '^mode:'; then
  echo "[coverage-go] ERROR: output missing mode header" >&2
  exit 1
fi

# Strip any vendor/ lines that may have leaked through (defense in depth +
# protection against -coverpkg incorrectly inheriting vendor deps)
if grep -q '/vendor/' "$OUTPUT_FILE"; then
  grep -v '/vendor/' "$OUTPUT_FILE" > "$OUTPUT_FILE.tmp"
  mv "$OUTPUT_FILE.tmp" "$OUTPUT_FILE"
fi

echo "[coverage-go] wrote merged profile to $OUTPUT_FILE"
