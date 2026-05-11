#!/usr/bin/env bash
# run-triangulation.sh — Orchestrates T3.2 (Triangulation Analysis) after T3.1 LIVE run.
#
# Reads 3 persona YAML outputs from docs/governance/scoring-YYYY-MM-DD/
# Invokes triangulate-scores.py
# Writes triangulation.yaml + summary report
#
# Per T3.2 of status-validation-and-doc-consolidation-plan.

set -euo pipefail

SCORING_DIR="${SCORING_DIR:-docs/governance/scoring-$(date +%Y-%m-%d)}"

if [[ ! -d "$SCORING_DIR" ]]; then
    echo "Scoring dir not found: $SCORING_DIR" >&2
    echo "Usage: SCORING_DIR=docs/governance/scoring-YYYY-MM-DD $(basename "$0")" >&2
    exit 1
fi

# Find the 3 persona outputs
P1="$SCORING_DIR/p1-vercel-dd.yaml"
P2="$SCORING_DIR/p2-hostile-vc.yaml"
P3="$SCORING_DIR/p3-paranoid-sre.yaml"

for f in "$P1" "$P2" "$P3"; do
    if [[ ! -f "$f" ]]; then
        echo "Missing persona output: $f" >&2
        echo "T3.1 LIVE run must produce all 3 files before T3.2 can run." >&2
        exit 1
    fi
done

echo "Triangulating: $P1 + $P2 + $P3"

# Run triangulator → write triangulation.yaml + raw stdout
python3 infra/scripts/governance/triangulate-scores.py "$P1" "$P2" "$P3" \
    > "$SCORING_DIR/triangulation.yaml"

TRIANG_EXIT=$?
echo "Triangulation exit: $TRIANG_EXIT"
echo "Output: $SCORING_DIR/triangulation.yaml"

# Generate provenance log
cat > "$SCORING_DIR/raw-runs.md" <<EOF
# Raw Runs Provenance — $(date +%Y-%m-%d)

Per T3.1+T3.2 of status-validation-and-doc-consolidation-plan.

## Persona invocations

| Persona | Model | Temp | Seed | Output file |
|---------|-------|-----:|-----:|-------------|
| P1 Vercel DD | claude-opus-4-7 | 0.7 | 42 | $(basename "$P1") |
| P2 Hostile VC | claude-sonnet-4-6 | 0.3 | 137 | $(basename "$P2") |
| P3 Paranoid SRE | claude-haiku-4-5 | 0.5 | 99 | $(basename "$P3") |

## Independence dimensions varied
- **Models:** 3 distinct (opus / sonnet / haiku) — fallback Claude-only family (codex unavailable in Ralph context)
- **Temperatures:** 3 distinct (0.7 / 0.3 / 0.5)
- **Seeds:** 3 distinct (42 / 137 / 99)

**Verification result:** $(USE_FALLBACK=true bash infra/scripts/governance/verify-independence.sh 2>&1 | tail -1)

## Confidence cap

Per ADR D1: 90-95% (fallback mode — Claude-only family). For 95-97% cap, cross-vendor codex required.

## Triangulation outputs

See \`triangulation.yaml\` for median + std-dev per dimension.

Flagged dims (std-dev > 0.3) require reconciliation meeting per T3.3.

EOF

echo "Provenance: $SCORING_DIR/raw-runs.md"

# Display summary
echo ""
echo "=== Triangulation Summary ==="
grep -E "^  (core_deploy_flow|dx_cli|infra_ops|security|scale_multitenancy|product_completeness|tests_confidence):|^    (median|std_dev|flagged|final_score):" "$SCORING_DIR/triangulation.yaml" | head -50
echo ""
grep "^flags_count:" "$SCORING_DIR/triangulation.yaml" || true
grep -A 10 "^flagged_dimensions:" "$SCORING_DIR/triangulation.yaml" || echo "No flagged dimensions"

exit $TRIANG_EXIT
