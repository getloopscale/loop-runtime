#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""Data-processing helper for baseline-capture.sh (de-costumed from inline
python3 heredocs per plan home-unhomed-logic-suspect-keeps T1.1).

The bash orchestrator (baseline-capture.sh) still owns ALL external I/O
(git / kubectl / helm / curl / psql); this module is pure data-processing:
it reads inputs from environment variables / temp files and emits JSON to
stdout. One subcommand per former heredoc (argv[1]):

  code     — Layer 1 code_state JSON      (env: BC_COMMIT/BC_BRANCH/BC_AHEAD/BC_BEHIND/BC_CLEAN)
  runtime  — Layer 2 runtime_state JSON   (env: TMP_DIR with pods/helm/apps/tens .json)
  config   — Layer 3 config_state JSON    (env: BC_PG_MAX/BC_PLP/BC_COSIGN)
  probes   — Layer 4 live_probes JSON     (env: BC_R1..BC_R6)
  compose  — final 4-layer snapshot JSON  (env: COMPOSE_DIR/CAPTURED_AT)
"""
import json
import os
import sys


def _bool(env_value: str) -> bool:
    return env_value == "True"


def code() -> None:
    print(json.dumps({
        "commit": os.environ.get("BC_COMMIT", ""),
        "branch": os.environ.get("BC_BRANCH", ""),
        "ahead": int(os.environ.get("BC_AHEAD", "0") or "0"),
        "behind": int(os.environ.get("BC_BEHIND", "0") or "0"),
        "working_tree_clean": _bool(os.environ.get("BC_CLEAN", "False")),
    }, sort_keys=True))


def _workload(pod: dict) -> str:
    """The workload that owns a pod: a Deployment's ReplicaSet loses its template
    hash, any other owner is named as-is, and an unowned pod is its own workload."""
    meta = pod.get("metadata", {})
    labels = meta.get("labels") or {}
    # k3s names a ServiceLB DaemonSet after a prefix of the Service UID, which a
    # rebuild changes; the service it serves is the stable identity (#417).
    svc = labels.get("svccontroller.k3s.cattle.io/svcname")
    if svc:
        return f"svclb-{labels.get('svccontroller.k3s.cattle.io/svcnamespace', '')}-{svc}"
    owners = meta.get("ownerReferences") or []
    if not owners:
        return meta.get("name", "")
    owner = owners[0]
    name = owner.get("name", "")
    if owner.get("kind") == "ReplicaSet" and "-" in name:
        return name.rsplit("-", 1)[0]
    return name


def runtime() -> None:
    tmp = os.environ["TMP_DIR"]
    with open(f"{tmp}/pods.json") as f:
        pods = json.load(f)
    with open(f"{tmp}/helm.json") as f:
        helm = json.load(f)
    with open(f"{tmp}/apps.json") as f:
        apps = json.load(f)
    with open(f"{tmp}/tens.json") as f:
        tenants = json.load(f)

    # A pod is identified by the workload that owns it, not by its name: a rebuild
    # names every Deployment and DaemonSet pod afresh, and comparing names made the
    # before/after diff fail on every rebuild (#417).
    workloads = {}
    for p in pods.get("items", []):
        ns = p.get("metadata", {}).get("namespace", "")
        wl = _workload(p)
        entry = workloads.setdefault((ns, wl), {"namespace": ns, "name": f"{ns}/{wl}", "pods": 0, "running": 0})
        entry["pods"] += 1
        if p.get("status", {}).get("phase", "") == "Running":
            entry["running"] += 1
    pods_out = list(workloads.values())
    helm_out = [
        {"name": r.get("name", ""), "namespace": r.get("namespace", ""),
         "chart": r.get("chart", "")}
        for r in (helm if isinstance(helm, list) else [])
    ]
    apps_out = [
        {"name": a.get("metadata", {}).get("name", ""),
         "sync_status": a.get("status", {}).get("sync", {}).get("status", ""),
         "health_status": a.get("status", {}).get("health", {}).get("status", "")}
        for a in apps.get("items", [])
    ]
    tenants_out = [
        {"name": t.get("metadata", {}).get("name", ""),
         "phase": t.get("status", {}).get("phase", ""),
         "plan": t.get("spec", {}).get("plan", "")}
        for t in tenants.get("items", [])
    ]
    # Sort arrays by name for deterministic ordering.
    pods_out.sort(key=lambda x: (x["namespace"], x["name"]))
    helm_out.sort(key=lambda x: (x["namespace"], x["name"]))
    apps_out.sort(key=lambda x: x["name"])
    tenants_out.sort(key=lambda x: x["name"])

    print(json.dumps({
        "pods": pods_out,
        "helm_releases": helm_out,
        "argocd_apps": apps_out,
        "tenants": tenants_out,
    }, sort_keys=True))


def config() -> None:
    print(json.dumps({
        "pg_max_migration": int(os.environ.get("BC_PG_MAX", "0") or "0"),
        "plan_limits_present": _bool(os.environ.get("BC_PLP", "False")),
        "cosign_mode": os.environ.get("BC_COSIGN", "keyed"),
    }, sort_keys=True))


def probes() -> None:
    print(json.dumps({
        "https_app_root": int(os.environ["BC_R1"]),
        "https_app_projects": int(os.environ["BC_R2"]),
        "https_app_envs": int(os.environ["BC_R3"]),
        "https_cloud_api_projects": int(os.environ["BC_R4"]),
        "https_app_health": int(os.environ["BC_R5"]),
        "https_cloud_webhook": int(os.environ["BC_R6"]),
    }, sort_keys=True))


def compose() -> None:
    d = os.environ["COMPOSE_DIR"]
    out = {
        "captured_at": os.environ["CAPTURED_AT"],
        "code_state": json.load(open(f"{d}/code.json")),
        "runtime_state": json.load(open(f"{d}/runtime.json")),
        "config_state": json.load(open(f"{d}/config.json")),
        "live_probes": json.load(open(f"{d}/probes.json")),
    }
    print(json.dumps(out, sort_keys=True, indent=2))


MODES = {
    "code": code,
    "runtime": runtime,
    "config": config,
    "probes": probes,
    "compose": compose,
}

if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] not in MODES:
        sys.stderr.write(f"usage: {sys.argv[0]} {{{'|'.join(MODES)}}}\n")
        sys.exit(2)
    MODES[sys.argv[1]]()
