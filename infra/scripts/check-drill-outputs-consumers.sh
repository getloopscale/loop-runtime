#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T1.1.bis (EC-02 fix): Grep audit for drill outputs consumers.
#
# T1.1 changes pulumi-drill outputs schema from:
#   { cluster, clusterKubeconfig, ... }                    (singular, legacy)
# to:
#   { mgmtCluster: {...}, buildCluster: {...},
#     runtimeCluster: {...}, runtimeCluster2: {...}, ... }  (cluster map)
#
# EC-02 catastrophic risk: if any consumer NOT updated atomically with T1.1,
# `pulumi stack output cluster` returns undefined → kubectl uses default
# kubeconfig context → DEFAULT MAY BE PROD (see finding-2026-05-16-drill-
# teardown-kubeconfig-orphan.md, where drill teardown left DOKS as
# current-context). Drill operations could accidentally touch prod cluster.
#
# This script greps the repo for known patterns indicating legacy consumption.
# Used as pre-commit hook + CI gating step. Exits 1 if legacy patterns found
# outside pulumi-drill itself, prompting operator to update all consumers in
# the same PR as T1.1 changes.

set -euo pipefail

# ROOT defaults to repo root (script lives in infra/scripts/).
# Tests override via SCAN_ROOT env var for hermetic isolation.
if [[ -n "${SCAN_ROOT:-}" ]]; then
  ROOT="$SCAN_ROOT"
else
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
cd "$ROOT"

# Patterns that indicate LEGACY drill outputs consumption.
# Per L2-unlock plan T1.3: must detect BOTH legacy schemas (v0 + v1).
#
# Three schema versions exist historically (only v2 is valid going forward):
#
#   v0 (single-cluster legacy, pre-iter-1):
#     pulumi stack output cluster       → singular access (no suffix)
#     .outputs.cluster                   → JSON cluster access
#     DRILL_CLUSTER_KUBECONFIG           → env var (no longer used)
#
#   v1 (cluster-map post-iter-1, BANNED post-L2-unlock T1.2):
#     .outputs.mgmtCluster.kubeconfig    → object-literal nested access
#     .outputs.buildCluster.endpoint     → ditto
#     .outputs.runtimeCluster.X          → ditto
#     .outputs.runtimeCluster2.X         → ditto
#     pulumi stack output outputs        → access the wrapping object (legacy)
#
#   v2 (FLAT NAMED EXPORTS — current):
#     pulumi stack output mgmtClusterKubeconfig   → direct named output access
#     pulumi stack output buildClusterEndpoint    → ditto
#     etc. (29 named exports per infra/pulumi-drill/src/index.ts)
#
# Each pattern is a regex; matches outside pulumi-drill itself are findings.
LEGACY_PATTERNS=(
  # v0 singular legacy
  'pulumi stack output cluster\b(?!Kubeconfig|Map|Endpoint)'   # bare `cluster` (singular)
  'pulumi stack output clusterKubeconfig'                       # legacy singular kubeconfig
  '\bstack[._]output\.cluster\b'                                # JSON.parse(...).cluster (singular)
  '\bDRILL_CLUSTER_KUBECONFIG\b'                                # legacy env var name
  # v0+v1 .outputs.X access (BANNED — v2 uses direct named exports)
  '\.outputs\.(mgmt|build|runtime|runtimeCluster2?)Cluster2?\.'  # .outputs.mgmtCluster.kubeconfig etc. (v1)
  '\.outputs\.cluster\b(?!Map)'                                  # .outputs.cluster (v0 singular)
  '\.outputs\.bucket\b'                                          # legacy .outputs.bucket (v0 singular)
  '\.outputs\.pg\.'                                              # v1 .outputs.pg.host etc.
  '\.outputs\.buckets\.'                                         # v1 .outputs.buckets.builds etc.
  '\.outputs\.zone\.'                                            # v1 .outputs.zone.id etc.
  '\.outputs\.vpc\.'                                             # v1 .outputs.vpc.id etc.
)

# Directories to exclude from scan (legacy source allowed to retain patterns
# during transition; consumers must update).
EXCLUDE_DIRS=(
  'node_modules'
  'dist'
  'build'
  '.git'
  '.terraform'
  'vendor'
  '.claude/knowledge-base'  # docs may reference legacy via deliberate quote
  'infra/pulumi-drill'      # source itself — patterns OK in source, audit checks consumers
)

# Build grep --exclude-dir args
EXCLUDE_ARGS=()
for dir in "${EXCLUDE_DIRS[@]}"; do
  EXCLUDE_ARGS+=("--exclude-dir=$dir")
done

# File globs to scan
SCAN_GLOBS=(
  '--include=*.sh'
  '--include=*.yml'
  '--include=*.yaml'
  '--include=*.go'
  '--include=*.ts'
  '--include=*.tsx'
  '--include=*.js'
  '--include=Taskfile.yml'
)

# Exclude self by basename (this script's pattern documentation triggers false-positives)
SELF="check-drill-outputs-consumers.sh"
SELF_TEST="check-drill-outputs-consumers_test.sh"

EXIT_CODE=0
FINDINGS=0

# Use grep -P for perl regex (negative lookahead support)
for pattern in "${LEGACY_PATTERNS[@]}"; do
  echo "Scanning pattern: $pattern" >&2
  # `grep -rPn` returns 0 if matches, 1 if no matches, 2 if error.
  # We treat matches as findings (set EXIT_CODE=1).
  if MATCHES=$(grep -rPn "$pattern" "${SCAN_GLOBS[@]}" "${EXCLUDE_ARGS[@]}" . 2>/dev/null); then
    # Filter out self-references (script + its test contain pattern as documentation)
    FILTERED=$(echo "$MATCHES" | grep -v "$SELF" | grep -v "$SELF_TEST" || true)
    if [[ -n "$FILTERED" ]]; then
      EXIT_CODE=1
      FINDINGS=$((FINDINGS + $(echo "$FILTERED" | wc -l)))
      echo "" >&2
      echo "::warning title=Legacy drill outputs consumer::" >&2
      echo "$FILTERED" >&2
      echo "" >&2
    fi
  fi
done

if [[ $EXIT_CODE -eq 0 ]]; then
  echo ""
  echo "✓ No legacy drill outputs consumers found. Safe to merge L2-unlock T1.2/T1.3."
  exit 0
else
  echo ""
  echo "✗ Found $FINDINGS legacy consumer reference(s)."
  echo ""
  echo "Action required:"
  echo "  Each match above MUST be updated to use the v2 FLAT NAMED EXPORTS schema."
  echo ""
  echo "  Schema versions:"
  echo "    v0 (BANNED): pulumi stack output cluster"
  echo "    v1 (BANNED): .outputs.mgmtCluster.kubeconfig (object-literal nested)"
  echo "    v2 (CURRENT): pulumi stack output mgmtClusterKubeconfig (flat named)"
  echo ""
  echo "  Migration:"
  echo "    OLD v1: .outputs.mgmtCluster.kubeconfig"
  echo "    NEW v2: pulumi stack output --stack drill mgmtClusterKubeconfig --show-secrets"
  echo ""
  echo "    OLD v1: .outputs.pg.host"
  echo "    NEW v2: pulumi stack output --stack drill pgHost"
  echo ""
  echo "    OLD v1: .outputs.buckets.builds"
  echo "    NEW v2: pulumi stack output --stack drill bucketBuilds"
  echo ""
  echo "  Per L2-unlock plan T1.3 + EC-02 (parent plan T1.1.bis), all consumers"
  echo "  MUST update atomically with T1.2 schema migration to prevent kubeconfig="
  echo "  undefined fallback to default kubectl context (which may be prod"
  echo "  after a prior drill teardown — see finding-2026-05-16-drill-"
  echo "  teardown-kubeconfig-orphan.md)."
  echo ""
  echo "  RCA: .claude/knowledge-base/concepts/operations/finding-2026-05-16-pulumi-outputs-rca.md"
  exit 1
fi
