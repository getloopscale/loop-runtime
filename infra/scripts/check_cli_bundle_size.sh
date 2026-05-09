#!/usr/bin/env bash
#
# T9.3 of deep-review-remediation-plan (find_dep_cli_otel_bundle_bloat LOW).
#
# T9.3 was deferred per KISS+YAGNI ("bundle size acceptable; lazy-load adds
# branching complexity for a 2-3MB win"). This lint is the regression
# guard: if a PR adds dependencies that bloat the compiled `dist/` output
# beyond the documented baseline, the build fails and the author has to
# either justify the addition (ADR + bump baseline) OR ship the lazy-load
# refactor that T9.3 originally proposed.
#
# Baseline captured 2026-05-09: dist size = ~1.0MB compiled JS.
# Hard cap = 4MB to leave headroom for organic feature growth without
# forcing the lazy-load refactor immediately.
#
# Exit 0: dist size <= MAX_BYTES.
# Exit 1: dist size > MAX_BYTES (regression).

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"
DIST_DIR="$REPO_ROOT/cli/dist"

# 4MB cap. Current baseline ~1MB, so this leaves ~3MB headroom.
MAX_BYTES=$((4 * 1024 * 1024))

if [ ! -d "$DIST_DIR" ]; then
    echo "PASS — cli/dist not present (run 'cd cli && npm run build' first)."
    exit 0
fi

# Sum total bytes of compiled JS files.
size=$(find "$DIST_DIR" -name '*.js' -printf '%s\n' | awk '{s+=$1} END {print s+0}')

if [ "$size" -le "$MAX_BYTES" ]; then
    printf "PASS — cli/dist size %d bytes (%.1f MB) <= cap %d bytes (%.1f MB).\n" \
        "$size" "$(awk "BEGIN {printf \"%.1f\", $size/1024/1024}")" \
        "$MAX_BYTES" "$(awk "BEGIN {printf \"%.1f\", $MAX_BYTES/1024/1024}")"
    exit 0
fi

cat <<EOF >&2
FAIL — T9.3 governance violation
================================

cli/dist size = $size bytes ($(awk "BEGIN {printf \"%.1f MB\", $size/1024/1024}"))
cap          = $MAX_BYTES bytes ($(awk "BEGIN {printf \"%.1f MB\", $MAX_BYTES/1024/1024}"))

The CLI bundle has grown beyond the documented headroom. T9.3 of
deep-review-remediation-plan was deferred per KISS+YAGNI; the deferral
was explicit that re-evaluation triggers if "bundle size becomes a CI
issue." This lint is that re-evaluation trigger.

Options:
  1. Identify the new heavy dependency and remove/lazy-load it.
  2. Implement the lazy-load refactor T9.3 originally proposed
     (move @opentelemetry/* to optionalDependencies + dynamic import).
  3. Bump MAX_BYTES in this script + add an ADR explaining why the
     bundle growth is necessary.

See: docs/plans/deep-review-remediation-plan.md (T9.3).
EOF
exit 1
