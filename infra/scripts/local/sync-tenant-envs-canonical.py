#!/usr/bin/env python3
"""
sync-tenant-envs-canonical.py — canonical post-deploy reconcile of
Tenant CR Spec.Environments with deployed project names.

Trigger: QA Dogfooding Run-6 #135. Symptom — `loopzero deploy` build chain
SUCCEEDED but ArgoCD App sync stuck:

  one or more synchronization tasks are not valid
  Rollout/loopzerokit-example-app: status=SyncFailed
  msg=namespace loopzero-tn-6864661cd10bd134-loopzerokit-example-production
  is not permitted in project 'tn-6864661cd10bd134'

Root cause: Tenant.Spec.Environments declared envs for project `teste`
(originally connected), but `loopzero deploy` from the `loopzerokit-example`
git repo created a NEW project from `loopzero.yaml` (sub-issue 3 of #133:
`loopzero deploy` ignores connected project, uses loopzero.yaml.project as
namespace prefix). The renderer emitted manifests with namespace
`loopzero-tn-<tenant>-loopzerokit-example-production`, but the AppProject
(operator-derived from Tenant CR) only allowed `teste-*`. Mismatch
→ sync fails forever.

This script discovers the project names in recent builds (PG `builds`
table) and idempotently extends Tenant CR Spec.Environments so the
AppProject reconciles to allow those destinations. This is the
LIVE-side mitigation; the canonical engine-side fix is to ensure
`loopzero deploy` uses the connected project name (engine handler at
api/internal/services/build_orchestrator.go — separate slice).

Usage:
  python3 sync-tenant-envs-canonical.py --tenant tn-6864661cd10bd134
  python3 sync-tenant-envs-canonical.py --all     # discover all tenants

Idempotent: skips entries already present in Spec.Environments.
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys


def builds_projects_for_tenant(tenant_id: str) -> set[str]:
    sql = (
        "SELECT DISTINCT project_id FROM builds "
        f"WHERE tenant_id = '{tenant_id}' AND status IN ('SUCCEEDED','RUNNING') "
        "AND project_id IS NOT NULL AND project_id != '';"
    )
    out = subprocess.check_output(
        ["kubectl","--context=k3d-loopzero-core","-n","loopzero-system","exec","postgresql-0","--",
         "env","PGPASSWORD=loopzero-dev-password",
         "psql","-U","loopzero","-d","loopzero","-tAc", sql],
        text=True,
    )
    return {line.strip() for line in out.splitlines() if line.strip() and not line.startswith("Defaulted")}


def get_tenant_envs(tenant_id: str) -> tuple[list[dict], dict]:
    raw = subprocess.check_output(
        ["kubectl","--context=k3d-loopzero-core","-n","loopzero-system",
         "get","tenant", tenant_id, "-o","json"],
        text=True,
    )
    obj = json.loads(raw)
    return obj["spec"].get("environments", []), obj


def patch_tenant_envs(tenant_id: str, ops: list[dict]) -> None:
    proc = subprocess.run(
        ["kubectl","--context=k3d-loopzero-core","-n","loopzero-system",
         "patch","tenant", tenant_id, "--type=json", "-p", json.dumps(ops)],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"patch failed: {proc.stderr}")


def reconcile_one(tenant_id: str, dry_run: bool) -> int:
    projects = builds_projects_for_tenant(tenant_id)
    if not projects:
        print(f"[{tenant_id}] no built projects found — nothing to reconcile")
        return 0

    envs, _ = get_tenant_envs(tenant_id)
    have = {(e.get("project"), e.get("name")) for e in envs}

    needed = []
    # Per canonical PaaS model (project_paas_envs_preview_production_only):
    # every project has permanent preview + production envs.
    for proj in projects:
        for env in ("production", "preview"):
            if (proj, env) not in have:
                needed.append({"name": env, "project": proj, "cluster": "workload"})

    if not needed:
        print(f"[{tenant_id}] all built projects already mapped — idempotent no-op "
              f"(envs={len(envs)}, projects={projects})")
        return 0

    print(f"[{tenant_id}] adding {len(needed)} env entries: {[(e['project'],e['name']) for e in needed]}")
    if dry_run:
        return 0

    ops = [{"op":"add","path":"/spec/environments/-","value":v} for v in needed]
    patch_tenant_envs(tenant_id, ops)
    print(f"[{tenant_id}] patched")
    return 0


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--tenant")
    p.add_argument("--all", action="store_true")
    p.add_argument("--dry-run", action="store_true")
    args = p.parse_args()

    if args.all:
        raw = subprocess.check_output(
            ["kubectl","--context=k3d-loopzero-core","-n","loopzero-system",
             "get","tenants","-o","jsonpath={.items[*].metadata.name}"],
            text=True,
        )
        targets = raw.split()
    elif args.tenant:
        targets = [args.tenant]
    else:
        print("usage: --tenant <id> | --all", file=sys.stderr)
        return 2

    for t in targets:
        reconcile_one(t, args.dry_run)
    return 0


if __name__ == "__main__":
    sys.exit(main())
