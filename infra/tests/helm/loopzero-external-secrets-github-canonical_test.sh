#!/usr/bin/env bash
# T3.5 of plan 2026-06-11-cleanroom-canonical-5-defense-layers.
#
# Hermetic source-level assertion that per-env `loopzero-external-secrets.yaml.gotmpl`
# overlays declare canonical ClusterSecretStore + 2 ExternalSecrets per
# ADR D8. Closes catches #9 + #10 from T6.2 session (memories
# `j09-aprendizado-github-creds-via-eso-canonical` + `j23-aprendizado-
# webhook-secret-separate-canonical`):
#   - catch #9: GitHub App credentials chain (5-layer ESO chain) missing
#               in dev-public overlay — autodeploy fails silently
#   - catch #10: `loopzero-github-webhook` Secret must be SEPARATE from
#                `loopzero-api-github` (J-23 lesson) — combined secret causes
#                cross-tenant leakage at webhook delivery boundary
#
# Per ADR D8 canonical contract:
#   - ClusterSecretStore name: `loopzero-vault-secrets` (or env-specific variant)
#   - ExternalSecret #1: `loopzero-api-github` — GitHub App credentials for
#     auto-deploy chain (APP_ID + PRIVATE_KEY + installation_id mapping)
#   - ExternalSecret #2: `loopzero-github-webhook` — Webhook HMAC secret
#     separately so dashboard webhook endpoint isolated from app creds
#
# Pattern: hermetic grep per-env overlay file. Two separate ExternalSecret
# resources MUST be declared (not merged into one).
#
# Wired in: task quality:gates (after T1.5 ships).
# Allowlist entry: KEEP-CI-WIRED (T3.5 of cleanroom-canonical plan).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OVERLAY_DIR="$REPO_ROOT/infra/helmfile/values"

pass=0
fail=0

log() { printf '[loopzero-external-secrets-github-canonical] %s\n' "$*" >&2; }

assert_grep_in_file() {
  local desc="$1" file="$2" pattern="$3"
  if [[ ! -f "$file" ]]; then
    log "FAIL [$desc]: file not found: $file"
    fail=$((fail + 1))
    return
  fi
  if grep -qE "$pattern" "$file"; then
    log "PASS [$desc]"
    pass=$((pass + 1))
  else
    log "FAIL [$desc]: pattern not matched in $(basename "$(dirname "$file")")/$(basename "$file"): $pattern"
    fail=$((fail + 1))
  fi
}

# === Per-env: dev-public + prod (standalone-multi can use defaults — no edge surface) ===
for env in dev-public prod; do
  OVERLAY="$OVERLAY_DIR/$env/loopzero-external-secrets.yaml.gotmpl"

  # (1) ClusterSecretStore canonical
  assert_grep_in_file "$env declares ClusterSecretStore loopzero-vault-secrets" \
    "$OVERLAY" \
    '(kind:.*ClusterSecretStore|loopzero-vault-secrets|clusterSecretStoreRef)'

  # (2) ExternalSecret #1: loopzero-api-github (GitHub App credentials)
  assert_grep_in_file "$env declares ExternalSecret loopzero-api-github (catch #9)" \
    "$OVERLAY" \
    'loopzero-api-github'

  # (3) ExternalSecret #2: loopzero-github-webhook (separate per J-23 lesson)
  assert_grep_in_file "$env declares ExternalSecret loopzero-github-webhook (catch #10 separate)" \
    "$OVERLAY" \
    'loopzero-github-webhook'
done

# === Cross-env regression guard: webhook MUST NOT be merged into loopzero-api-github ===
# Per memory j23-aprendizado-webhook-secret-separate-canonical, the bug was
# embedding webhook HMAC secret inside loopzero-api-github ExternalSecret's data
# map. Canonical: 2 separate ExternalSecret resources.
SEPARATE_WEBHOOK_FOUND=0
for env in dev-public prod; do
  OVERLAY="$OVERLAY_DIR/$env/loopzero-external-secrets.yaml.gotmpl"
  if [[ ! -f "$OVERLAY" ]]; then continue; fi
  # Count distinct `name: loopzero-github-webhook` declarations at metadata level
  # (proxy: each ExternalSecret has its own metadata.name line OR list-item
  # entry `- name: loopzero-github-webhook` in externalSecrets[] / externalSecretsExtra[]).
  webhook_decl=$(grep -cE '^[[:space:]-]*name:[[:space:]]*loopzero-github-webhook' "$OVERLAY" 2>/dev/null || true)
  if [[ "$webhook_decl" -ge 1 ]]; then
    SEPARATE_WEBHOOK_FOUND=$((SEPARATE_WEBHOOK_FOUND + 1))
  fi
done

if [[ "$SEPARATE_WEBHOOK_FOUND" -ge 1 ]]; then
  log "PASS [webhook secret declared as SEPARATE ExternalSecret (per J-23 canonical) in $SEPARATE_WEBHOOK_FOUND env(s)]"
  pass=$((pass + 1))
else
  log "FAIL [webhook secret NOT declared as separate ExternalSecret in any env]"
  fail=$((fail + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  loopzero-external-secrets-github-canonical aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
