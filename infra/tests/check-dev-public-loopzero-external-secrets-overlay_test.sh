#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-dev-public-loopzero-external-secrets-overlay_test.sh
# Hermetic structural test for the dev-public env overlay
# `infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl`.
#
# Originally shipped 2026-06-10 (iter 25 of plan
# 2026-06-10-j22-openbao-transit-eso-sync v1.2) as the AppRole-shape
# lock. Rewritten 2026-06-19 (T1.2 of plan dev-public-cross-cluster-secrets
# v1.1, issue #111) for the post-Jeito A canonical contract:
#   - workload identity (Kubernetes-auth) replaces AppRole
#   - no static credential (secret_id) materialized in mgmt
#   - canonical intra-cluster DNS preserves cert SAN (EC-2)
#   - TokenRequest audience matches OpenBao role bound_audiences (EC-5)
#
# Locks the post-T1.2 contract:
#   - openbaoTransit.enabled defaults to false (safe — anti-foot-shooting)
#   - server retains canonical intra-cluster DNS (preserves cert SAN)
#   - auth.kind=kubernetes (NOT appRole) — workload identity, no static cred
#   - auth.kubernetes.role + serviceAccountRef + audiences present
#   - No `roleId:` or `secretRef:` fields (legacy AppRole shape removed)
#   - Traceability: issue #111 / Jeito A / dev-public-cross-cluster-secrets
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

# D — auth.kind=kubernetes (workload identity replaces AppRole per T1.2)
assert "D1 auth.kind set to kubernetes (workload identity)" \
  "grep -E 'kind: \"kubernetes\"' '$OVERLAY'"

assert "D2 auth.kubernetes.role declared (OpenBao role binding)" \
  "grep -E '^      role: ' '$OVERLAY'"

assert "D3 auth.kubernetes.serviceAccountRef.name declared" \
  "grep -E 'name: \"external-secrets\"' '$OVERLAY'"

assert "D4 auth.kubernetes.serviceAccountRef.audiences declared (EC-5 contract)" \
  "grep -E '^        audiences:' '$OVERLAY'"

# E — ANTI-HARDCODE GUARD: legacy AppRole shape REMOVED + caBundle empty.
# The whole point of T1.2 is removing the static secret_id from the
# overlay; presence of `roleId:` or `secretRef:` is a structural regression.
assert "E1 NO roleId field (legacy AppRole shape forbidden post-T1.2)" \
  "! grep -E '^  roleId:' '$OVERLAY'"

assert "E2 NO secretRef block (legacy AppRole shape forbidden post-T1.2)" \
  "! grep -E '^  secretRef:' '$OVERLAY'"

assert "E3 NO fabricated UUID hardcoded anywhere" \
  "! grep -E '\"[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\"' '$OVERLAY'"

assert "E4 caBundle placeholder is empty string" \
  "grep -E '^  caBundle: \"\"' '$OVERLAY'"

# F — Documentation: traceability so the next operator understands
# why the K8s-auth shape exists.
assert "F1 doc cites issue #111 / Jeito A / dev-public-cross-cluster-secrets" \
  "grep -E '(issue #111|Jeito A|dev-public-cross-cluster-secrets)' '$OVERLAY'"

assert "F2 doc cites OpenBao init runbook (sponsor JUNTOS Shamir ceremony)" \
  "grep 'openbao-init-runbook.md' '$OVERLAY'"

assert "F3 doc cites EC-1 (cross-cluster token review requirement)" \
  "grep -E 'EC-1' '$OVERLAY'"

assert "F4 doc cites EC-2 (CoreDNS host-alias preserves cert SAN)" \
  "grep -E 'EC-2' '$OVERLAY'"

# G — Anti-patterns: no Co-Authored-By trailers
assert "G1 no Co-Authored-By trailer in comments" \
  "! grep -i 'Co-Authored-By' '$OVERLAY'"

# H — Cross-link to chart's actual `openbaoTransit:` schema
assert "H1 overlay uses 'openbaoTransit:' top-level key (matches chart values.yaml schema)" \
  "grep -E '^openbaoTransit:' '$OVERLAY'"

# I — Cross-cluster contract awareness
assert "I1 doc cites the separate loopzero-vault cluster topology" \
  "grep -E 'loopzero-vault' '$OVERLAY'"

# Summary
echo ""
echo "dev-public loopzero-external-secrets overlay contract: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot dir preserved: $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
