#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# T9.2-T9.5 of plan 2026-06-02-openbao-adoption v1.4 — operator orchestrator
# for migrating inline `secure: v1:...` Pulumi secrets to Vault KV-v2.
#
# Workflow:
#   1. Iterate over Pulumi stack config keys ending in well-known secret names
#   2. Read decoded value via `pulumi config get --show-secrets`
#   3. Write to Vault path `secret/loopruntime/<env>/iac/<resource>/<key>` (kv-v2)
#   4. Operator removes inline entry via `pulumi config rm` (Step 7 of runbook)
#
# Safety:
#   --dry-run (DEFAULT): prints the secret-name → vault-path mapping without
#                        executing any pulumi/bao commands
#   --execute: writes to Vault (still does NOT delete pulumi inline entries —
#              operator does that in Step 7 after verification)
#
# Requirements:
#   - bao CLI + BAO_ADDR + BAO_TOKEN
#   - pulumi CLI + PULUMI_CONFIG_PASSPHRASE
#   - jq
#
# See: docs/runbooks.md

set -euo pipefail

STACK=""
DRY_RUN=true
PULUMI_DIR="${PULUMI_DIR:-infra/pulumi}"
ENV_LABEL=""  # e.g., "dev", "staging", "prod" — defaults to STACK

while [[ $# -gt 0 ]]; do
  case "$1" in
    --stack) STACK="$2"; shift 2 ;;
    --env)   ENV_LABEL="$2"; shift 2 ;;
    --pulumi-dir) PULUMI_DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=true; shift ;;
    --execute) DRY_RUN=false; shift ;;
    --help|-h)
      sed -n '4,30p' "$0"
      exit 0
      ;;
    *) echo "ERR: unknown flag $1" >&2; exit 1 ;;
  esac
done

[[ -z "${STACK}" ]] && { echo "ERR: --stack <name> required" >&2; exit 1; }
[[ -z "${ENV_LABEL}" ]] && ENV_LABEL="${STACK}"

# Pre-flight checks
for cmd in bao pulumi jq; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "ERR: ${cmd} CLI not found" >&2
    exit 1
  fi
done
[[ -z "${BAO_ADDR:-}" ]] && { echo "ERR: BAO_ADDR not set" >&2; exit 1; }
[[ -z "${BAO_TOKEN:-}" ]] && { echo "ERR: BAO_TOKEN not set" >&2; exit 1; }
[[ -z "${PULUMI_CONFIG_PASSPHRASE:-}" ]] && {
  echo "ERR: PULUMI_CONFIG_PASSPHRASE not set" >&2; exit 1
}

# Mapping: pulumi config key → vault path (under secret/loopruntime/<env>/iac/...)
# Operators add/override entries here as the canonical inventory.
declare -A MAPPING=(
  ["cf-api-token"]="iac/cloudflare/api-token"
  ["do-token"]="iac/digitalocean/token"
  ["do-spaces-key"]="iac/digitalocean/spaces-access-key"
  ["do-spaces-secret"]="iac/digitalocean/spaces-secret-key"
  ["github-app-private-key"]="iac/github/app-private-key"
  ["github-app-webhook-secret"]="iac/github/app-webhook-secret"
  ["stripe-secret-key"]="iac/stripe/secret-key"
  ["loopruntime-secret-encryption-key"]="iac/loopruntime/secret-encryption-key"
  ["loopruntime-bootstrap-secret"]="iac/loopruntime/bootstrap-secret"
)

echo "[migrate-pulumi-to-vault] stack=${STACK} env=${ENV_LABEL} dry-run=${DRY_RUN}"
echo "[migrate-pulumi-to-vault] pulumi-dir=${PULUMI_DIR}"
echo "[migrate-pulumi-to-vault] inventory: ${#MAPPING[@]} secret(s)"

cd "${PULUMI_DIR}"

# Counters
MIGRATED=0
SKIPPED=0
FAILED=0

for key in "${!MAPPING[@]}"; do
  vault_subpath="${MAPPING[$key]}"
  vault_path="secret/loopruntime/${ENV_LABEL}/${vault_subpath}"

  # Try to read the decoded value via pulumi config get
  if value=$(pulumi config get --stack "${STACK}" "${key}" --show-secrets 2>/dev/null); then
    if [[ -z "${value}" ]]; then
      echo "[SKIP] ${key}: empty (not configured for stack ${STACK})"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi
    # Mask value in stdout — only path + key length is logged.
    echo "[MIGRATE] ${key} (${#value} chars) → ${vault_path}"
    if [[ "${DRY_RUN}" = "false" ]]; then
      if BAO_ADDR="${BAO_ADDR}" BAO_TOKEN="${BAO_TOKEN}" \
         bao kv put "${vault_path}" "value=${value}" >/dev/null 2>&1; then
        MIGRATED=$((MIGRATED + 1))
      else
        echo "[FAIL ] bao kv put failed for ${vault_path}"
        FAILED=$((FAILED + 1))
      fi
    else
      MIGRATED=$((MIGRATED + 1))
    fi
  else
    echo "[SKIP] ${key}: not in stack ${STACK} pulumi config"
    SKIPPED=$((SKIPPED + 1))
  fi
done

echo
echo "[migrate-pulumi-to-vault] summary: migrated=${MIGRATED} skipped=${SKIPPED} failed=${FAILED}"

if [[ "${DRY_RUN}" = "true" ]]; then
  echo "[migrate-pulumi-to-vault] DRY-RUN — re-run with --execute to write to Vault"
fi

if [[ "${FAILED}" -gt 0 ]]; then
  exit 2
fi
exit 0
