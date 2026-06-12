#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# loopzero-api-openbao-approle-envfrom_test.sh — T6.1 hermetic render test
# of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2.
#
# Locks the contract that the loopzero-api chart unconditionally wires the
# AppRole secret_id from K8s Secret `loopzero-openbao-approle` via envFrom:
# secretRef with optional=true. Defense against the canonical drift
# mode (inline-literal secret_id reintroduced via "kubectl set env" on
# a hot-fix branch) caught canonically by `feedback_no_hardcode_gate_
# 2026_06_09` + `feedback_secret_canonical_no_hardcode`.
#
# Hermetic — no kubectl, no helm install. Pipes `helm template` into
# grep assertions. Run via `task quality:gates` or directly.

set -euo pipefail

CHART="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/helm/loopzero-api"
TMPDIR=$(mktemp -d -t loopzero-api-openbao-envfrom-XXXXXX)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

PASS=0
FAIL=0
assert() {
  local desc="$1" cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "FAIL: $desc"
    FAIL=$((FAIL+1))
  fi
}

# Render with defaults; the envFrom block is unconditional.
helm template loopzero-api "$CHART" -f "$CHART/values.yaml" > "$TMPDIR/render.yaml" 2>&1 || {
  echo "FAIL: helm template failed"
  cat "$TMPDIR/render.yaml"
  exit 1
}

# A — envFrom block contains the new secretRef
assert "A1 envFrom references loopzero-openbao-approle secret" \
  "grep -q 'name: loopzero-openbao-approle' '$TMPDIR/render.yaml'"

assert "A2 loopzero-openbao-approle secretRef is optional=true (graceful when absent)" \
  "awk '/name: loopzero-openbao-approle/,/optional:/' '$TMPDIR/render.yaml' | grep -q 'optional: true'"

# B — comment block cites the plan + the runbook (drift defense)
assert "B1 chart comment cites plan 2026-06-10-j22-openbao-transit-eso-sync" \
  "grep -q '2026-06-10-j22-openbao-transit-eso-sync' '$TMPDIR/render.yaml'"

assert "B2 chart comment points operator at the cutover runbook" \
  "grep -q 'openbao-transit-cutover-runbook.md' '$TMPDIR/render.yaml'"

# C — the secret name itself is canonical (anti-hardcode gate: no literal
#     value: in the rendered output for OPENBAO_VAULT_SECRET_ID — secret_id
#     only flows via envFrom, never inline)
assert "C1 NO inline 'value:' for OPENBAO_VAULT_SECRET_ID (anti-hardcode)" \
  "! grep -B0 -A1 'name: OPENBAO_VAULT_SECRET_ID' '$TMPDIR/render.yaml' | grep -q 'value:'"

# D — the existing canonical envFrom block siblings still render (regression
#     defense: the new entry shouldn't have displaced or commented out
#     loopzero-service-auth / loopzero-api-minio)
assert "D1 loopzero-service-auth envFrom still present" \
  "grep -q 'name: loopzero-service-auth' '$TMPDIR/render.yaml'"

# E — anti-patterns + cross-links (plan attribution + memory pointers
#     surface in the chart comment block; ensures future grep+blame can
#     trace the wire)
assert "E1 chart comment block references the secret-canonical memory" \
  "grep -q 'feedback_secret_canonical_no_hardcode' '$TMPDIR/render.yaml'"

# Summary + exit code contract
echo ""
echo "openbao-approle envFrom render: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "render snapshot saved at: $TMPDIR (preserved for diagnostics)"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
