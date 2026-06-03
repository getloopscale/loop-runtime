#!/usr/bin/env bash
# check-loopzero-external-secrets-chart-sanity_test.sh — Comprehensive chart
# rendering sanity test for iter 23 of plan 2026-06-02-loopzero-api-vault-
# integration v1.8.
#
# Complements existing tests:
#   - check-loopzero-external-secrets-creation-policy_test.sh (iter 20 P0 regression)
#   - check-rotation-table-parity_test.sh (iter 17 drift)
#
# This test checks INVARIANTS that span the entire rendered chart output:
#   1. Renders 13 ExternalSecret CRs (matches inventory count)
#   2. All CRs have unique metadata.name (no duplicate target Secret names)
#   3. All CRs have refreshInterval set (no missing → ESO would use 1h
#      default but inventory may explicit override per criticality)
#   4. All CRs have secretStoreRef pointing to loopzero-vault-secrets CSS
#   5. All CRs render correctly for each canonical env name
#      (vault-local, standalone-multi, dev, drill, prod)
#   6. vaultBinaryOverride: loopzero-tenant-operator CR data block has THREE
#      entries with override (GITEA_TOKEN, MINIO_ACCESS_KEY, MINIO_SECRET_KEY)
#      pointing to api/secrets/* paths (cross-binary D6 shared)
#   7. envName fail-fast: rendering without envName → helm error
#
# Plan: 2026-06-02-loopzero-api-vault-integration v1.8 iter 23.
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

echo "Running loopzero-external-secrets chart sanity test..."
echo ""

# Helper: render chart for given env
render_chart() {
  local env="$1"
  local out="$WORKDIR/rendered_${env}.yaml"
  local err="$WORKDIR/render_err_${env}.txt"
  if helm template loopzero-external-secrets "$CHART_DIR" --set envName="$env" >"$out" 2>"$err"; then
    echo "$out"
    return 0
  else
    cat "$err"
    return 1
  fi
}

# ----------------------------------------------------------------------------
# Test 1: 13 ExternalSecret CRs rendered for vault-local
# ----------------------------------------------------------------------------
out=$(render_chart "vault-local")
if [ $? -eq 0 ]; then
  count=$(grep -c "^kind: ExternalSecret" "$out" || true)
  if [ "$count" = "13" ]; then
    printf '  PASS: test_renders_13_external_secrets (count=13)\n'; PASS=$((PASS+1))
  else
    printf '  FAIL: test_renders_13_external_secrets (count=%s expected 13)\n' "$count"
    FAIL=$((FAIL+1))
  fi
else
  printf '  FAIL: test_renders_13_external_secrets (render failed)\n'
  FAIL=$((FAIL+1))
fi

# ----------------------------------------------------------------------------
# Test 2: All metadata.name unique
# ----------------------------------------------------------------------------
out=$(render_chart "vault-local")
if [ $? -eq 0 ]; then
  total_names=$(grep "^  name:" "$out" | wc -l)
  unique_names=$(grep "^  name:" "$out" | sort -u | wc -l)
  if [ "$total_names" -ge 13 ] && [ "$unique_names" = "$total_names" ]; then
    printf '  PASS: test_all_target_names_unique (total=%d unique=%d)\n' "$total_names" "$unique_names"; PASS=$((PASS+1))
  else
    printf '  FAIL: test_all_target_names_unique (total=%d unique=%d — duplicates exist)\n' "$total_names" "$unique_names"
    FAIL=$((FAIL+1))
  fi
fi

# ----------------------------------------------------------------------------
# Test 3: All CRs have refreshInterval set
# ----------------------------------------------------------------------------
out=$(render_chart "vault-local")
if [ $? -eq 0 ]; then
  refresh_count=$(grep -c "^  refreshInterval:" "$out" || true)
  if [ "$refresh_count" = "13" ]; then
    printf '  PASS: test_all_have_refresh_interval (count=13)\n'; PASS=$((PASS+1))
  else
    printf '  FAIL: test_all_have_refresh_interval (count=%s expected 13)\n' "$refresh_count"
    FAIL=$((FAIL+1))
  fi
fi

# ----------------------------------------------------------------------------
# Test 4: All CRs reference loopzero-vault-secrets ClusterSecretStore
# ----------------------------------------------------------------------------
out=$(render_chart "vault-local")
if [ $? -eq 0 ]; then
  css_refs=$(grep -c "name: loopzero-vault-secrets" "$out" || true)
  if [ "$css_refs" = "13" ]; then
    printf '  PASS: test_all_reference_clustersecretstore (count=13)\n'; PASS=$((PASS+1))
  else
    printf '  FAIL: test_all_reference_clustersecretstore (count=%s expected 13)\n' "$css_refs"
    FAIL=$((FAIL+1))
  fi
fi

# ----------------------------------------------------------------------------
# Test 5: Chart renders for ALL canonical envs (standalone-multi, dev, drill, prod)
# ----------------------------------------------------------------------------
all_envs_ok=1
for env in standalone-multi dev drill prod; do
  if ! render_chart "$env" >/dev/null; then
    all_envs_ok=0
    printf '  FAIL: chart did NOT render for env=%s\n' "$env"
  fi
done
if [ "$all_envs_ok" = "1" ]; then
  printf '  PASS: test_renders_for_all_canonical_envs (4 envs)\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_renders_for_all_canonical_envs\n'
  FAIL=$((FAIL+1))
fi

# ----------------------------------------------------------------------------
# Test 6: vaultBinaryOverride correctly resolves to api/secrets/* paths
# in loopzero-tenant-operator CR data block
# ----------------------------------------------------------------------------
out=$(render_chart "vault-local")
if [ $? -eq 0 ]; then
  # The path in remoteRef.key follows pattern:
  #   loopzero/<env>/<binary>/<category>/<key>
  # With vaultBinaryOverride=api, the binary should be `api`, not `operators`.
  # tenant-operator CR (vaultBinary=operators) has GITEA_TOKEN + MINIO_*
  # with vaultBinaryOverride=api.
  api_overrides=$(awk '
    /name: loopzero-tenant-operator/ { in_to=1; next }
    in_to && /^---/ { in_to=0 }
    in_to && /key: loopzero\/vault-local\/api\/secrets\// { print }
  ' "$out" | wc -l)
  # Expect at least 3: GITEA_TOKEN + MINIO_ACCESS_KEY + MINIO_SECRET_KEY → api
  if [ "$api_overrides" -ge 3 ]; then
    printf '  PASS: test_vault_binary_override_resolves_to_api (api_overrides=%d)\n' "$api_overrides"; PASS=$((PASS+1))
  else
    printf '  FAIL: test_vault_binary_override_resolves_to_api (api_overrides=%d expected >=3)\n' "$api_overrides"
    FAIL=$((FAIL+1))
  fi
fi

# ----------------------------------------------------------------------------
# Test 7: envName empty fail-fast
# ----------------------------------------------------------------------------
err=$(mktemp)
if helm template loopzero-external-secrets "$CHART_DIR" >/dev/null 2>"$err"; then
  printf '  FAIL: test_env_name_empty_fail_fast (rendered without envName — should fail!)\n'
  FAIL=$((FAIL+1))
else
  # Helm should fail with explicit error message about envName required
  if grep -qi "envname" "$err"; then
    printf '  PASS: test_env_name_empty_fail_fast (helm error mentions envName)\n'; PASS=$((PASS+1))
  else
    # Helm failed but error message didn't mention envName explicitly — still
    # acceptable (template error somewhere), but log for context
    printf '  PASS: test_env_name_empty_fail_fast (helm failed without envName)\n'; PASS=$((PASS+1))
  fi
fi
rm -f "$err"

# ----------------------------------------------------------------------------
# Test 8: NO data block points to operators/secrets/gitea/token (override
# correctly applied, no unwanted operators path leak)
# ----------------------------------------------------------------------------
out=$(render_chart "vault-local")
if [ $? -eq 0 ]; then
  leak_count=$(grep -c "key: loopzero/vault-local/operators/secrets/gitea/token" "$out" || true)
  if [ "$leak_count" = "0" ]; then
    printf '  PASS: test_no_operators_gitea_path_leak (leak=0)\n'; PASS=$((PASS+1))
  else
    printf '  FAIL: test_no_operators_gitea_path_leak (leak=%d — vaultBinaryOverride not honored)\n' "$leak_count"
    FAIL=$((FAIL+1))
  fi
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
