#!/usr/bin/env bash
# report.sh — Generates structured audit report for a chaos test run.
#
# Pulls hypothesis + slo-query + dod-test annotations from manifest,
# combines with baseline + during + post measurements, produces Markdown.
#
# Usage:
#   bash infra/scripts/test/report.sh \
#     --scenario C01 \
#     --manifest infra/tests/chaos/manifests/pod-kill-api-leader.yaml \
#     --run-dir /tmp/chaos-C01-2026... \
#     --verdict PASS \
#     --pre-p95 10 --during-p95 12 --post-p95 11 \
#     --pre-success 100 --during-success 100 --post-success 100 \
#     --restart-delta 0 \
#     --reasons "" \
#     --output /tmp/chaos-C01-2026.../report.md

set -euo pipefail

# T2.2-followup (plan 2026-05-21-deep-review-remediation): adopt shared
# dry-run lib. chaos/report.sh writes a single Markdown report file to
# `$OUTPUT`; with --dry-run it prints the report to stdout WITHOUT writing
# the file, useful for operator template inspection during chaos
# scenario development.
#
# Bug fix (caught during T2.2-followup audit): previous code had a
# redundant `set -uo pipefail` immediately after `set -euo pipefail`.
# The redundant call OMITS `-e` and therefore DISABLES error-fail
# semantics. Removed in this commit — failures in report metadata
# extraction now propagate correctly.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/dry-run.sh disable=SC1091
source "$SCRIPT_DIR/../lib/dry-run.sh"
parse_dry_run_flag "$@"
if [ "${#REMAINING_ARGS[@]}" -gt 0 ]; then
  set -- "${REMAINING_ARGS[@]}"
else
  set --
fi

SCENARIO="" MANIFEST="" RUN_DIR="" VERDICT="" OUTPUT=""
PRE_P95=0 DURING_P95=0 POST_P95=0
PRE_SUCCESS=0 DURING_SUCCESS=0 POST_SUCCESS=0
RESTART_DELTA=0
REASONS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scenario) SCENARIO="$2"; shift 2 ;;
    --manifest) MANIFEST="$2"; shift 2 ;;
    --run-dir) RUN_DIR="$2"; shift 2 ;;
    --verdict) VERDICT="$2"; shift 2 ;;
    --pre-p95) PRE_P95="$2"; shift 2 ;;
    --during-p95) DURING_P95="$2"; shift 2 ;;
    --post-p95) POST_P95="$2"; shift 2 ;;
    --pre-success) PRE_SUCCESS="$2"; shift 2 ;;
    --during-success) DURING_SUCCESS="$2"; shift 2 ;;
    --post-success) POST_SUCCESS="$2"; shift 2 ;;
    --restart-delta) RESTART_DELTA="$2"; shift 2 ;;
    --reasons) REASONS="$2"; shift 2 ;;
    --output) OUTPUT="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

[[ -n "$SCENARIO" && -n "$MANIFEST" && -n "$RUN_DIR" && -n "$VERDICT" && -n "$OUTPUT" ]] || {
  echo "ERROR: missing required arg" >&2
  exit 1
}

# In dry-run mode, redirect $OUTPUT to /dev/stdout so the report is emitted
# without writing to disk. Existing destination preserved for normal runs.
ORIGINAL_OUTPUT="$OUTPUT"
if [ "${DRY_RUN:-0}" = "1" ]; then
  dry_run_log "would write chaos report to $OUTPUT — printing to stdout instead"
  OUTPUT="/dev/stdout"
fi

# Extract metadata from manifest
HYPOTHESIS=$(grep "runtime.getloopscale.com/hypothesis:" "$MANIFEST" | head -1 | sed 's/.*runtime.getloopscale.com\/hypothesis: *//;s/^"//;s/"$//' || echo "(missing)")
DOD_TEST=$(grep "runtime.getloopscale.com/dod-test:" "$MANIFEST" | head -1 | sed 's/.*runtime.getloopscale.com\/dod-test: *//;s/^"//;s/"$//' || echo "(missing)")
CHAOS_KIND=$(grep "^kind:" "$MANIFEST" | head -1 | awk '{print $2}')
CHAOS_ACTION=$(grep "^  action:" "$MANIFEST" | head -1 | sed 's/.*action: *//' || echo "(n/a)")
DURATION=$(grep "^  duration:" "$MANIFEST" | head -1 | sed 's/.*duration: *//;s/"//g' || echo "(n/a)")

# Extract slo-query (multi-line YAML scalar)
SLO_QUERY=$(awk '/runtime.getloopscale.com\/slo-query:/{flag=1; next} flag && /^      [a-zA-Z#=]/{print; next} flag && !/^      /{flag=0}' "$MANIFEST" | sed 's/^      //' | head -20)

cat > "$OUTPUT" <<EOF
# Chaos Test Report — $SCENARIO

**Generated:** $(date -u +%Y-%m-%dT%H:%M:%SZ)
**Verdict:** **$VERDICT**

## Scenario metadata

| Field | Value |
|-------|-------|
| Scenario ID | $SCENARIO |
| Manifest | \`$MANIFEST\` |
| Chaos kind | $CHAOS_KIND |
| Action | $CHAOS_ACTION |
| Duration | $DURATION |
| Run directory | \`$RUN_DIR/\` |

## Hypothesis (from manifest)

> $HYPOTHESIS

## DoD test criteria (from manifest)

> $DOD_TEST

## SLO query (from manifest)

\`\`\`promql
$SLO_QUERY
\`\`\`

## Measurements

### API health (success rate %)

| Phase | Success rate |
|-------|--------------|
| Pre-chaos (baseline) | **${PRE_SUCCESS}%** |
| During chaos | **${DURING_SUCCESS}%** |
| Post-recovery | **${POST_SUCCESS}%** |

### API latency P95 (ms)

| Phase | P95 latency |
|-------|-------------|
| Pre-chaos (baseline) | **${PRE_P95}ms** |
| During chaos | **${DURING_P95}ms** (Δ +$((DURING_P95 - PRE_P95))ms from baseline) |
| Post-recovery | **${POST_P95}ms** (Δ +$((POST_P95 - PRE_P95))ms from baseline) |

### Pod restart deltas

| Pod | Pre-chaos count | Post-recovery count | Delta |
|-----|----------------|---------------------|-------|
| loop-runtime-api | (see baseline.json) | (see post.json) | **$RESTART_DELTA** |

## Verdict reasoning

EOF

if [[ "$VERDICT" == "PASS" ]]; then
  cat >> "$OUTPUT" <<'EOF'
**PASS criteria all met:**
- API success rate during chaos held above 90%
- P95 latency degradation during chaos within 500ms tolerance
- Post-recovery latency drift within 100ms of baseline
- Pod restart delta within 2-restart tolerance
EOF
else
  cat >> "$OUTPUT" <<EOF
**FAIL — criteria violated:**

EOF
  echo "$REASONS" | tr '|' '\n' | sed 's/^/- /' >> "$OUTPUT"
fi

cat >> "$OUTPUT" <<EOF

## Evidence files

- \`baseline.json\` — pre-chaos steady-state metrics
- \`during.json\` — metrics captured during chaos window
- \`post.json\` — post-recovery validation metrics
- \`inject.log\` — kubectl apply output
- \`report.md\` — this file

## Verdict criteria (from /chaos-test SKILL)

### PASS
- SLO held during entire chaos window
- Recovery within budget (default 5min)
- Zero data loss
- Pod restart count delta matches expected
- No cascading failures

### FAIL
- SLO violated during chaos
- Recovery exceeded budget
- Data loss detected
- Unexpected pod restarts
- Cascading failure

### INCONCLUSIVE
- Baseline not captured
- Chaos resource never entered \`Running\` phase
- Cleanup failed
EOF

echo "$ORIGINAL_OUTPUT"
