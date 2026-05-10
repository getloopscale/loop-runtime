#!/usr/bin/env bash
#
# T5.10 of deep-review-remediation-plan (find_obs_003 MEDIUM).
#
# Lints PrometheusRule files for `vector(0) > 1` patterns — alerts staged
# without their underlying metric. These create false-confidence: operator
# sees "we have an alert" but the alert can NEVER fire because the metric
# isn't emitted.
#
# Existing 16 orphan alerts (find_obs_003 baseline) are tracked in
# EXCEPTION_LIST below. New PRs adding `vector(0) > 1` without entry in
# EXCEPTION_LIST will fail. Each exception MUST have a comment explaining
# WHY (deferred metric emission with linked work item).
#
# Exit 0: no new orphan alerts.
# Exit 1: orphan alert added without exception entry.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"
RULES_DIR="$REPO_ROOT/infra/helm/monitoring/manifests"

# EXCEPTION_LIST — alert names that are KNOWN orphans.
#
# T3.1 of deep-review-2026-05-10-remediation-plan removed all 16 baseline
# orphan alerts from the manifests. List is now EMPTY: any new
# `vector(0) > 1` alert is a violation. If a future need legitimately
# requires a placeholder, add it here WITH a comment + finding ID.
EXCEPTION_LIST=(
)

violations=0

while IFS= read -r line_with_file; do
    file="${line_with_file%%:*}"
    rest="${line_with_file#*:}"
    lineno="${rest%%:*}"

    # Find the alert: name preceding this expr line. Look up to 5 lines
    # before for "alert: NAME" pattern.
    start=$((lineno - 5))
    if [ "$start" -lt 1 ]; then start=1; fi
    alert_name="$(sed -n "${start},${lineno}p" "$file" | grep -oE 'alert: [A-Za-z0-9_]+' | tail -1 | sed 's/alert: //')"

    if [ -z "$alert_name" ]; then
        echo "WARN: could not determine alert name for vector(0) > 1 at $file:$lineno"
        violations=$((violations + 1))
        continue
    fi

    # Check exception list
    excepted=0
    for known in "${EXCEPTION_LIST[@]}"; do
        if [ "$alert_name" = "$known" ]; then
            excepted=1
            break
        fi
    done

    if [ "$excepted" -eq 0 ]; then
        echo "ORPHAN-ALERT VIOLATION: $file:$lineno — alert $alert_name uses vector(0) > 1 (metric not emitted) without entry in EXCEPTION_LIST"
        violations=$((violations + 1))
    fi
done < <(grep -nE 'expr:\s*vector\(0\)\s*>\s*1' "$RULES_DIR"/*.yaml 2>/dev/null || true)

if [ "$violations" -eq 0 ]; then
    echo "PASS — no orphan alerts (T3.1 of deep-review-2026-05-10 cleared the baseline)."
    exit 0
fi

echo ""
echo "FAIL — $violations new orphan alert(s)."
echo ""
echo "Either:"
echo "  (a) Wire the real metric and rewrite the alert expression against it."
echo "  (b) Add the alert name to EXCEPTION_LIST in this script with an"
echo "      explanatory comment + linked work item (governance gate)."
echo ""
echo "Long-term goal: reduce EXCEPTION_LIST to zero by emitting the deferred metrics."
exit 1
