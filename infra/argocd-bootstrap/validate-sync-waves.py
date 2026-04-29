#!/usr/bin/env python3
"""
CHG-2026-04-28-LAYERED-BOOTSTRAP — sync-wave invariant checker for ArgoCD App-of-Apps.

Validates that the 38 Application manifests in apps/ have a monotonic sync-wave
ordering — i.e., resources that depend on others come AFTER them. Catches manual
edits that would make ArgoCD try to create resources before their CRDs exist.

Run:
  python3 infra/argocd-bootstrap/validate-sync-waves.py

Exit codes:
  0 — all invariants OK
  1 — at least one invariant violated
"""
import os
import sys

try:
    import yaml
except ImportError:
    print("ERROR: install pyyaml: pip install --user pyyaml", file=sys.stderr)
    sys.exit(2)

# Run from project root (script may be invoked from elsewhere).
HERE = os.path.dirname(os.path.abspath(__file__))
APPS_DIR = os.path.join(HERE, "apps")

# Expected ordering invariants — (app_a, app_b) means wave(a) <= wave(b).
INVARIANTS = [
    # mgmt cluster
    ("mgmt-cert-manager", "mgmt-traefik"),
    ("mgmt-cert-manager", "mgmt-argocd"),
    ("mgmt-cert-manager", "mgmt-harbor"),
    ("mgmt-traefik", "mgmt-loopzero-api"),
    ("mgmt-prometheus", "mgmt-grafana"),
    ("mgmt-tempo", "mgmt-otel-collector"),
    ("mgmt-argocd", "mgmt-loopzero-api"),
    ("mgmt-gitea", "mgmt-loopzero-api"),
    ("mgmt-minio", "mgmt-harbor"),
    ("mgmt-ory-kratos", "mgmt-ory-hydra"),
    ("mgmt-ory-hydra", "mgmt-loopzero-cloud"),
    ("mgmt-loopzero-api", "mgmt-loopzero-controller"),
    ("mgmt-loopzero-api", "mgmt-tenant-controller"),
    ("mgmt-loopzero-api", "mgmt-smoke-test"),
    # runtime cluster
    ("runtime-cert-manager", "runtime-traefik"),
    ("runtime-cert-manager", "runtime-policy-controller"),
    ("runtime-policy-controller", "runtime-admission-policies"),
    ("runtime-keda", "runtime-keda-http-addon"),
    ("runtime-traefik", "runtime-cluster-issuer"),
    # build cluster
    ("build-loki", "build-promtail"),
]


def main() -> int:
    if not os.path.isdir(APPS_DIR):
        print(f"ERROR: apps dir not found: {APPS_DIR}", file=sys.stderr)
        return 2

    waves: dict[str, int] = {}
    for fname in sorted(os.listdir(APPS_DIR)):
        if not fname.endswith(".yaml"):
            continue
        with open(os.path.join(APPS_DIR, fname)) as fh:
            doc = yaml.safe_load(fh)
        if doc is None:
            continue
        annot = doc.get("metadata", {}).get("annotations", {}) or {}
        wave_str = annot.get("argocd.argoproj.io/sync-wave", "0")
        try:
            waves[doc["metadata"]["name"]] = int(wave_str)
        except (KeyError, ValueError) as e:
            print(f"WARN: {fname} has invalid sync-wave: {e}", file=sys.stderr)

    fails: list[str] = []
    for before, after in INVARIANTS:
        w_before = waves.get(before)
        w_after = waves.get(after)
        if w_before is None:
            fails.append(f"app missing: {before}")
            continue
        if w_after is None:
            fails.append(f"app missing: {after}")
            continue
        if w_before > w_after:
            fails.append(
                f"sync-wave broken: {before}(wave={w_before}) must come before {after}(wave={w_after})"
            )

    if fails:
        print("FAIL — sync-wave invariants broken:")
        for f in fails:
            print(f"  - {f}")
        return 1

    print(f"✓ {len(INVARIANTS)} sync-wave invariants OK across {len(waves)} apps")
    return 0


if __name__ == "__main__":
    sys.exit(main())
