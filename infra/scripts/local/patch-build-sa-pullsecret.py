#!/usr/bin/env python3
"""
patch-build-sa-pullsecret.py — canonical post-Cleanroom fix for #134.

Trigger: QA Dogfooding Run-5 #134. Symptom — deploy build chain stuck
indefinitely after `generate-dockerfile`; pod
`loopzero-build-build-<id>-sign-image-app-*` in argo namespace stayed
`ImagePullBackOff` for `registry.digitalocean.com/loopzero-system/
loopzero-cosign-signer:v0.1.0`.

Root cause: the ServiceAccount `argo/loopzero-build-sa` (which workflow pods
inherit) had EMPTY imagePullSecrets. The chart
`charts/loopzero-build-cwts/templates/serviceaccount.yaml` SHOULD inject
`imagePullSecrets: - name: loopzero-system`, but the helm release was
deployed without that block triggering, AND the cluster has the DOCR
auth Secret under a different name (`do-registry`, not `loopzero-system`).

Net effect: every workflow step pulling a DOCR-hosted tool image
(cosign-signer, syft-scanner, clone-runner) sat in ImagePullBackOff and
the deploy never progressed past local-only build steps.

This script (Python — avoids no-new-bash gate) patches
`argo/loopzero-build-sa` with the live secret name(s), idempotent.

Per `feedback_close_issue_update_live_dev_public_2026_06_22` this is
the canonical post-Cleanroom action when the build chain stalls on
ImagePullBackOff. Companion to `regen-build-runtime-kubeconfigs.py`
(which addressed the SA token expiry at the loopzero-api→argo boundary).

Usage:
  python3 patch-build-sa-pullsecret.py             # apply LIVE
  python3 patch-build-sa-pullsecret.py --dry-run   # show what would be patched
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys


def list_pull_secrets() -> list[str]:
    """Return all dockerconfigjson Secret names in argo NS."""
    out = subprocess.check_output(
        ["kubectl","--context=k3d-loopzero-build","-n","argo",
         "get","secrets","-o","json"],
        text=True,
    )
    secrets = json.loads(out).get("items", [])
    return [
        s["metadata"]["name"]
        for s in secrets
        if s.get("type") == "kubernetes.io/dockerconfigjson"
    ]


def get_sa_pull_secrets() -> list[str]:
    out = subprocess.check_output(
        ["kubectl","--context=k3d-loopzero-build","-n","argo",
         "get","sa","loopzero-build-sa","-o","json"],
        text=True,
    )
    sa = json.loads(out)
    return [s["name"] for s in sa.get("imagePullSecrets", [])]


def patch_sa(secret_names: list[str], dry_run: bool) -> None:
    patch = {"imagePullSecrets": [{"name": n} for n in secret_names]}
    if dry_run:
        print(f"[dry-run] would patch loopzero-build-sa with: {patch}")
        return
    proc = subprocess.run(
        ["kubectl","--context=k3d-loopzero-build","-n","argo",
         "patch","sa","loopzero-build-sa",
         "-p", json.dumps(patch)],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"patch failed: {proc.stderr}")
    print(f"[patched] loopzero-build-sa imagePullSecrets={secret_names}")


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--dry-run", action="store_true")
    args = p.parse_args()

    pull = list_pull_secrets()
    if not pull:
        print("[fail] no dockerconfigjson secret found in argo NS — "
              "DOCR/registry auth missing entirely; cannot patch", file=sys.stderr)
        return 1
    print(f"[discovered] dockerconfigjson secrets in argo: {pull}")

    current = get_sa_pull_secrets()
    print(f"[current] loopzero-build-sa imagePullSecrets: {current}")

    if set(current) == set(pull):
        print(f"[skip] loopzero-build-sa already references all available pull secrets — idempotent no-op")
        return 0

    patch_sa(pull, args.dry_run)
    return 0


if __name__ == "__main__":
    sys.exit(main())
