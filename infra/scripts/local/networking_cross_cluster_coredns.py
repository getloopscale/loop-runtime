#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""CoreDNS NodeHosts transforms for networking-cross-cluster-coredns.sh
(de-costumed from the inline python3 heredoc + fragile `python3 -c` blocks per
plan home-unhomed-logic-suspect-keeps T3.1).

Per ADR D2 the kubectl get/patch/apply I/O stays in the bash orchestrator; this
module is a pure transform. Subcommand (argv[1]):

  merge          — merge current NodeHosts (env NCC_CURRENT) with the target
                   block (env NCC_TARGET); existing entries kept, target hosts
                   override; emit the sorted `ip host` block to stdout.
  set-nodehosts  — read the CoreDNS ConfigMap YAML from stdin, set
                   data.NodeHosts to env NCC_NODEHOSTS, emit patched YAML.
"""
import os
import sys

import yaml


def _index(block: str) -> dict:
    out = {}
    for line in block.strip().splitlines():
        parts = line.split()
        if len(parts) >= 2:
            out[parts[1]] = parts[0]  # host -> ip
    return out


def merge() -> None:
    current = _index(os.environ.get("NCC_CURRENT", ""))
    target = _index(os.environ.get("NCC_TARGET", ""))
    merged = dict(current)
    merged.update(target)  # target hosts override
    print("\n".join(f"{ip} {host}" for host, ip in sorted(merged.items())))


def set_nodehosts() -> None:
    d = yaml.safe_load(sys.stdin)
    d.setdefault("data", {})["NodeHosts"] = os.environ["NCC_NODEHOSTS"]
    print(yaml.safe_dump(d))


MODES = {"merge": merge, "set-nodehosts": set_nodehosts}

if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] not in MODES:
        sys.stderr.write(f"usage: {sys.argv[0]} {{{'|'.join(MODES)}}}\n")
        sys.exit(2)
    MODES[sys.argv[1]]()
