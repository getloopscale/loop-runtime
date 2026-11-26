#!/usr/bin/env python3
# CHG-2026-04-30-INFRA-NORMALIZE / Coverage Matrix #8
#
# Sloth SLO file validator (offline). Closes the "Sloth files bypass
# kubeconform" gap. Does NOT exercise the sloth CLI (which generates
# PrometheusRule resources from these inputs); when sloth is available
# in CI, add a second pass.
#
# Usage:
#   python3 infra/sloth/validate.py
# Exit 0 = all files valid; non-zero = at least one error.

import glob
import os
import sys

import yaml

REQUIRED_TOP = {"version", "service", "slos"}
REQUIRED_SLO = {"name", "objective", "sli", "alerting"}
WINDOW_TOKEN = "{" + "{.window}" + "}"


def validate(path: str) -> int:
    errs = 0
    try:
        with open(path) as fh:
            doc = yaml.safe_load(fh)
    except Exception as e:
        print(f"  FAIL  {path}: YAML parse error: {e}", file=sys.stderr)
        return 1

    missing_top = REQUIRED_TOP - set(doc.keys())
    if missing_top:
        print(f"  FAIL  {path}: missing top keys {missing_top}", file=sys.stderr)
        return 1

    if doc["version"] != "prometheus/v1":
        print(f"  FAIL  {path}: version={doc['version']} (need 'prometheus/v1')", file=sys.stderr)
        errs += 1

    slos = doc.get("slos") or []
    if not slos:
        print(f"  FAIL  {path}: empty slos[]", file=sys.stderr)
        return 1

    for i, slo in enumerate(slos):
        missing = REQUIRED_SLO - set(slo.keys())
        if missing:
            print(f"  FAIL  {path}: slo[{i}] missing {missing}", file=sys.stderr)
            errs += 1
            continue

        sli = slo.get("sli", {}) or {}
        # Sloth supports two SLI specs (mutually exclusive):
        #   events: error_query + total_query (counter-based)
        #   raw:    error_ratio_query        (gauge / pre-computed ratio)
        if "events" in sli:
            events = sli["events"] or {}
            for k in ("error_query", "total_query"):
                q = events.get(k, "")
                if not q:
                    print(f"  FAIL  {path}: slo[{i}].sli.events.{k} empty", file=sys.stderr)
                    errs += 1
                elif WINDOW_TOKEN not in q:
                    print(f"  WARN  {path}: slo[{i}].sli.events.{k} missing {WINDOW_TOKEN}: {q!r}", file=sys.stderr)
        elif "raw" in sli:
            raw = sli["raw"] or {}
            q = raw.get("error_ratio_query", "")
            if not q:
                print(f"  FAIL  {path}: slo[{i}].sli.raw.error_ratio_query empty", file=sys.stderr)
                errs += 1
            elif WINDOW_TOKEN not in q:
                print(f"  WARN  {path}: slo[{i}].sli.raw.error_ratio_query missing {WINDOW_TOKEN}: {q!r}", file=sys.stderr)
        else:
            print(f"  FAIL  {path}: slo[{i}].sli must define either 'events' or 'raw'", file=sys.stderr)
            errs += 1

        alerting = slo.get("alerting", {}) or {}
        for ak in ("page_alert", "ticket_alert"):
            if ak not in alerting:
                print(
                    f"  FAIL  {path}: slo[{i}].alerting.{ak} missing (multi-burn-rate required)",
                    file=sys.stderr,
                )
                errs += 1

    if errs == 0:
        print(f"  OK    {path} ({len(slos)} SLO{'s' if len(slos) != 1 else ''})")
    return errs


def main() -> int:
    root = os.environ.get("LOOP_RUNTIME_ROOT", os.getcwd())
    pattern = os.path.join(root, "infra", "sloth", "*.yaml")
    files = sorted(glob.glob(pattern))
    if not files:
        print(f"FAIL: no Sloth files at {pattern}", file=sys.stderr)
        return 1

    total_errs = 0
    for f in files:
        total_errs += validate(f)

    print(f"[validate:slos] {len(files)} files, {total_errs} errors")
    return 0 if total_errs == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
