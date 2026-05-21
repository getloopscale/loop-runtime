#!/usr/bin/env bash
# audit-helm-drift.sh — detect drift between charts/ (published) and infra/helm/ (bootstrap)
#
# Per ADR-0041 (drift detector graduated advisory→blocking 2026-05-21 per
# T3.2 of plan 2026-05-21-deep-review-remediation) + CH-G16 NEW gate.
# Reports drifts that should be either:
#   (a) documented as intentional via ADR (e.g., loopzero-api dev minimal vs prod full)
#   (b) fixed as regression (e.g., loopzero-admission-policies pre-T1.2)
#
# Usage:
#   bash scripts/audit-helm-drift.sh                      # human-readable
#   bash scripts/audit-helm-drift.sh --output-format=github-annotations
#
# Exit codes (BLOCKING):
#   0 — no drifts OR all drifts have approving ADR
#   1 — drifts without ADR detected (FAIL CI)
#   2 — script error (yq missing, charts missing)
#
# Plan: charts-sota-implementation-plan T1.3
# Gate: CH-G16 (BLOCKING — graduated from advisory 2026-05-21 per T3.2).
# Consumer: .github/workflows/charts-drift-cron.yml exits 1 on this script's
# exit 1, opens GitHub issue with drift report, fails the workflow run.

set -euo pipefail

OUTPUT_FORMAT="${1:-human}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# Conceptual mapping: published chart → bootstrap chart (when names differ).
declare -A CONCEPTUAL_PAIRS=(
  ["loopzero-admission-policies"]="admission-policies"
)

# ADR registry — pre-approved drifts. Format: "published:bootstrap" → "ADR-NNNN".
declare -A APPROVED_DRIFTS=(
  # loopzero-api drift (12 vs 3 templates) — published full RBAC vs bootstrap minimal local-dev.
  ["loopzero-api:loopzero-api"]="ADR-0043"
  ["loopzero-cloud:loopzero-cloud"]="ADR-0043"
)

EXIT_CODE=0
DRIFT_REPORT_JSON='{"drifts":['
DRIFT_HUMAN=""
FIRST=true

emit_annotation() {
  local pair="$1"
  local message="$2"
  local severity="${3:-warning}"

  if [[ "$OUTPUT_FORMAT" == "github-annotations" ]]; then
    echo "::${severity}::Drift detected for ${pair}: ${message}"
  fi
}

emit_human() {
  local pair="$1"
  local body="$2"
  local intentional="${3:-false}"

  local marker="⚠"
  [[ "$intentional" == "true" ]] && marker="ℹ"

  DRIFT_HUMAN+="\n${marker} ${pair}\n${body}\n"
}

emit_json() {
  local pair="$1"
  local templates_only_published="$2"
  local templates_only_bootstrap="$3"
  local intentional="$4"
  local adr="${5:-null}"
  local severity="${6:-medium}"

  if [[ "$FIRST" == "true" ]]; then
    FIRST=false
  else
    DRIFT_REPORT_JSON+=","
  fi

  if [[ "$adr" != "null" ]]; then
    adr="\"${adr}\""
  fi

  DRIFT_REPORT_JSON+=$(cat <<EOF
{"pair":"${pair}","intentional":${intentional},"adr":${adr},"severity":"${severity}","templates_only_in_published":[${templates_only_published}],"templates_only_in_bootstrap":[${templates_only_bootstrap}]}
EOF
)
}

list_templates() {
  local dir="$1"
  if [[ -d "$dir" ]]; then
    find "$dir" -maxdepth 1 -type f \( -name "*.yaml" -o -name "*.yml" \) -printf "%f\n" | sort
  fi
}

audit_pair() {
  local published_name="$1"
  local bootstrap_name="$2"
  local pair_key="${published_name}:${bootstrap_name}"

  local published_dir="charts/${published_name}/templates"
  local bootstrap_dir="infra/helm/${bootstrap_name}/templates"

  # Skip if both don't exist.
  if [[ ! -d "$published_dir" ]] && [[ ! -d "$bootstrap_dir" ]]; then
    return
  fi

  local published_templates bootstrap_templates
  published_templates=$(list_templates "$published_dir")
  bootstrap_templates=$(list_templates "$bootstrap_dir")

  # Set diff: only-in-published vs only-in-bootstrap.
  local only_published only_bootstrap
  only_published=$(comm -23 <(echo "$published_templates") <(echo "$bootstrap_templates") | grep -v '^$' || true)
  only_bootstrap=$(comm -13 <(echo "$published_templates") <(echo "$bootstrap_templates") | grep -v '^$' || true)

  if [[ -z "$only_published" ]] && [[ -z "$only_bootstrap" ]]; then
    return  # No drift.
  fi

  local published_count bootstrap_count
  published_count=$(echo "$published_templates" | grep -c '^.\+$' || true)
  bootstrap_count=$(echo "$bootstrap_templates" | grep -c '^.\+$' || true)

  local intentional="false"
  local adr="${APPROVED_DRIFTS[$pair_key]:-}"
  local severity="medium"

  if [[ -n "$adr" ]]; then
    intentional="true"
    severity="info"
  else
    EXIT_CODE=1
    severity="high"
  fi

  # Build template lists for JSON.
  local only_published_json=""
  local only_bootstrap_json=""
  if [[ -n "$only_published" ]]; then
    only_published_json=$(echo "$only_published" | awk '{printf "%s\"%s\"", (NR>1?",":""), $0}')
  fi
  if [[ -n "$only_bootstrap" ]]; then
    only_bootstrap_json=$(echo "$only_bootstrap" | awk '{printf "%s\"%s\"", (NR>1?",":""), $0}')
  fi

  emit_json "$pair_key" "$only_published_json" "$only_bootstrap_json" "$intentional" "${adr:-null}" "$severity"

  local human_body=""
  human_body+="  published: $published_count templates / bootstrap: $bootstrap_count templates\n"
  if [[ -n "$only_published" ]]; then
    human_body+="  Only in published (charts/$published_name): $(echo "$only_published" | tr '\n' ' ')\n"
  fi
  if [[ -n "$only_bootstrap" ]]; then
    human_body+="  Only in bootstrap (infra/helm/$bootstrap_name): $(echo "$only_bootstrap" | tr '\n' ' ')\n"
  fi
  if [[ "$intentional" == "true" ]]; then
    human_body+="  STATUS: intentional (approved by $adr)"
  else
    human_body+="  STATUS: needs ADR or fix"
  fi
  emit_human "$pair_key" "$human_body" "$intentional"

  if [[ "$intentional" == "false" ]]; then
    emit_annotation "$pair_key" "templates differ; $published_count vs $bootstrap_count" "warning"
  fi
}

# Iterate over real overlaps (same name in both).
for chart_dir in charts/loopzero-*/; do
  chart_name=$(basename "$chart_dir")
  short_name="${chart_name#loopzero-}"

  if [[ -d "infra/helm/${chart_name}" ]]; then
    audit_pair "${chart_name}" "${chart_name}"
  fi
done

# Iterate over conceptual pairs (different names).
for published in "${!CONCEPTUAL_PAIRS[@]}"; do
  bootstrap="${CONCEPTUAL_PAIRS[$published]}"
  audit_pair "$published" "$bootstrap"
done

DRIFT_REPORT_JSON+="]}"

# Output
case "$OUTPUT_FORMAT" in
  json)
    echo "$DRIFT_REPORT_JSON"
    ;;
  github-annotations)
    # Annotations already emitted inline.
    if [[ "$EXIT_CODE" == "0" ]]; then
      echo "::notice::charts/ ↔ infra/helm/ drift audit: no unapproved drifts."
    fi
    ;;
  human|*)
    if [[ -z "$DRIFT_HUMAN" ]]; then
      echo "✓ charts/ ↔ infra/helm/ drift audit: no drifts detected."
    else
      echo "Drift audit report"
      echo "==================="
      echo -e "$DRIFT_HUMAN"
      if [[ "$EXIT_CODE" != "0" ]]; then
        echo
        echo "❌ Drifts without approving ADR detected. Classify each drift as intentional (ADR) or regression fix."
      else
        echo
        echo "✓ All drifts have approving ADRs (intentional)."
      fi
    fi
    ;;
esac

exit $EXIT_CODE
