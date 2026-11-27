#!/usr/bin/env bash
# seed-loop-runtime-secrets.sh — Idempotent Vault seed per env.
#
# T8.1 EC-21 mitigation: replace 26+ manual `bao kv put` commands with
# 1 invocation. Reads inventory canonical from
# infra/helm/loop-runtime-external-secrets/values.yaml and `bao kv put` per path
# at `secret/data/loop-runtime/<env>/<binary>/<category>/<vaultKey>`.
#
# Each value is stored as KV-v2 entry `{ value: <secret> }` matching
# remoteRef.property=value em ExternalSecret CRs (Phase 3.2 template).
#
# IDEMPOTENT: `bao kv put` is safe to re-run (writes new version per K-V2,
# no destructive operation). Use --dry-run flag to print actions without
# executing (validates inventory parse).
#
# Usage:
#   bash infra/scripts/openbao/seed-loop-runtime-secrets.sh \
#     --env vault-local \
#     --secret-values-file /path/to/operator-secrets.yaml \
#     [--dry-run]
#
# Pre-flight:
#   - bao CLI in PATH
#   - BAO_ADDR + BAO_TOKEN env vars set (root token or sufficient policy)
#   - BAO_SKIP_VERIFY=true if using self-signed CA (vault-local)
#   - yq + jq in PATH (parse inventory + values file)
#
# Secret values file format (operator-provided, separate from inventory):
#   secrets:
#     api:
#       gitea/token: "actual-gitea-token-value"
#       minio/access_key: "loop-runtime-admin"
#       ...
#     cloud:
#       service_auth/ed25519_private_key: "-----BEGIN PRIVATE KEY-----\n..."
#       ...
#     operators:
#       digitalocean/token: "dop_v1_..."
#       ...
#
# Plan: 2026-06-02-loop-runtime-api-vault-integration v1.8 T8.1 EC-21.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# ----------------------------------------------------------------------------
# CLI args
# ----------------------------------------------------------------------------
ENV=""
SECRET_VALUES_FILE=""
DRY_RUN=0
INVENTORY="${REPO_ROOT:-$(git rev-parse --show-toplevel)}/infra/helm/loop-runtime-external-secrets/values.yaml"

usage() {
  cat <<EOF
seed-loop-runtime-secrets.sh — Idempotent Vault seed per env.

USAGE
  $0 --env <env> --secret-values-file <path> [--dry-run]

REQUIRED
  --env                 LoopRuntime env name (vault-local|standalone-multi|dev|drill|staging|prod)
  --secret-values-file  YAML with actual secret values (operator-provided, gitignored)

OPTIONAL
  --dry-run             Print planned bao kv put commands without executing
  --inventory <path>    Override default inventory path (default: loop-runtime-external-secrets/values.yaml)
  --help                Show this help

PRE-FLIGHT (operator MUST set BEFORE invoking)
  export BAO_ADDR=https://vault.example:8200
  export BAO_TOKEN=<root-or-policy-token>
  [export BAO_SKIP_VERIFY=true]  # if using self-signed CA

EXAMPLES
  # Vault-local (k3d) seed:
  export BAO_ADDR=https://127.0.0.1:8210 BAO_SKIP_VERIFY=true
  export BAO_TOKEN=\$(jq -r .root_token /tmp/openbao-init.json)
  $0 --env vault-local --secret-values-file ~/.loop-runtime-secrets-vault-local.yaml

  # Dry-run validation:
  $0 --env staging --secret-values-file ~/.loop-runtime-secrets-staging.yaml --dry-run
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV="$2"; shift 2 ;;
    --secret-values-file) SECRET_VALUES_FILE="$2"; shift 2 ;;
    --inventory) INVENTORY="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------
if [ -z "$ENV" ]; then echo "ERROR: --env required" >&2; usage; exit 1; fi
if [ -z "$SECRET_VALUES_FILE" ]; then echo "ERROR: --secret-values-file required" >&2; usage; exit 1; fi
if [ ! -f "$INVENTORY" ]; then echo "ERROR: inventory not found: $INVENTORY" >&2; exit 1; fi
if [ ! -f "$SECRET_VALUES_FILE" ]; then echo "ERROR: secret values file not found: $SECRET_VALUES_FILE" >&2; exit 1; fi

for tool in bao yq jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool not in PATH (required)" >&2
    exit 1
  fi
done

if [ "$DRY_RUN" -eq 0 ]; then
  if [ -z "${BAO_ADDR:-}" ]; then echo "ERROR: BAO_ADDR env var required" >&2; exit 1; fi
  if [ -z "${BAO_TOKEN:-}" ]; then echo "ERROR: BAO_TOKEN env var required" >&2; exit 1; fi
  echo "[INFO] BAO_ADDR=$BAO_ADDR" >&2
fi

echo "[INFO] env=$ENV  inventory=$INVENTORY  values=$SECRET_VALUES_FILE  dry-run=$DRY_RUN" >&2

# ----------------------------------------------------------------------------
# Parse inventory: extract (binary, category, vaultKey) tuples + apply
# vaultBinaryOverride per data entry.
# ----------------------------------------------------------------------------
# Build list of (binary, category, vault_key, secret_lookup_path) tuples.
# secret_lookup_path = how to find value in SECRET_VALUES_FILE = .secrets.<binary>.<vaultKey>
TOTAL=0
SEEDED=0
SKIPPED=0
FAILED=0

# Iterate externalSecrets in inventory
while IFS= read -r es_index; do
  es_binary=$(yq ".externalSecrets[$es_index].vaultBinary" "$INVENTORY")
  es_category=$(yq ".externalSecrets[$es_index].vaultCategory" "$INVENTORY")
  data_len=$(yq ".externalSecrets[$es_index].data | length" "$INVENTORY")

  for ((i=0; i<data_len; i++)); do
    vault_key=$(yq ".externalSecrets[$es_index].data[$i].vaultKey" "$INVENTORY")
    override=$(yq ".externalSecrets[$es_index].data[$i].vaultBinaryOverride // \"\"" "$INVENTORY")

    # Resolve effective binary (vaultBinaryOverride > es.vaultBinary)
    binary="$es_binary"
    if [ -n "$override" ] && [ "$override" != "null" ]; then
      binary="$override"
    fi

    full_path="secret/loop-runtime/$ENV/$binary/$es_category/$vault_key"

    # Look up value: secrets.<binary>.<vault_key>
    lookup=".secrets.$binary[\"$vault_key\"] // \"\""
    secret_value=$(yq "$lookup" "$SECRET_VALUES_FILE" 2>/dev/null || echo "")

    TOTAL=$((TOTAL + 1))

    if [ -z "$secret_value" ] || [ "$secret_value" = "null" ]; then
      echo "[WARN] $full_path — value missing in $SECRET_VALUES_FILE under .secrets.$binary.$vault_key — SKIP" >&2
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    # Idempotent: bao kv put writes new version every time (KV-v2 semantics).
    # Skip if same value already at latest version? Cheaper to just write.
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "[DRY-RUN] bao kv put $full_path value=<${#secret_value} chars>"
      SEEDED=$((SEEDED + 1))
    else
      if bao kv put "$full_path" "value=$secret_value" >/dev/null 2>&1; then
        echo "[OK] $full_path"
        SEEDED=$((SEEDED + 1))
      else
        echo "[FAIL] $full_path — bao kv put returned non-zero" >&2
        FAILED=$((FAILED + 1))
      fi
    fi
  done
done < <(yq '.externalSecrets | keys | .[]' "$INVENTORY")

# ----------------------------------------------------------------------------
# Report
# ----------------------------------------------------------------------------
echo "" >&2
echo "[SUMMARY] env=$ENV total=$TOTAL seeded=$SEEDED skipped=$SKIPPED failed=$FAILED" >&2

if [ "$FAILED" -gt 0 ]; then
  exit 2
fi
exit 0
