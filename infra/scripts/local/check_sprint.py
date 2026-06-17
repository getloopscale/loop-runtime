#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""JSON report builder for check-sprint.sh (de-costumed from the inline python3
heredoc per plan home-unhomed-logic-suspect-keeps T3.2).

The bash orchestrator runs the category probes and records each result into a
temp file (one `id:status:detail` line per category); this module reads that
file + the counts from the environment and emits the report JSON to stdout.
No external I/O.

Env:
  CS_RESULTS_FILE — path to the `id:status:detail` results file
  CS_PASS / CS_WARN / CS_FAIL / CS_SKIP / CS_ELAPSED — integer counters
"""
import json
import os
import sys


def main() -> int:
    results = []
    with open(os.environ["CS_RESULTS_FILE"]) as f:
        for line in f:
            line = line.rstrip("\n")
            if line:
                results.append(line.split(":", 2))
    print(json.dumps({
        "pass": int(os.environ["CS_PASS"]),
        "warn": int(os.environ["CS_WARN"]),
        "fail": int(os.environ["CS_FAIL"]),
        "skip": int(os.environ["CS_SKIP"]),
        "elapsed_seconds": int(os.environ["CS_ELAPSED"]),
        "results": results,
    }, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
