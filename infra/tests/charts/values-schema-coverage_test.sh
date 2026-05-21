#!/usr/bin/env bash
# Hermetic gate: every chart dir under infra/helm/ + charts/ MUST have
# values.schema.json. Exceptions: library charts (loopzero-common).
#
# Plan 2026-05-21-deep-review-remediation T3.1 + ADR D2.
#
# Background: deep review v2 found 18 of 34 infra/helm/ charts (53%)
# lacking values.schema.json. helm lint --strict cannot validate values
# without a schema, so dev mistakes (wrong key, wrong type) ship to
# rendered manifests undetected. T3.1 generated permissive minimal
# schemas for all 16 (after T1.3 cleanup); this gate enforces no
# regression.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }
info() { echo "[schema-coverage] $*"; }

# Exempt library charts (no values.schema.json required)
EXEMPT_CHARTS=("loopzero-common")

is_exempt() {
  local name="$1"
  for ex in "${EXEMPT_CHARTS[@]}"; do
    [ "$ex" = "$name" ] && return 0
  done
  return 1
}

check_chart_dir() {
  local d="$1"
  [ -d "$d" ] || return 0
  local name
  name=$(basename "$d")
  if is_exempt "$name"; then
    return 0
  fi
  if [ ! -f "$d/values.schema.json" ]; then
    fail "$d missing values.schema.json"
    return 1
  fi
  # Validate JSON
  if ! python3 -c "import json; json.load(open('$d/values.schema.json'))" 2>/dev/null; then
    fail "$d/values.schema.json is not valid JSON"
    return 1
  fi
  return 0
}

INFRA_TOTAL=0
INFRA_PASS=0
for d in infra/helm/*/; do
  [ -d "$d" ] || continue
  INFRA_TOTAL=$((INFRA_TOTAL + 1))
  if check_chart_dir "$d"; then
    INFRA_PASS=$((INFRA_PASS + 1))
  fi
done

CHARTS_TOTAL=0
CHARTS_PASS=0
for d in charts/*/; do
  [ -d "$d" ] || continue
  CHARTS_TOTAL=$((CHARTS_TOTAL + 1))
  if check_chart_dir "$d"; then
    CHARTS_PASS=$((CHARTS_PASS + 1))
  fi
done

info "infra/helm/: $INFRA_PASS / $INFRA_TOTAL charts have valid values.schema.json"
info "charts/: $CHARTS_PASS / $CHARTS_TOTAL charts have valid values.schema.json"

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[schema-coverage] FAIL: $VIOLATIONS chart(s) without schema" >&2
  exit 1
fi

echo "[schema-coverage] PASS — all charts have values.schema.json (except library chart loopzero-common)"
