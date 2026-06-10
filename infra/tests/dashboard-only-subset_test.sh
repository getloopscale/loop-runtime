#!/usr/bin/env bash
# T0.1 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1 EC-01 + EC-02: assert that profile=dashboard-only selector matches
# EXACTLY the 16 required mgmt releases AND no more (strict subset).
#
# Hermetic — does not touch a live cluster; parses the helmfile.yaml.gotmpl
# directly via yq.
#
# Run: bash infra/tests/dashboard-only-subset_test.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

HELMFILE="helmfile/mgmt.helmfile.yaml.gotmpl"

if [ ! -f "$HELMFILE" ]; then
  echo "FAIL: $HELMFILE not found"
  exit 1
fi

REQUIRED=(
  cert-manager loopzero-crds traefik postgresql
  gitea-redis gitea minio argocd
  ory-kratos ory-hydra loopzero-api
  loopzero-tenant-operator loopzero-app-operator
  loopzero-preview-operator loopzero-platform-operator
  loopzero-cloud
)

# Releases that MUST NOT have the dashboard-only label (would inflate the subset
# beyond what the droplet capacity supports and reintroduce the 2026-05-29
# apiserver overload).
FORBIDDEN=(
  harbor harbor-redis nfs-server-provisioner velero
  prometheus grafana opencost tempo otel-collector
  blackbox-exporter chaos-mesh smoke-test metrics-server
)

# Extract release name → labels.profile mapping using python (avoids yq's
# gotmpl-vs-yaml ambiguity).
python3 <<PY
import re, sys
from pathlib import Path

src = Path("$HELMFILE").read_text()
lines = src.split("\n")

# Find releases: section
start = None
for i,l in enumerate(lines):
    if l.strip() == "releases:":
        start = i; break
if start is None:
    print("FAIL: releases: section not found"); sys.exit(1)

# Walk releases
results = {}  # name -> profile_label or None
current_name = None
i = start + 1
while i < len(lines):
    line = lines[i]
    m = re.match(r"^  - name: ([\w-]+)\s*$", line)
    if m:
        current_name = m.group(1)
        results.setdefault(current_name, None)
        # Look ahead up to 10 lines for labels: \n      profile: X
        for j in range(i+1, min(i+15, len(lines))):
            if re.match(r"^  - name:", lines[j]):
                break
            sub = re.match(r"^      profile: (\S+)\s*$", lines[j])
            if sub:
                results[current_name] = sub.group(1)
                break
    i += 1

# Required set must all be labeled
required = """$( printf "%s\n" "${REQUIRED[@]}" )""".strip().split("\n")
forbidden = """$( printf "%s\n" "${FORBIDDEN[@]}" )""".strip().split("\n")

fail = []
for name in required:
    if results.get(name) != "dashboard-only":
        fail.append(f"REQUIRED '{name}' missing 'profile: dashboard-only' label (got: {results.get(name)})")
for name in forbidden:
    if results.get(name) == "dashboard-only":
        fail.append(f"FORBIDDEN '{name}' has 'profile: dashboard-only' label — would re-bloat the cluster")

# Sanity: every release labeled dashboard-only MUST be in required
labeled = [n for n, p in results.items() if p == "dashboard-only"]
extras = set(labeled) - set(required)
for e in extras:
    fail.append(f"UNEXPECTED label on '{e}' — only the 16 required releases should carry profile=dashboard-only")

if fail:
    print("FAIL: hermetic subset check")
    for f in fail:
        print("  -", f)
    sys.exit(1)

print(f"OK: {len(labeled)} releases labeled profile=dashboard-only")
print(f"OK: all {len(required)} required releases present")
print(f"OK: all {len(forbidden)} forbidden releases absent from subset")
PY
