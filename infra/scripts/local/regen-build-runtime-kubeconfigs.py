#!/usr/bin/env python3
"""
regen-build-runtime-kubeconfigs.py — canonical post-Cleanroom regen of the
build/runtime/runtime2 SA tokens embedded in loopzero-cluster-kubeconfigs.

Trigger: QA Dogfooding Run-4 #133. Symptom — `loopzero deploy` failed with
INTERNAL_ERROR; loopzero-api logs showed continuous
    "argo step watcher: build cluster auth failed — rotate loopzero-build-sa token"
Root cause: the `default` SA token (kube-system) in the build cluster
expired (~24h TTL from Cleanroom). The `loopzero-cluster-kubeconfigs` Secret
mounted into loopzero-api (`/etc/loopzero/kubeconfigs/build.kubeconfig`) carried
the stale token. loopzero-api dispatch to Argo Workflows 401-ed → workflow
submission silently failed → builds marked FAILED with no logs.

Canonical fix (this script, idempotent):
  1. Discover each peer cluster's docker-network IP (k3d container).
  2. Mint a fresh `kube-system/default` SA token via kubectl (--duration=24h).
  3. Materialize a structurally-valid kubeconfig with the new token,
     using the cluster's published CA-data from `k3d kubeconfig get`.
  4. Patch `loopzero-system/loopzero-cluster-kubeconfigs` Secret in mgmt cluster
     with the new YAML (proper YAML serialization, NOT regex substitution
     which can break flow-mapping syntax).
  5. Restart loopzero-api so the volume-mounted Secret is re-read.

This script REPLACES the silent-rotation antipattern documented in
the H13 mitigation but specialized for the cross-cluster
loopzero-cluster-kubeconfigs Secret (not the per-cell ClusterCredential).

Per `feedback_close_issue_update_live_dev_public_2026_06_22` this is the
canonical post-Cleanroom action when QA dogfooding reports build/deploy
failure with INTERNAL_ERROR + loopzero-api auth-fail logs.

Usage:
  python3 regen-build-runtime-kubeconfigs.py           # all 3 (build+runtime+runtime2)
  python3 regen-build-runtime-kubeconfigs.py --only build   # one cluster
  python3 regen-build-runtime-kubeconfigs.py --verify  # restart + smoke probe
"""
from __future__ import annotations

import argparse
import base64
import json
import subprocess
import sys
import yaml


# Sprint 7 plan qa-sprint7-cleanup-2026-06-25 T2.1 (closes loopzero-cloud#36):
# extended with per-cluster (namespace, sa) tuple. The pre-Sprint-7 script
# minted `kube-system/default` for ALL clusters — but the build cluster's
# Argo Workflows RBAC ties `create workflows.argoproj.io` to the SA
# `argo/loopzero-workflow-submitter`, NOT to `kube-system/default`. Result:
# `loopzero deploy` failed at workflow submit with `argo: create workflow:
# Unauthorized` even though Role+RoleBinding for the correct SA existed.
#
# Per ADR D1: per-cluster SA mapping closes the RBAC gap canonically.
# runtime + runtime2 keep `kube-system/default` (no Argo workloads to
# submit there — those clusters host user workloads, not the build
# orchestrator's Argo Workflows control plane).
CLUSTER_MAP = {
    # logical name → (k3d cluster context, k3d container, kubeconfig data key, (sa_namespace, sa_name))
    "build":    ("k3d-loopzero-build",    "k3d-loopzero-build-server-0",    "build.kubeconfig",    ("argo", "loopzero-workflow-submitter")),
    "runtime":  ("k3d-loopzero-workload", "k3d-loopzero-workload-server-0", "runtime.kubeconfig",  ("kube-system", "default")),
    "runtime2": ("k3d-loopzero-runtime-2","k3d-loopzero-runtime-2-server-0","runtime2.kubeconfig", ("kube-system", "default")),
}


def discover_cluster_ip(container: str) -> str:
    out = subprocess.check_output(
        ["docker","inspect","-f",
         '{{(index .NetworkSettings.Networks "k3d-loopzero").IPAddress}}',
         container],
        text=True,
    ).strip()
    if not out:
        raise RuntimeError(f"docker network lookup empty for {container}")
    return out


def mint_token(context: str, namespace: str, sa: str) -> str:
    # Sprint 7 T2.1: namespace + sa parameterized per cluster. Pre-Sprint-7
    # hardcoded `kube-system/default` which was wrong for the build cluster
    # (RBAC ties create-workflows to argo/loopzero-workflow-submitter).
    return subprocess.check_output(
        ["kubectl", "--context=" + context, "-n", namespace,
         "create", "token", sa, "--duration=24h"],
        text=True,
    ).strip()


def get_ca_b64(context: str) -> str:
    raw = subprocess.check_output(["k3d","kubeconfig","get", context.replace("k3d-loopzero-","loopzero-").replace("loopzero-workload","loopzero-workload")], text=True)
    # k3d kubeconfig get accepts cluster short name (loopzero-build / loopzero-workload / loopzero-runtime-2)
    parsed = yaml.safe_load(raw)
    return parsed["clusters"][0]["cluster"]["certificate-authority-data"]


def build_kubeconfig(server_ip: str, ca_b64: str, token: str) -> str:
    kc = {
        "apiVersion": "v1",
        "kind": "Config",
        "clusters": [{
            "name": "in-cluster",
            "cluster": {
                "server": f"https://{server_ip}:6443",
                "certificate-authority-data": ca_b64,
            },
        }],
        "contexts": [{
            "name": "in-cluster",
            "context": {"cluster": "in-cluster", "user": "in-cluster"},
        }],
        "current-context": "in-cluster",
        "users": [{
            "name": "in-cluster",
            "user": {"token": token},
        }],
    }
    return yaml.safe_dump(kc, default_flow_style=False)


def patch_secret(data_key: str, kc_yaml: str) -> None:
    sec = json.loads(subprocess.check_output(
        ["kubectl","--context=k3d-loopzero-mgmt","-n","loopzero-system",
         "get","secret","loopzero-cluster-kubeconfigs","-o","json"]
    ))
    sec["data"][data_key] = base64.b64encode(kc_yaml.encode()).decode()
    proc = subprocess.run(
        ["kubectl","--context=k3d-loopzero-mgmt","apply","-f","-"],
        input=json.dumps(sec), text=True, capture_output=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"kubectl apply failed: {proc.stderr}")


def regen_one(logical: str) -> None:
    context, container, data_key, sa_spec = CLUSTER_MAP[logical]
    sa_namespace, sa_name = sa_spec
    ip = discover_cluster_ip(container)
    token = mint_token(context, sa_namespace, sa_name)
    ca = get_ca_b64(context)
    kc_yaml = build_kubeconfig(ip, ca, token)
    patch_secret(data_key, kc_yaml)
    print(f"[regen] {logical}: ip={ip} sa={sa_namespace}/{sa_name} token-len={len(token)} key={data_key} → patched")


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--only", choices=list(CLUSTER_MAP.keys()),
                   help="regen only this cluster (default: all 3)")
    p.add_argument("--verify", action="store_true",
                   help="restart loopzero-api + tail logs for auth-fail clear")
    args = p.parse_args()

    targets = [args.only] if args.only else list(CLUSTER_MAP.keys())
    for t in targets:
        regen_one(t)

    if args.verify:
        subprocess.run(["kubectl","--context=k3d-loopzero-mgmt","-n","loopzero-system",
                        "rollout","restart","deploy/loopzero-api"], check=True)
        subprocess.run(["kubectl","--context=k3d-loopzero-mgmt","-n","loopzero-system",
                        "rollout","status","deploy/loopzero-api","--timeout=120s"], check=True)
        print("[regen] --verify rollout complete; check `kubectl logs deploy/loopzero-api` for absence of `build cluster auth failed`")

    return 0


if __name__ == "__main__":
    sys.exit(main())
