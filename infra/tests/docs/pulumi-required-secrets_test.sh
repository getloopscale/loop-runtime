#!/usr/bin/env bash
# Hermetic gate: every Pulumi.<env>.yaml stack config must enumerate
# the required secret keys for that environment (per docs/operations/
# credentials-required.md).
#
# Plan 2026-05-21-deep-review-remediation T1.5 + EC-14 + ADR D1.
#
# Background: prior to this gate, Pulumi.prod.yaml + Pulumi.staging.yaml
# shipped with ZERO `secure:` keys. The required secrets were documented
# only in comments. An operator running `pulumi preview --stack prod`
# locally without the secrets would get cryptic provider errors deep in
# resource creation, not a fail-fast "missing required config" error.
#
# This gate REPLACES the original plan T1.5 approach of committing
# encrypted-placeholder ciphertexts (which requires PULUMI_CONFIG_PASSPHRASE
# at commit time + produces per-operator-passphrase fragility). Instead
# we assert the file CONTAINS THE KEY (either as comment OR as `secure:`
# entry); the operator's actual `pulumi up` then fails with a typed
# error if the key has no encrypted value.
#
# EC-14 risk acknowledged: operator without PULUMI_CONFIG_PASSPHRASE will
# still get "failed to decrypt" instead of fail-fast. Mitigated by
# documenting passphrase as pre-requisite in
# docs/operations/credentials-required.md § "Pre-condition for pulumi
# --stack prod".

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }
info() { echo "[pulumi-required-secrets] $*"; }

# --- Required secrets per stack (must match the SoT doc table) ---
declare -A REQUIRED_PROD=(
  ["spacesAccessKeyId"]="DO Spaces (object storage)"
  ["spacesSecretAccessKey"]="DO Spaces (object storage)"
  ["digitaloceanToken"]="DO API + cluster provisioning"
  ["cfApiToken"]="Cloudflare Zone + Worker"
  ["giteaAdminPassword"]="Gitea admin"
  ["minioRootPassword"]="MinIO root (standalone only)"
)

declare -A REQUIRED_STAGING=(
  ["spacesAccessKeyId"]="DO Spaces"
  ["spacesSecretAccessKey"]="DO Spaces"
  ["digitaloceanToken"]="DO API"
  ["cfApiToken"]="Cloudflare"
)

# --- Helper: assert key is listed (as `secure:` entry OR as comment) ---
assert_key_present() {
  local key="$1" file="$2" stack="$3"
  # Match `<key>:` (as config entry) OR `<key>` in a comment.
  # Match: `<key>:` (bare), `<ns>:<key>:` (namespaced), OR `# ... <key> ...` (comment).
  if grep -qE "^\s*${key}:|^\s*[a-z][a-z0-9-]*:${key}:|#.*${key}" "$file"; then
    return 0
  fi
  fail "$stack: required secret '$key' NOT mentioned in $file (neither as config nor as comment)"
  return 1
}

# --- Prod stack ---
PROD_FILE="infra/pulumi/Pulumi.prod.yaml"
if [ -f "$PROD_FILE" ]; then
  info "Validating $PROD_FILE..."
  for key in "${!REQUIRED_PROD[@]}"; do
    assert_key_present "$key" "$PROD_FILE" "prod"
  done
else
  fail "$PROD_FILE missing"
fi

# --- Staging stack ---
STAGING_FILE="infra/pulumi/Pulumi.staging.yaml"
if [ -f "$STAGING_FILE" ]; then
  info "Validating $STAGING_FILE..."
  for key in "${!REQUIRED_STAGING[@]}"; do
    assert_key_present "$key" "$STAGING_FILE" "staging"
  done
else
  fail "$STAGING_FILE missing"
fi

# --- Drill stack ---
# Note: drill uses DIGITALOCEAN_TOKEN via env var (D7' sponsor override
# 2026-05-16), NOT via Pulumi config. So we only require the keys that
# are actually stored in stack config: CF account + Spaces creds.
# DO token is the SAME as prod with project-tag isolation.
DRILL_FILE="infra/pulumi-drill/Pulumi.drill.yaml"
if [ -f "$DRILL_FILE" ]; then
  info "Validating $DRILL_FILE..."
  for key in cfAccountId spacesAccessId spacesSecretKey; do
    assert_key_present "$key" "$DRILL_FILE" "drill"
  done
fi

# --- Cross-check: SoT doc lists same secrets ---
SOT_DOC="docs/operations/credentials-required.md"
if [ ! -f "$SOT_DOC" ]; then
  fail "SoT doc missing: $SOT_DOC (run T1.1 first)"
else
  for key in "${!REQUIRED_PROD[@]}"; do
    if ! grep -qE "\`${key}\`" "$SOT_DOC"; then
      fail "Required secret '$key' missing from SoT doc — update $SOT_DOC"
    fi
  done
fi

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[pulumi-required-secrets] FAIL: $VIOLATIONS violation(s)" >&2
  exit 1
fi

PROD_COUNT="${#REQUIRED_PROD[@]}"
STAGING_COUNT="${#REQUIRED_STAGING[@]}"
echo "[pulumi-required-secrets] PASS — prod($PROD_COUNT) + staging($STAGING_COUNT) + drill(3) secrets all listed in Pulumi.<env>.yaml + SoT doc"
