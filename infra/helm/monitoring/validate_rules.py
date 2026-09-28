#!/usr/bin/env python3
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 5 T5.4 + Phase 8 T8.1
#
# Structural validator for PrometheusRule manifests when promtool is not
# available locally. Plan TDD `test_alerts_promtool_validate` translated to
# offline schema check. CI workflows still SHOULD run `promtool check rules`
# against these files (added to multi-cell.yml live job) — this script is
# the offline-runnable analog.
#
# Validates:
#   - YAML parses
#   - apiVersion + kind canonical (monitoring.coreos.com/v1, PrometheusRule)
#   - spec.groups present and non-empty
#   - Each group has a name and rules
#   - Each alert rule has: alert (name), expr (non-empty), labels.severity,
#     annotations.summary, annotations.runbook (or runbook_url)
#   - severity ∈ {critical, warning, info}
#
# Usage:
#   python3 infra/helm/monitoring/validate_rules.py
# Exit 0 = all rules valid; non-zero = at least one error.

import glob
import os
import sys

import yaml

VALID_SEVERITIES = {"critical", "warning", "info"}
RUNBOOK_KEYS = {"runbook", "runbook_url"}


def validate(path: str) -> int:
    errs = 0
    try:
        with open(path) as fh:
            doc = yaml.safe_load(fh)
    except Exception as e:
        print(f"  FAIL  {path}: YAML parse error: {e}", file=sys.stderr)
        return 1

    if doc.get("apiVersion") != "monitoring.coreos.com/v1":
        print(f"  FAIL  {path}: apiVersion={doc.get('apiVersion')!r} (need 'monitoring.coreos.com/v1')", file=sys.stderr)
        errs += 1

    if doc.get("kind") != "PrometheusRule":
        print(f"  FAIL  {path}: kind={doc.get('kind')!r} (need 'PrometheusRule')", file=sys.stderr)
        return 1

    groups = ((doc.get("spec") or {}).get("groups")) or []
    if not groups:
        print(f"  FAIL  {path}: spec.groups empty", file=sys.stderr)
        return 1

    rules_seen = 0
    for gi, group in enumerate(groups):
        gname = group.get("name", "")
        if not gname:
            print(f"  FAIL  {path}: groups[{gi}].name empty", file=sys.stderr)
            errs += 1

        rules = group.get("rules") or []
        if not rules:
            print(f"  FAIL  {path}: groups[{gi}] '{gname}' has no rules", file=sys.stderr)
            errs += 1
            continue

        for ri, rule in enumerate(rules):
            rules_seen += 1
            # Skip recording rules (they have `record:` not `alert:`) — they
            # are valid but the canary protocol cares about alert rules.
            if "record" in rule and "alert" not in rule:
                continue

            name = rule.get("alert", "")
            if not name:
                print(f"  FAIL  {path}: groups[{gi}].rules[{ri}] missing 'alert' name", file=sys.stderr)
                errs += 1
                continue

            expr = rule.get("expr", "")
            if not str(expr).strip():
                print(f"  FAIL  {path}: alert '{name}' missing/empty expr", file=sys.stderr)
                errs += 1

            labels = rule.get("labels", {}) or {}
            severity = labels.get("severity", "")
            if severity not in VALID_SEVERITIES:
                print(f"  FAIL  {path}: alert '{name}' severity={severity!r} not in {sorted(VALID_SEVERITIES)}", file=sys.stderr)
                errs += 1

            annotations = rule.get("annotations", {}) or {}
            summary = annotations.get("summary", "")
            if not str(summary).strip():
                print(f"  FAIL  {path}: alert '{name}' missing annotations.summary", file=sys.stderr)
                errs += 1

            if not (RUNBOOK_KEYS & set(annotations.keys())):
                print(f"  WARN  {path}: alert '{name}' has no runbook/runbook_url annotation", file=sys.stderr)

    if errs == 0:
        print(f"  OK    {path} ({rules_seen} rules across {len(groups)} groups)")
    return errs


def main() -> int:
    root = os.environ.get("LOOPZERO_ROOT", os.getcwd())
    pattern = os.path.join(root, "infra", "helm", "monitoring", "manifests", "prometheusrule-*.yaml")
    files = sorted(glob.glob(pattern))
    if not files:
        print(f"FAIL: no PrometheusRule files at {pattern}", file=sys.stderr)
        return 1

    total = 0
    for f in files:
        total += validate(f)

    print(f"[validate:rules] {len(files)} files, {total} errors")
    return 0 if total == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
