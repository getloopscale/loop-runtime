#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-dev-public-loopzero-external-secrets-overlay_test.sh — iter 25
# hermetic structural test for the dev-public env overlay
# `infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl`
# (productive-idle iter 25 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2).
#
# Locks the contract that the overlay:
#   - Defaults openbaoTransit.enabled=false (safe — prevents boot
#     failures before runbook step 1+2 complete)
#   - Sets canonical mgmt-cluster service DNS for Option (c)
#   - Wires secretRef to loopzero-openbao-approle in external-secrets ns
#     with key OPENBAO_VAULT_SECRET_ID (matches runbook step 2.1 +
#     iter 20 chart envFrom contract)
#   - role_id is EMPTY placeholder (not a fabricated UUID hardcode)
#   - Documents the 6-box pre-flight that operator MUST complete before
#     flipping enabled=true
#   - References the runbook + iter 24 shortcut + canonical memory pointers
#
# Hermetic — no kubectl, no helm install. Pure grep on overlay file.

set -euo pipefail

OVERLAY="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl"
TMPDIR=$(mktemp -d -t loopzero-eso-overlay-XXXXXX)
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

# A — file exists at canonical path
assert "A1 overlay file exists" "test -f '$OVERLAY'"

# B — SAFE DEFAULT: enabled stays false (operator opt-in post-step-1+2)
assert "B1 openbaoTransit.enabled defaults to false (safe)" \
  "grep -E '^  enabled: false' '$OVERLAY'"

assert "B2 NO line setting enabled: true (anti-foot-shooting)" \
  "! grep -E '^  enabled: true' '$OVERLAY'"

# C — Option (c) canonical service DNS
assert "C1 server is canonical mgmt-cluster in-cluster DNS" \
  "grep 'openbao.vault-system.svc.cluster.local:8200' '$OVERLAY'"

assert "C2 server is https (NOT http)" \
  "grep 'server: \"https://' '$OVERLAY'"

# D — secretRef matches runbook step 2.1 + iter 20 chart envFrom
assert "D1 secretRef.name = loopzero-openbao-approle (matches runbook step 2.1)" \
  "grep 'name: \"loopzero-openbao-approle\"' '$OVERLAY'"

assert "D2 secretRef.namespace = external-secrets (ESO controller ns convention)" \
  "grep 'namespace: \"external-secrets\"' '$OVERLAY'"

assert "D3 secretRef.key = OPENBAO_VAULT_SECRET_ID (matches iter 20 chart envFrom)" \
  "grep 'key: \"OPENBAO_VAULT_SECRET_ID\"' '$OVERLAY'"

# E — ANTI-HARDCODE GUARD: role_id is empty placeholder + caBundle empty
# (no UUID accidentally checked in)
assert "E1 roleId placeholder is empty string (operator pastes post-step-1.3)" \
  "grep -E '^  roleId: \"\"' '$OVERLAY'"

assert "E2 NO fabricated UUID hardcoded as roleId" \
  "! grep -E 'roleId: \"[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\"' '$OVERLAY'"

assert "E3 caBundle placeholder is empty string" \
  "grep -E '^  caBundle: \"\"' '$OVERLAY'"

# F — Documentation: the overlay MUST cite the 6-box pre-flight + runbook
# + iter 24 shortcut + canonical memory pointers so operators reading it
# alone (without the runbook) still see the warnings.
assert "F1 doc cites cutover runbook" \
  "grep 'openbao-transit-cutover-runbook.md' '$OVERLAY'"

assert "F2 doc references iter 24 task k3d:vault:bootstrap-in-mgmt shortcut" \
  "grep 'k3d:vault:bootstrap-in-mgmt' '$OVERLAY'"

assert "F3 doc cites feedback_no_hardcode_gate_2026_06_09 memory" \
  "grep 'feedback_no_hardcode_gate_2026_06_09' '$OVERLAY'"

assert "F4 doc cites feedback_secret_canonical_no_hardcode memory" \
  "grep 'feedback_secret_canonical_no_hardcode' '$OVERLAY'"

# G — Anti-patterns: no Co-Authored-By trailers, no --no-verify references
assert "G1 no Co-Authored-By trailer in comments" \
  "! grep -i 'Co-Authored-By' '$OVERLAY'"

# H — Cross-link to chart's actual `openbaoTransit:` schema (defensive
# typo guard — if someone renames the chart key, overlay's still in
# sync via grep)
assert "H1 overlay uses 'openbaoTransit:' top-level key (matches chart values.yaml schema)" \
  "grep -E '^openbaoTransit:' '$OVERLAY'"

# I — Cross-cluster Open Question awareness
assert "I1 doc mentions Option (a) / (b) cross-cluster variants in trade-off note" \
  "grep -E 'Option \([ab]\)' '$OVERLAY'"

# Summary
echo ""
echo "dev-public loopzero-external-secrets overlay contract: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot dir preserved: $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
