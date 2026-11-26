#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
"""Verdict emitter for live-proof-dev-public.sh (de-costumed from the inline
python3 heredoc per plan home-unhomed-logic-suspect-keeps T2.3).

The bash orchestrator owns all kubectl/curl/psql probing and records each
result into a temp file (one `key<TAB>value` line per result). This module
reads that file + ENV_NAME/PASS/FAIL from the environment and emits the
machine-readable verdict JSON to stdout. No external I/O.

Env:
  LP_RESULTS_FILE — path to the `key<TAB>value` results file
  LP_ENV_NAME     — environment name (e.g., dev-public)
  LP_PASS         — pass count (int)
  LP_FAIL         — fail count (int)
"""
import json
import os
import sys


def main() -> int:
    results: dict[str, str] = {}
    with open(os.environ["LP_RESULTS_FILE"]) as f:
        for line in f:
            line = line.rstrip("\n")
            if not line:
                continue
            key, _, value = line.partition("\t")
            results[key] = value

    verdict = "GREEN" if all(v == "PROOF" for v in results.values()) else "RED"
    print(json.dumps({
        "env": os.environ["LP_ENV_NAME"],
        "pass": int(os.environ["LP_PASS"]),
        "fail": int(os.environ["LP_FAIL"]),
        "verdict": verdict,
        "results": results,
    }, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
