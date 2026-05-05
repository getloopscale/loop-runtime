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

# B7 (iter 7b refinement): registry is plain registry:2 (no --proxy-remote-url)
# because proxy-mode rejects pushes. Docker Hub bypass moved to containerd
# mirror layer. So we now assert the OPPOSITE of original B6: --proxy-remote-url
# MUST NOT be present.
if grep -qE '\-\-proxy-remote-url' "${REPO_ROOT}/taskfiles/k3d.yml"; then
  printf '  FAIL  registry:create must NOT use --proxy-remote-url (breaks pushes)\n' >&2
  FAIL=$((FAIL + 1))
else
  printf '  PASS  registry:create has no --proxy-remote-url (plain registry, pushes work)\n'
  PASS=$((PASS + 1))
fi

# Negative assertion: registry-1.docker.io must not appear as proxy URL anywhere.
if grep -qE '\-\-proxy-remote-url[[:space:]]+https://registry-1\.docker\.io' "${REPO_ROOT}/taskfiles/k3d.yml"; then
  printf '  FAIL  proxy must NOT point at registry-1.docker.io (rate-limited)\n' >&2
  FAIL=$((FAIL + 1))
else
  printf '  PASS  no registry-1.docker.io reference in proxy config\n'
  PASS=$((PASS + 1))
fi

for cluster in mgmt build workload runtime-2; do
  yaml="${REPO_ROOT}/infra/k3d/${cluster}-cluster.yaml"
  check "${cluster}: registries.config block exists" "$yaml" '^[[:space:]]+config:[[:space:]]*\|'
  check "${cluster}: docker.io mirror endpoint" "$yaml" '"docker.io"'
  check "${cluster}: registry-1.docker.io mirror endpoint" "$yaml" '"registry-1.docker.io"'
  # B7 (iter 7b): mirror endpoint MUST be mirror.gcr.io (Google free mirror),
  # NOT k3d-loopzero-registry (which is now plain pull/push, no proxy mode).
  check "${cluster}: mirror endpoint points at mirror.gcr.io" "$yaml" 'https://mirror\.gcr\.io'
done

echo
printf '%d/%d checks passed\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
