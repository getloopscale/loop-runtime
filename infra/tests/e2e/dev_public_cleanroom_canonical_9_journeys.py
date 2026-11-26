#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
"""JSON report builder for dev-public-cleanroom-canonical-9-journeys_test.sh
(de-costumed from the inline python3 heredoc per plan
home-unhomed-logic-suspect-keeps T3.3).

The bash orchestrator runs the 9 journeys and records each result into a temp
file (one `id:status:detail` line per journey); this module reads that file +
the counts from the environment and emits the aggregate report JSON to stdout.
No external I/O.

Env:
  CR_RESULTS_FILE — path to the `id:status:detail` results file
  CR_PASS / CR_FAIL / CR_SKIP / CR_ELAPSED — integer counters
"""
import json
import os
import sys


def main() -> int:
    journeys = []
    with open(os.environ["CR_RESULTS_FILE"]) as f:
        for line in f:
            line = line.rstrip("\n")
            if line:
                journeys.append(line.split(":", 2))
    fail = int(os.environ["CR_FAIL"])
    print(json.dumps({
        "passed": int(os.environ["CR_PASS"]),
        "failed": fail,
        "skipped": int(os.environ["CR_SKIP"]),
        "elapsed_seconds": int(os.environ["CR_ELAPSED"]),
        "journeys": journeys,
        "verdict": "PASS" if fail == 0 else "FAIL",
    }, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
