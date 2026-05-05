#!/usr/bin/env bash
# infra/tests/scripts/postgresql-pdb-config_test.sh
#
# Regression for B2 (real-paas-cleanroom-dogfood-loop-plan iter 2):
# the Bitnami postgresql chart MUST NOT create a PodDisruptionBudget.
# Tiltfile applies its own PDB at infra/helm/postgresql/templates/pdb.yaml
# with `minAvailable: 1`. Bitnami's chart-owned PDB uses `maxUnavailable: 1`.
# K8s rejects PDBs with both fields set; the merged apply failed with
# `minAvailable and maxUnavailable cannot be both set` and tilt ci exited
# in 9s. Fix: set `primary.pdb.create: false` in
# infra/helm/postgresql/values.yaml. This test guards that setting against
# accidental removal.
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
VALUES="${REPO_ROOT}/infra/helm/postgresql/values.yaml"

if [[ ! -f "$VALUES" ]]; then
  printf 'FAIL: values.yaml missing at %s\n' "$VALUES" >&2
  exit 1
fi

# Use python-yaml for accurate parsing — pure grep would miss YAML structure.
python3 - "$VALUES" <<'PY'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    v = yaml.safe_load(f)

primary = (v or {}).get("primary", {})
pdb = primary.get("pdb")

if pdb is None:
    print(f"FAIL: primary.pdb missing in {path}", file=sys.stderr)
    print("  required: primary.pdb.create == false (B2 regression)", file=sys.stderr)
    sys.exit(1)

if pdb.get("create") is not False:
    print(f"FAIL: primary.pdb.create must be False, got {pdb.get('create')!r}", file=sys.stderr)
    print("  required: primary.pdb.create == false (B2 regression)", file=sys.stderr)
    sys.exit(1)

print("PASS: primary.pdb.create is false (B2 regression OK)")
PY
