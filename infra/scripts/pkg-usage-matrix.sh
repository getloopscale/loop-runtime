#!/usr/bin/env bash
# pkg-usage-matrix.sh — Generate Real Usage Matrix for pkg/ shared libraries.
#
# Usage:
#   pkg-usage-matrix.sh --check   # exit 1 if architecture.md is out-of-date
#   pkg-usage-matrix.sh --write   # regenerate the table in architecture.md
#
# Per pkg-sota-faang-grade-plan v1.0 T1.7 + gate PKG-G14
# (Documentation Reflects Real Usage).
#
# The matrix is bracketed in docs/technical/pkg/architecture.md by:
#   <!-- USAGE-MATRIX-START -->
#   ...generated...
#   <!-- USAGE-MATRIX-END -->
#
# CI calls --check; humans run --write locally before commit.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DOC="$ROOT/docs/technical/pkg/architecture.md"
START_MARK="<!-- USAGE-MATRIX-START -->"
END_MARK="<!-- USAGE-MATRIX-END -->"

mode="${1:-}"
case "$mode" in
  --check|--write) ;;
  *) echo "usage: $0 --check|--write" >&2; exit 2 ;;
esac

# ---------------------------------------------------------------------------
# Generate fresh table content (markdown).
# ---------------------------------------------------------------------------

generate_table() {
  local pkg consumers
  local total api operators cloud cli others

  echo "## Real Usage Matrix"
  echo ""
  echo "Tabela gerada via grep de \`import \"github.com/loopzero/loopzero/pkg/X\"\` em **$(date -u +%Y-%m-%d)**."
  echo "Atualizar via \`task pkg:usage-matrix:write\` (script: \`infra/scripts/pkg-usage-matrix.sh\`)."
  echo "CI gate \`task pkg:usage-matrix:check\` falha se a tabela estiver out-of-date (gate PKG-G14)."
  echo ""
  echo "| Pacote | Total imports | api/internal | operators/internal | cloud | cli | Outros |"
  echo "|---|---:|---:|---:|---:|---:|---:|"

  # List pkg subdirectories in stable order.
  for pkg_dir in "$ROOT/pkg"/*/; do
    pkg="$(basename "$pkg_dir")"
    [ "$pkg" = "tools" ] && continue # ignore future tooling subdir

    # Count imports excluding the package itself.
    total=$(cd "$ROOT" && grep -r "github.com/loopzero/loopzero/pkg/$pkg\"" \
      --include="*.go" -l 2>/dev/null \
      | grep -v "^pkg/$pkg/" \
      | wc -l | tr -d ' ')
    api=$(cd "$ROOT" && grep -r "github.com/loopzero/loopzero/pkg/$pkg\"" \
      --include="*.go" -l 2>/dev/null \
      | grep -c "^api/internal" || true)
    operators=$(cd "$ROOT" && grep -r "github.com/loopzero/loopzero/pkg/$pkg\"" \
      --include="*.go" -l 2>/dev/null \
      | grep -c "^operators/internal" || true)
    cloud=$(cd "$ROOT" && grep -r "github.com/loopzero/loopzero/pkg/$pkg\"" \
      --include="*.go" -l 2>/dev/null \
      | grep -c "^cloud/" || true)
    cli=$(cd "$ROOT" && grep -r "github.com/loopzero/loopzero/pkg/$pkg\"" \
      --include="*.go" -l 2>/dev/null \
      | grep -c "^cli/" || true)
    consumers=$(cd "$ROOT" && grep -r "github.com/loopzero/loopzero/pkg/$pkg\"" \
      --include="*.go" -l 2>/dev/null \
      | grep -v "^pkg/$pkg/" \
      | grep -vE "^(api/internal|operators/internal|cloud/|cli/)" \
      | wc -l | tr -d ' ')
    others="$consumers"

    # Marker for dead code (zero non-self consumers).
    local marker=""
    if [ "$total" = "0" ]; then
      marker=" ⚠️ **dead code candidate**"
    fi
    echo "| \`pkg/$pkg\` | $total$marker | $api | $operators | $cloud | $cli | $others |"
  done
}

# ---------------------------------------------------------------------------
# Splice generated content between markers.
# ---------------------------------------------------------------------------

splice_doc() {
  local table_content
  table_content="$(generate_table)"

  awk -v start="$START_MARK" -v end="$END_MARK" -v body="$table_content" '
    BEGIN { skip = 0 }
    {
      if (index($0, start) > 0) {
        print start
        print ""
        print body
        print ""
        print end
        skip = 1
        next
      }
      if (index($0, end) > 0) {
        skip = 0
        next
      }
      if (skip == 0) print
    }
  ' "$DOC"
}

# ---------------------------------------------------------------------------
# Mode dispatch.
# ---------------------------------------------------------------------------

if ! grep -q "$START_MARK" "$DOC"; then
  echo "ERROR: $DOC missing $START_MARK marker" >&2
  echo "Run --write once to insert markers (or add them manually)." >&2
  exit 3
fi
if ! grep -q "$END_MARK" "$DOC"; then
  echo "ERROR: $DOC missing $END_MARK marker" >&2
  exit 3
fi

case "$mode" in
  --write)
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    splice_doc > "$tmp"
    mv "$tmp" "$DOC"
    echo "Wrote refreshed Real Usage Matrix to $DOC"
    ;;
  --check)
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    splice_doc > "$tmp"
    if ! diff -q "$tmp" "$DOC" >/dev/null; then
      echo "ERROR: $DOC out-of-date — run \`task pkg:usage-matrix:write\` and commit." >&2
      diff -u "$DOC" "$tmp" >&2 || true
      exit 1
    fi
    echo "OK: pkg usage matrix is up-to-date."
    ;;
esac
