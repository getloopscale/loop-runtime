#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic test for `check-drill-outputs-consumers.sh` (Plan 2026-05-16 T1.1.bis).
#
# Validates the EC-02 grep audit script catches legacy consumer patterns
# correctly across all supported file types, while excluding self.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/check-drill-outputs-consumers.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Each test runs in its own tmpdir to ensure isolation.
run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-grep-audit-test-XXXXXX)
  trap "rm -rf '$tmpdir'" RETURN

  local result
  if "$@" "$tmpdir"; then
    result="PASS"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    result="FAIL"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  echo "  [$result] $name"
  rm -rf "$tmpdir"
}

# ---- Test cases ----

test_no_findings_clean_repo() {
  local tmpdir="$1"
  # Empty repo skeleton — script should exit 0
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill/src
  echo 'export const outputs = {};' > infra/pulumi-drill/src/index.ts
  # Adjust ROOT for testing
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

test_catches_legacy_pulumi_stack_output() {
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill .github/workflows
  # Fake consumer with legacy pattern
  cat > .github/workflows/fake-workflow.yml <<EOF
name: fake
jobs:
  test:
    steps:
      - run: pulumi stack output cluster
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1  # should FAIL (exit 1) but didn't
  else
    return 0  # correctly detected legacy
  fi
}

test_catches_legacy_clusterKubeconfig() {
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill scripts
  cat > scripts/fake.sh <<EOF
#!/bin/bash
KUBECONFIG=\$(pulumi stack output clusterKubeconfig)
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1
  else
    return 0
  fi
}

test_accepts_new_schema() {
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill .github/workflows
  cat > .github/workflows/good-workflow.yml <<EOF
name: good
jobs:
  test:
    steps:
      - run: |
          MGMT=\$(pulumi stack output mgmtCluster)
          BUILD=\$(pulumi stack output buildCluster)
          RUNTIME=\$(pulumi stack output runtimeCluster)
          RUNTIME2=\$(pulumi stack output runtimeCluster2)
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 0  # new schema, no findings
  else
    return 1
  fi
}

test_excludes_self_script() {
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  # Only the script itself (which contains pattern as documentation)
  # No other consumers — script should exit 0 (self-references filtered)
  mkdir -p infra/pulumi-drill
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

# ---- v1 nested cluster-map detection (L2-unlock plan T1.3 TDD) ----

test_catches_v1_nested_mgmtCluster_kubeconfig() {
  # TDD spec: fixture with `.outputs.mgmtCluster.kubeconfig` → exit 1
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill .github/workflows
  cat > .github/workflows/fake-v1.yml <<EOF
name: fake-v1
jobs:
  test:
    steps:
      - run: pulumi stack output --json | jq -r '.outputs.mgmtCluster.kubeconfig'
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1  # should FAIL but didn't catch v1
  else
    return 0  # correctly detected v1 nested
  fi
}

test_catches_v1_nested_pg_host() {
  # v1 `.outputs.pg.host` → exit 1
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill scripts
  cat > scripts/fake-pg.sh <<EOF
#!/bin/bash
PG_HOST=\$(pulumi stack output --json | jq -r '.outputs.pg.host')
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1
  else
    return 0
  fi
}

test_accepts_v2_named_exports() {
  # TDD spec: fixture with `pulumi stack output mgmtClusterKubeconfig` → exit 0
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill .github/workflows
  cat > .github/workflows/good-v2.yml <<EOF
name: good-v2
jobs:
  test:
    steps:
      - run: |
          KC=\$(pulumi stack output --stack drill mgmtClusterKubeconfig --show-secrets)
          PG=\$(pulumi stack output --stack drill pgHost)
          BUCKET=\$(pulumi stack output --stack drill bucketBuilds)
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

# ---- Run all tests ----

echo "Running hermetic tests for check-drill-outputs-consumers.sh..."
echo ""

test_catches_multiline_jq_cluster_concat() {
  # Iter 5 discovery: taskfiles/drill.yml had multi-line jq with `" + .mgmtCluster.endpoint`
  # pattern that escaped the v1 grep (because .outputs. was on a separate line).
  # New pattern catches the bare jq field concatenation form.
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill scripts
  cat > scripts/fake-jq-concat.yml <<EOF
tasks:
  legacy:
    cmds:
      - |
        npx pulumi stack output --json | jq -r '
          .outputs |
          "  mgmt: " + .mgmtCluster.endpoint,
          "  build: " + .buildCluster.endpoint
        '
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1  # should FAIL (exit 1) but didn't catch jq concat
  else
    return 0  # correctly detected
  fi
}

test_catches_multiline_jq_pg_concat() {
  # `" + .pg.host` jq concat pattern
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill scripts
  cat > scripts/fake-pg-jq.sh <<EOF
#!/bin/bash
PG=\$(npx pulumi stack output --json | jq -r '"  pg: " + .pg.host')
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1
  else
    return 0
  fi
}

test_catches_phantom_apiUrl_output() {
  # Iter 7 discovery: drill-l2-scale.yml read `pulumi stack output apiUrl` —
  # apiUrl is NOT in any drill schema version (v0/v1/v2). Returned empty
  # silently. Catches operator-believed-exists outputs.
  local tmpdir="$1"
  cd "$tmpdir"
  cp "$SCRIPT" .
  mkdir -p infra/pulumi-drill .github/workflows
  cat > .github/workflows/fake-phantom.yml <<EOF
name: phantom
jobs:
  test:
    steps:
      - run: API_URL=\$(npx pulumi stack output apiUrl --stack drill)
EOF
  if SCAN_ROOT="$tmpdir" "$tmpdir/check-drill-outputs-consumers.sh" > /dev/null 2>&1; then
    return 1  # should FAIL but didn't catch phantom
  else
    return 0
  fi
}

run_test "no_findings_clean_repo" test_no_findings_clean_repo
run_test "catches_legacy_pulumi_stack_output" test_catches_legacy_pulumi_stack_output
run_test "catches_legacy_clusterKubeconfig" test_catches_legacy_clusterKubeconfig
run_test "accepts_new_schema" test_accepts_new_schema
run_test "excludes_self_script" test_excludes_self_script
run_test "catches_v1_nested_mgmtCluster_kubeconfig" test_catches_v1_nested_mgmtCluster_kubeconfig
run_test "catches_v1_nested_pg_host" test_catches_v1_nested_pg_host
run_test "accepts_v2_named_exports" test_accepts_v2_named_exports
run_test "catches_multiline_jq_cluster_concat" test_catches_multiline_jq_cluster_concat
run_test "catches_multiline_jq_pg_concat" test_catches_multiline_jq_pg_concat
run_test "catches_phantom_apiUrl_output" test_catches_phantom_apiUrl_output

echo ""
echo "Total: $TESTS_RUN | Passed: $TESTS_PASSED | Failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
