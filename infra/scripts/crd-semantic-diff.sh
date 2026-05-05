#!/usr/bin/env bash
# crd-semantic-diff.sh — semantic diff between operator-source CRDs and chart templates.
#
# Compares `operators/config/crd/bases/loopzero.io_<X>.yaml` (kubebuilder source)
# against `charts/loopzero-crds/templates/<X>.yaml` (Helm template, post-render).
# Detects mismatches in `.spec` (group, scope, versions[].schema, etc) that
# the existing crd-sync nominal check misses.
#
# Plan: charts-sota-implementation-plan T1.4
# Gate: CH-G06 (PARTIAL → MET)
#
# Usage:
#   bash scripts/crd-semantic-diff.sh                    # scan all CRDs
#   bash scripts/crd-semantic-diff.sh --crd <name>        # single CRD (e.g. tenants)
#
# Exit:
#   0 — all CRDs in sync (semantic spec match)
#   1 — drift detected
#   2 — script error (missing yq, missing helm)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

if ! command -v yq >/dev/null 2>&1; then
  echo "::error::yq is required but not installed"
  exit 2
fi

if ! command -v helm >/dev/null 2>&1; then
  echo "::error::helm is required but not installed"
  exit 2
fi

DRIFT_COUNT=0
SCAN_COUNT=0
CRD_FILTER="${1:-}"
[[ "$CRD_FILTER" == "--crd" ]] && CRD_FILTER="${2:-}"

# Render the chart once; extract individual CRD specs by name from rendered output.
RENDER_TMP=$(mktemp)
trap "rm -f $RENDER_TMP" EXIT
helm template charts/loopzero-crds > "$RENDER_TMP" 2>/dev/null

normalize_spec() {
  # Read .spec content from stdin (caller already extracted via `yq eval '.spec'`).
  # Strip controller-gen build-tool noise that doesn't affect API semantic.
  yq eval 'del(.. | select(has("controller-gen.kubebuilder.io/version")))' -
}

extract_chart_crd() {
  # Given $1 = CRD name (e.g. tenants.loopzero.io), extract its document from rendered output.
  local name="$1"
  yq eval-all "select(.kind == \"CustomResourceDefinition\" and .metadata.name == \"$name\")" "$RENDER_TMP"
}

audit_crd() {
  local source_file="$1"
  local crd_name short_name
  crd_name=$(yq eval '.metadata.name' "$source_file")
  short_name=$(basename "$source_file" .yaml | sed 's/loopzero.io_//')

  if [[ -n "$CRD_FILTER" ]] && [[ "$short_name" != "$CRD_FILTER" ]]; then
    return
  fi

  SCAN_COUNT=$((SCAN_COUNT + 1))

  local source_spec chart_spec
  source_spec=$(yq eval '.spec' "$source_file" | normalize_spec)

  local chart_yaml
  chart_yaml=$(extract_chart_crd "$crd_name")
  if [[ -z "$chart_yaml" ]]; then
    echo "::error::Chart template missing CRD $crd_name"
    DRIFT_COUNT=$((DRIFT_COUNT + 1))
    return
  fi

  chart_spec=$(echo "$chart_yaml" | yq eval '.spec' - | normalize_spec)

  if [[ "$source_spec" == "$chart_spec" ]]; then
    echo "  ✓ $short_name ($crd_name)"
    return
  fi

  echo "  ✗ $short_name ($crd_name) — semantic drift detected"
  echo "    Reproduce: bash scripts/crd-semantic-diff.sh --crd $short_name"
  DRIFT_COUNT=$((DRIFT_COUNT + 1))

  if [[ "${VERBOSE:-0}" == "1" ]]; then
    diff <(echo "$source_spec") <(echo "$chart_spec") | head -30 || true
  fi
}

echo "CRD semantic diff (operators/config/crd/bases ↔ charts/loopzero-crds/templates)"
echo "==========================================================================="

for src in operators/config/crd/bases/loopzero.io_*.yaml; do
  audit_crd "$src"
done

echo
echo "Scanned: $SCAN_COUNT CRDs"

if [[ "$DRIFT_COUNT" -gt 0 ]]; then
  echo "::error::Semantic drift in $DRIFT_COUNT CRD(s). Run with VERBOSE=1 for diff details."
  echo "Fix: regenerate chart from kubebuilder via 'task helm:sync-crds'"
  exit 1
fi

echo "✓ All $SCAN_COUNT CRDs are in semantic sync."
exit 0
