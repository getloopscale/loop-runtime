#!/usr/bin/env bash
#
# T5.8 of deep-review-remediation-plan (closes find_obs_005 MEDIUM).
#
# Lints PrometheusRule files for alerts missing a runbook_url annotation.
# Without runbook_url, Alertmanager cannot auto-link operators to recovery
# documentation. The non-standard `runbook:` annotation (legacy) is also
# accepted to allow gradual migration.
#
# 14 existing alerts without ANY runbook annotation are catalogued in
# EXCEPTION_LIST below. New PRs adding alerts without runbook fail CI.
#
# Exit 0: every alert has runbook (or is in EXCEPTION_LIST).
# Exit 1: new alert added without runbook.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"
RULES_DIR="$REPO_ROOT/infra/helm/monitoring/manifests"

# EXCEPTION_LIST — pre-T5.8-baseline alerts without runbook. Each entry
# is the alert name. Long-term goal: shrink this list to zero by adding
# runbooks for legacy alerts.
EXCEPTION_LIST=(
    "PulumiOrphanStackDetected"
    "GiteaAuthLatencyHigh"
    "ArgoWorkflowQuotaExceeded"
    "HPAAtMaxReplicas"
    "PVCPendingSustained"
    "TenantReconcileBacklog"
    "BuildFailureRateHigh"
    "LoopZeroQueueSlow"
    "LoopZeroDeployFailureRate"
    "LoopZeroLockContention"
    "LoopZeroSyncSlow"
    "LoopZeroBuildQueueSlow"
    "LoopZeroTempoStorageHigh"
    "LoopZeroCollectorExportErrors"
)

violations=0

# Use python for proper YAML-aware parsing of alert blocks.
python3 - "$RULES_DIR" "${EXCEPTION_LIST[@]}" << 'PY'
import re, os, sys

rules_dir = sys.argv[1]
exceptions = set(sys.argv[2:])
violations = 0

for f in sorted(os.listdir(rules_dir)):
    if 'prometheusrule' not in f or not f.endswith('.yaml'):
        continue
    path = os.path.join(rules_dir, f)
    with open(path) as fh:
        content = fh.read()
    # Split on alert markers
    blocks = re.split(r'\n\s*- alert:', content)
    for blk in blocks[1:]:
        m = re.match(r'\s*([A-Za-z0-9_]+)', blk)
        if not m:
            continue
        name = m.group(1)
        # Look ahead in block for runbook_url or runbook annotation
        body = blk[:1500]
        has_runbook = ('runbook_url' in body) or ('runbook:' in body)
        if has_runbook:
            continue
        if name in exceptions:
            continue
        print(f'RUNBOOK-MISSING VIOLATION: {f}:{name}')
        violations += 1

if violations == 0:
    print('PASS — every alert has a runbook (or is in EXCEPTION_LIST).')
    sys.exit(0)
print(f'\nFAIL — {violations} alert(s) without runbook annotation.')
print('To fix: add a `runbook_url:` annotation to the alert with a link to the relevant documentation.')
print('Or add the alert name to EXCEPTION_LIST in this script (with explanation).')
sys.exit(1)
PY
