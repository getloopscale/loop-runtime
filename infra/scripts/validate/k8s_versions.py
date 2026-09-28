#!/usr/bin/env python3
"""validate-k8s_versions.py — validate K8s versions in Pulumi.<stack>.yaml
are still supported by DigitalOcean.

Plan 2026-05-21-deep-review-remediation T5.4: extracted from Taskfile
heredoc (taskfiles/test.yml:910-950).

DOKS rotates patch versions ~quarterly — this catches stale slugs before
bootstrap fails with 422 invalid version slug.

Exit codes:
  0 — all version slugs supported
  1 — doctl unavailable or unauthenticated
  2 — at least one stack has a stale slug
"""

from __future__ import annotations

import json
import subprocess
import sys

import yaml


def main() -> int:
    try:
        supported_raw = subprocess.check_output(
            ["doctl", "kubernetes", "options", "versions", "-o", "json"],
            stderr=subprocess.DEVNULL,
            timeout=30,
        )
        supported = {v["slug"] for v in json.loads(supported_raw)}
    except Exception as e:  # noqa: BLE001
        print(f"FAIL: doctl unavailable or unauthenticated: {e}", file=sys.stderr)
        return 1

    print(f"DO supports: {sorted(supported)}")

    failed: list[str] = []
    for stack_yaml in [
        "Pulumi.dev.yaml",
        "Pulumi.staging.yaml",
        "Pulumi.homolog.yaml",
        "Pulumi.prod.yaml",
    ]:
        path = f"infra/pulumi/{stack_yaml}"
        try:
            cfg = yaml.safe_load(open(path)).get("config", {})
        except FileNotFoundError:
            print(f"  - {stack_yaml}: missing (skipped)")
            continue

        for key in ["mgmtK8sVersion", "runtimeK8sVersion", "buildK8sVersion"]:
            slug = cfg.get(f"loopzero-infra:{key}")
            if not slug:
                continue
            if slug in supported:
                print(f"OK  {stack_yaml} {key} = {slug}")
            else:
                failed.append(
                    f"  - {stack_yaml} {key}: {slug} NOT in supported set"
                )

    if failed:
        print("\nFAIL: Stale K8s versions detected:")
        print("\n".join(failed))
        return 2

    print("\n✓ All K8s version slugs supported by DO")
    return 0


if __name__ == "__main__":
    sys.exit(main())
