#!/usr/bin/env bash
# infra/tests/scripts/k3d-docker-hub-mirror_test.sh
#
# Regression for B6 (real-paas-cleanroom-dogfood-loop-plan iter 6):
# k3d-loopzero-registry MUST be created with --proxy-remote-url to act as a
# Docker Hub pull-through cache, AND all 4 cluster YAMLs MUST configure
# containerd to mirror docker.io / registry-1.docker.io through it.
# Without these, anonymous Docker Hub rate-limit (100 pulls/6h) blocks
# the cleanroom loop after ~5 destroy/recreate cycles.
#
# Static check (script + YAML grep). Hermetic — no cluster operations.
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
PASS=0
FAIL=0

check() {
  local label=$1 file=$2 pattern=$3
  if grep -qE "$pattern" "$file"; then
    printf '  PASS  %s\n' "$label"
    PASS=$((PASS + 1))
  else
    printf '  FAIL  %s — pattern not found: %s\n' "$label" "$pattern" >&2
    printf '         file: %s\n' "$file" >&2
    FAIL=$((FAIL + 1))
  fi
}

echo "B6 regression: k3d Docker Hub pull-through mirror"
echo

check "registry:create has --proxy-remote-url" \
  "${REPO_ROOT}/taskfiles/k3d.yml" \
  '\-\-proxy-remote-url[[:space:]]+https://registry-1\.docker\.io'

for cluster in mgmt build workload runtime-2; do
  yaml="${REPO_ROOT}/infra/k3d/${cluster}-cluster.yaml"
  check "${cluster}: registries.config block exists" "$yaml" '^[[:space:]]+config:[[:space:]]*\|'
  check "${cluster}: docker.io mirror endpoint" "$yaml" '"docker.io"'
  check "${cluster}: registry-1.docker.io mirror endpoint" "$yaml" '"registry-1.docker.io"'
done

echo
printf '%d/%d checks passed\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
