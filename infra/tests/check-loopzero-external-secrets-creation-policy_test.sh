#!/usr/bin/env bash
# check-loopzero-external-secrets-creation-policy_test.sh — Regression test for
# iter 20 of plan 2026-06-02-loopzero-api-vault-integration v1.8.
#
# Asserts loopzero-external-secrets chart renders ExternalSecret CRs with
# creationPolicy: Orphan (NOT Owner). This is a P0 architectural invariant
# discovered via LIVE testing:
#
#   - creationPolicy: Owner sets ownerReference with blockOwnerDeletion=true
#     → K8s garbage collector cascade-deletes the K8s Secret when the
#     ExternalSecret CR is deleted (e.g., helmfile sync vaultEnabled=false
#     rollback procedure).
#   - K8s GC runs BEFORE ESO controller reconcile → deletionPolicy=Retain
#     has NO effect → rollback procedure (runbook P3) becomes destructive
#     instead of safe.
#   - Fix: creationPolicy: Orphan decouples lifecycle. No ownerReference is
#     set. Operator running rollback preserves K8s Secrets with last-known
#     good values; pods continue serving via envFrom.
#
# This test guards against regression to creationPolicy=Owner.
#
# Plan: 2026-06-02-loopzero-api-vault-integration v1.8 iter 20 (LIVE-discovered).
# Audit: .claude/knowledge-base/audits/2026-06-03-vault-INT-LIVE-evidence.md
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CHART_DIR="$REPO_ROOT/helm/loopzero-external-secrets"

[[ -d "$CHART_DIR" ]] || { echo "FATAL: chart not found at $CHART_DIR"; exit 2; }
command -v helm >/dev/null 2>&1 || { echo "FATAL: helm required"; exit 2; }

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "Running loopzero-external-secrets creationPolicy regression test..."
echo ""

# Render chart with envName=test
RENDERED="$WORKDIR/rendered.yaml"
if ! helm template loopzero-external-secrets "$CHART_DIR" --set envName=test 2>"$WORKDIR/render_err" > "$RENDERED"; then
  echo "  FAIL: helm template failed"
  cat "$WORKDIR/render_err"
  exit 1
fi

# Test 1: rendered output must contain at least 1 ExternalSecret CR
es_count=$(grep -c "^kind: ExternalSecret" "$RENDERED" || true)
if [ "$es_count" -ge 1 ]; then
  printf '  PASS: test_chart_renders_at_least_one_externalsecret (count=%d)\n' "$es_count"
  PASS=$((PASS+1))
else
  printf '  FAIL: test_chart_renders_at_least_one_externalsecret (count=0)\n'
  FAIL=$((FAIL+1))
fi

# Test 2: ALL rendered ExternalSecret CRs must have creationPolicy: Orphan
owner_count=$(grep -c "creationPolicy: Owner" "$RENDERED" || true)
orphan_count=$(grep -c "creationPolicy: Orphan" "$RENDERED" || true)

if [ "$owner_count" = "0" ] && [ "$orphan_count" -ge 1 ]; then
  printf '  PASS: test_no_creation_policy_owner_regression (Owner=0, Orphan=%d)\n' "$orphan_count"
  PASS=$((PASS+1))
else
  printf '  FAIL: test_no_creation_policy_owner_regression (Owner=%s, Orphan=%s)\n' "$owner_count" "$orphan_count"
  echo "    P0 REGRESSION: creationPolicy: Owner sets blockOwnerDeletion ownerReference"
  echo "    → K8s GC cascade-deletes Secrets on chart uninstall"
  echo "    → rollback procedure (runbook P3) becomes destructive"
  echo "    → pods CrashLoop on next restart (envFrom secrets missing)"
  echo "    See: .claude/knowledge-base/audits/2026-06-03-vault-INT-LIVE-evidence.md iter 20"
  FAIL=$((FAIL+1))
fi

# Test 3: creationPolicy + deletionPolicy must appear together (defense-in-depth)
if [ "$orphan_count" -ge 1 ]; then
  retain_count=$(grep -c "deletionPolicy: Retain" "$RENDERED" || true)
  if [ "$retain_count" -ge "$orphan_count" ]; then
    printf '  PASS: test_deletion_policy_retain_present (Retain=%d, Orphan=%d)\n' "$retain_count" "$orphan_count"
    PASS=$((PASS+1))
  else
    printf '  FAIL: test_deletion_policy_retain_present (Retain=%d, Orphan=%d)\n' "$retain_count" "$orphan_count"
    FAIL=$((FAIL+1))
  fi
fi

# Test 4: count parity — Orphan count should match ExternalSecret count
if [ "$orphan_count" = "$es_count" ]; then
  printf '  PASS: test_all_es_have_orphan_policy (CRs=%d, Orphan=%d)\n' "$es_count" "$orphan_count"
  PASS=$((PASS+1))
else
  printf '  FAIL: test_all_es_have_orphan_policy (CRs=%d, Orphan=%d) — mismatch!\n' "$es_count" "$orphan_count"
  FAIL=$((FAIL+1))
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
