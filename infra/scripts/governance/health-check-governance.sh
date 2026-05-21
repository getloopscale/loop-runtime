#!/usr/bin/env bash
# health-check-governance.sh — Aggregator pre-flight check for the governance system.
#
# Part of pre-Dogfood-Gate checks. Validates that governance scripts + workflows +
# evidence files are all healthy and consistent. Does NOT require LIVE infrastructure.
#
# Usage:
#   task governance:health
#   bash infra/scripts/governance/health-check-governance.sh
#
# Exit codes:
#   0 — all checks PASS
#   1 — one or more checks FAILED

set -euo pipefail

set -u

PASS=0
FAIL=0
PASS_NAMES=()
FAIL_NAMES=()

check() {
    local name="$1"
    shift
    if "$@" > /dev/null 2>&1; then
        PASS=$((PASS + 1))
        PASS_NAMES+=("$name")
        printf "  \033[32m✓\033[0m %s\n" "$name"
    else
        FAIL=$((FAIL + 1))
        FAIL_NAMES+=("$name")
        printf "  \033[31m✗\033[0m %s\n" "$name"
    fi
}

check_with_output() {
    local name="$1"
    shift
    local output
    if output=$("$@" 2>&1); then
        PASS=$((PASS + 1))
        PASS_NAMES+=("$name")
        printf "  \033[32m✓\033[0m %s\n" "$name"
    else
        FAIL=$((FAIL + 1))
        FAIL_NAMES+=("$name")
        printf "  \033[31m✗\033[0m %s\n" "$name"
        printf "    output: %s\n" "$(echo "$output" | head -3 | tr '\n' ' ')"
    fi
}

echo "═══════════════════════════════════════════════════════════"
echo "  Governance System Health Check"
echo "  $(date +%Y-%m-%d\ %H:%M:%S)"
echo "═══════════════════════════════════════════════════════════"

echo ""
echo "1. Script syntax (bash + python AST)"
for f in infra/scripts/governance/*.sh; do
    check "syntax $(basename "$f")" bash -n "$f"
done
for f in infra/scripts/governance/*.py; do
    check "syntax $(basename "$f")" python3 -c "import ast; ast.parse(open('$f').read())"
done

echo "2. The canonical docs exist"
# NOTE: docs/STATUS.md was removed in 2026-05-11 governance consolidation.
# Canonical maturity document is now .claude/knowledge-base/audits/2026-05-11/SUMMARY.md.
for doc in \
    ".claude/knowledge-base/audits/2026-05-11/SUMMARY.md" \
    "docs/GOVERNANCE.md" \
    "docs/architecture/governance/README.md" \
    "docs/meetings/adr/0051-claim-evidence-decay-contract.md"
do
    check "canonical $doc" test -f "$doc"
done

echo ""
echo "2b. Data artifacts (YAML + history)"
for f in \
    "docs/governance/claim-catalog.yaml" \
    "docs/governance/personas/independence-config.yaml" \
    "docs/governance/.markdownlint.json" \
    "docs/maturity-history/2026-05-03-snapshot.md" \
    "docs/maturity-assessment.md"
do
    check "data $f" test -f "$f"
done

echo ""
echo "2c. Persona prompts + scoring templates (in infra/scripts/governance/)"
for f in \
    "infra/scripts/governance/prompts/vercel-dd-engineer.md" \
    "infra/scripts/governance/prompts/hostile-vc-auditor.md" \
    "infra/scripts/governance/prompts/paranoid-sre.md" \
    "infra/scripts/governance/templates/reconciliation-meeting.template.md"
do
    check "asset $f" test -f "$f"
done

echo ""
echo "3. Architecture diagrams exist (PlantUML + Excalidraw, AS-IS + Mode 3)"
for diag in \
    "docs/architecture/governance/c4-context.puml" \
    "docs/architecture/governance/c4-container.puml" \
    "docs/architecture/governance/c4-component.puml" \
    "docs/architecture/governance/concept-bias-taxonomy.excalidraw" \
    "docs/architecture/governance/concept-decay-lifecycle.excalidraw" \
    "docs/architecture/governance/concept-evidence-triangulation-pipeline.excalidraw" \
    "docs/architecture/governance/concept-status-md-information-architecture.excalidraw" \
    "docs/architecture/governance/concept-independence-matrix.excalidraw" \
    "docs/architecture/governance/diff/c4-context.puml" \
    "docs/architecture/governance/diff/c4-container.puml" \
    "docs/architecture/governance/diff/c4-component.puml"
do
    check "diagram $(basename "$diag")" test -f "$diag"
done

echo ""
echo "4. CI workflows exist + valid YAML"
for wf in \
    ".github/workflows/decay-scan.yml" \
    ".github/workflows/status-refresh.yml" \
    ".github/workflows/cross-validation-weekly.yml" \
    ".github/workflows/adversarial-spotcheck.yml" \
    ".github/workflows/status-pr-gate.yml" \
    ".github/actions/governance-notify/action.yml"
do
    check "exists $(basename "$wf")" test -f "$wf"
    check "yaml-valid $(basename "$wf")" python3 -c "import yaml; yaml.safe_load(open('$wf'))"
done

echo ""
echo "5. Excalidraw JSON validity"
for ex in docs/architecture/governance/concept-*.excalidraw; do
    check "json-valid $(basename "$ex")" python3 -c "import json; json.load(open('$ex'))"
done

echo ""
echo "6. Evidence fixtures + real evidence file pass validator"
check_with_output "fixture valid-three-sources" bash infra/scripts/governance/validate-evidence.sh docs/evidence/_fixtures/valid-three-sources.md
check_with_output "real evidence: validate-evidence-script" bash infra/scripts/governance/validate-evidence.sh docs/evidence/tests-confidence/2026-05-11-validate-evidence-script.md

echo ""
echo "7. Independence config passes verification"
check_with_output "independence verified" bash infra/scripts/governance/verify-independence.sh

echo ""
echo "8. Phase 3 LIVE scoring outputs exist"
for f in \
    "docs/governance/scoring-2026-05-11/p1-vercel-dd.yaml" \
    "docs/governance/scoring-2026-05-11/p2-hostile-vc.yaml" \
    "docs/governance/scoring-2026-05-11/p3-paranoid-sre.yaml" \
    "docs/governance/scoring-2026-05-11/triangulation.yaml" \
    "docs/governance/scoring-2026-05-11/raw-runs.md"
do
    check "scoring $(basename "$f")" test -f "$f"
done

echo ""
echo "9. 7 scorecards generated"
for dim in core-deploy-flow dx-cli infra-ops security scale-multitenancy product-completeness tests-confidence; do
    check "scorecard $dim" test -f "docs/governance/scoring-2026-05-11/scorecard-$dim.md"
done

echo ""
echo "10. 4 reconciliation records (flagged dims)"
for dim in dx-cli security product-completeness tests-confidence; do
    check "reconciliation $dim" test -f "docs/governance/scoring-2026-05-11/reconciliation-$dim.md"
done

echo ""
echo "11. Taskfile wiring (validate / decay / bias / cross-validate)"
check "task validate:evidence wired" grep -q "validate:evidence:" taskfiles/test.yml
check "task decay:scan wired" grep -q "decay:scan:" taskfiles/test.yml
check "task bias:check wired" grep -q "bias:check:" taskfiles/test.yml
check "task cross-validate:plans wired" grep -q "cross-validate:plans" taskfiles/test.yml
check "task decay:scan:catalog wired" grep -q "decay:scan:catalog" taskfiles/test.yml

echo ""
echo "12. CLAUDE.md Rule 11 + redirects present"
check "CLAUDE.md Rule 11 exists" grep -q "11\. \*\*Claims need evidence\*\*" CLAUDE.md
check "CLAUDE.md references audit SUMMARY" grep -q ".claude/knowledge-base/audits/2026-05-11/SUMMARY.md" CLAUDE.md
check "maturity-assessment.md is redirect" grep -q "This file is now a redirect" docs/maturity-assessment.md

echo ""
echo "13. Markdownlint clean (governance docs)"
check ".markdownlint.json exists" test -f docs/governance/.markdownlint.json
if command -v npx >/dev/null 2>&1; then
    check "markdownlint zero errors" npx -y markdownlint-cli2 "docs/governance/**/*.md"
else
    echo "  (npx not available — skipping markdownlint)"
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
TOTAL=$((PASS + FAIL))
echo "  Result: $PASS / $TOTAL checks PASS"
if [[ $FAIL -gt 0 ]]; then
    echo ""
    echo "  Failed checks:"
    for name in "${FAIL_NAMES[@]}"; do
        echo "    - $name"
    done
fi
echo "═══════════════════════════════════════════════════════════"

exit $((FAIL > 0 ? 1 : 0))
