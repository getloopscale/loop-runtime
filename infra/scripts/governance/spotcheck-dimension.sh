#!/usr/bin/env bash
# spotcheck-dimension.sh — Pick 1 random dimension + 1 random persona; flag if delta from audit SUMMARY.md > 0.3.
#
# Per T5.3 of status-validation-and-doc-consolidation-plan.
#
# Selection is weighted by ttl_remaining (closer-to-expiry = higher prob).
# Reproducible via SPOTCHECK_SEED env var.

set -euo pipefail

SEED="${SPOTCHECK_SEED:-$(date +%s)}"
# NOTE: docs/STATUS.md was removed in 2026-05-11 governance consolidation.
# Canonical maturity/status document is now .claude/knowledge-base/audits/2026-05-11/SUMMARY.md.
STATUS_FILE="${STATUS_FILE:-.claude/knowledge-base/audits/2026-05-11/SUMMARY.md}"
CATALOG_FILE="${CATALOG_FILE:-docs/governance/claim-catalog.yaml}"

DIMENSIONS=("core-deploy" "dx-cli" "infra-ops" "security" "scale-mt" "product-completeness" "tests-confidence")
PERSONAS=("p1_vercel_dd" "p2_hostile_vc" "p3_paranoid_sre")

# Use seed to make selection deterministic
RANDOM=$SEED

# Weight by ttl_remaining if catalog available
# For now: uniform random (improvement TODO when catalog scoring exists)
DIM_IDX=$((RANDOM % ${#DIMENSIONS[@]}))
PERSONA_IDX=$((RANDOM % ${#PERSONAS[@]}))

SELECTED_DIM="${DIMENSIONS[$DIM_IDX]}"
SELECTED_PERSONA="${PERSONAS[$PERSONA_IDX]}"

echo "spotcheck_seed: $SEED"
echo "selected_dimension: $SELECTED_DIM"
echo "selected_persona: $SELECTED_PERSONA"
echo "selection_method: seeded_uniform (improvement: weight by ttl_remaining)"

# Output JSON for CI consumption
cat <<JSON
{
  "seed": $SEED,
  "dimension": "$SELECTED_DIM",
  "persona": "$SELECTED_PERSONA",
  "selection_method": "seeded_uniform",
  "next_step": "Invoke persona agent with this dimension; compare against .claude/knowledge-base/audits/2026-05-11/SUMMARY.md; alert if delta > 0.3"
}
JSON
