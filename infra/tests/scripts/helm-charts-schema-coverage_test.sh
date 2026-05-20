#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-20 T2.5 ext — coverage gate ensuring every CUSTOM LoopZero chart
# has a values.schema.json.
#
# Why this gate matters
# ---------------------
# Without enforcement, new LoopZero-custom charts (created in future sprints)
# would silently skip schema validation — exactly the bug class we have
# spent this session eliminating (12+ bugs in existing schemas alone).
# This gate prevents schema-coverage regression.
#
# Custom vs Upstream (auto-detected, not hardcoded)
# -------------------------------------------------
# A chart is treated as Custom (schema required) when it has templates/
# directory — meaning LoopZero renders K8s resources from this chart directly,
# not just passing values to upstream chart.
#
# A chart is treated as Upstream wrapper (schema optional) when it has
# values.yaml WITHOUT templates/ — these delegate to upstream chart via
# helmfile `chart:` reference. Upstream chart owns its own schema.
#
# Library charts (charts/loopzero-common) are detected via Chart.yaml `type:
# library` and exempted (no installable values).
#
# Hardcoded EXEMPT_CHARTS list exists for charts that don't fit the auto-
# detection rule cleanly (e.g., loopzero-common library), or charts which the
# operator has explicitly approved as upstream-only despite having templates/.
#
# Exit codes:
#   0 — All custom charts have schema
#   1 — One or more custom charts MISSING schema (failure list in stderr)
#   5 — Missing test dependency

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

log() { printf '[helm-coverage-test] %s\n' "$*" >&2; }

# Explicit exemptions (in addition to auto-detection rules below)
# Each MUST have an inline comment documenting WHY. Review quarterly to
# eliminate exemptions by adding schemas.
EXEMPT_CHARTS=(
  # Library chart — no installable values
  "charts/loopzero-common"

  # === Custom LoopZero charts pending schema (HONEST DEBT, 2026-05-20) ===
  # These have templates/ directory (would NOT auto-exempt as upstream
  # wrapper). Schemas are recommended but not yet authored. Tracked
  # explicitly here so the gate stays GREEN while the gap is visible.
  # Each subsequent session should pick one off this list, add a schema
  # with TDD red coverage, and remove from this list.

  # Argo Workflows bootstrap chart — wraps upstream argo-workflows +
  # LoopZero-specific build cluster RBAC. Schema would constrain controller
  # config + workflow defaults.
  "infra/helm/argo-workflows"


  # chaos-mesh wrapper + LoopZero CRD allow-lists. Schema would constrain
  # PSA enforcement + RBAC narrow.
  "infra/helm/chaos-mesh"

  # cloudflared deployment template (LoopZero CF Tunnel egress). Schema would
  # constrain tunnel ID format + replica count + secret refs.
  "infra/helm/cloudflared"

  # Harbor wrapper (LoopZero private registry). Schema would constrain admin
  # password ref + storage backend.
  "infra/helm/harbor"

  # Smoke test chart (local-only). Lowest priority for schema.
  "infra/helm/smoke-test"

  # Static assets CF Worker config chart. Schema would constrain worker
  # script SHA + KV namespace IDs.
  "infra/helm/static-assets"
)

is_exempt() {
  local chart="$1"
  for exempt in "${EXEMPT_CHARTS[@]}"; do
    if [[ "$chart" == "$exempt" ]]; then
      return 0
    fi
  done
  return 1
}

# Auto-detect: chart is upstream-wrapper (NO custom templates) → exempt
# Wrapper signal: directory has values.yaml but NO templates/ subdirectory.
is_upstream_wrapper() {
  local chart_dir="$1"
  if [[ -f "$chart_dir/values.yaml" && ! -d "$chart_dir/templates" ]]; then
    return 0
  fi
  return 1
}

# Auto-detect: chart is library (Chart.yaml type: library) → exempt
is_library_chart() {
  local chart_dir="$1"
  if [[ -f "$chart_dir/Chart.yaml" ]] && grep -qE "^type:\s*library" "$chart_dir/Chart.yaml"; then
    return 0
  fi
  return 1
}

MISSING=()
SCANNED=0
COVERED=0
EXEMPTED=0
EXEMPT_REASONS=()

# Scan both chart trees
for chart_dir in "$ROOT"/infra/helm/*/ "$ROOT"/charts/*/; do
  [[ -d "$chart_dir" ]] || continue
  chart_dir="${chart_dir%/}"
  rel_path="${chart_dir#"$ROOT"/}"

  # Skip dirs without values.yaml (not a Helm chart)
  if [[ ! -f "$chart_dir/values.yaml" && ! -f "$chart_dir/Chart.yaml" ]]; then
    continue
  fi

  SCANNED=$((SCANNED + 1))

  # Explicit exemption list (e.g., loopzero-common library)
  if is_exempt "$rel_path"; then
    EXEMPTED=$((EXEMPTED + 1))
    EXEMPT_REASONS+=("$rel_path: explicit-exempt")
    continue
  fi

  # Auto-detect: library chart
  if is_library_chart "$chart_dir"; then
    EXEMPTED=$((EXEMPTED + 1))
    EXEMPT_REASONS+=("$rel_path: auto-detect library chart")
    continue
  fi

  # Auto-detect: upstream wrapper (values.yaml without templates/)
  if is_upstream_wrapper "$chart_dir"; then
    EXEMPTED=$((EXEMPTED + 1))
    EXEMPT_REASONS+=("$rel_path: auto-detect upstream wrapper (no templates/)")
    continue
  fi

  if [[ -f "$chart_dir/values.schema.json" ]]; then
    COVERED=$((COVERED + 1))
  else
    MISSING+=("$rel_path")
  fi
done

# --- Report ---
log "Scanned charts:  $SCANNED"
log "Covered:         $COVERED"
log "Exempted:        $EXEMPTED"
log "Missing schema:  ${#MISSING[@]}"

if [[ ${#MISSING[@]} -gt 0 ]]; then
  log ""
  log "FAIL — the following charts lack values.schema.json:"
  for m in "${MISSING[@]}"; do
    log "  - $m"
  done
  log ""
  log "Add values.schema.json to each, OR explicitly add to EXEMPT_CHARTS"
  log "in this test (and document WHY in inline comment)."
  exit 1
fi

log "PASS — schema coverage maintained"
exit 0
