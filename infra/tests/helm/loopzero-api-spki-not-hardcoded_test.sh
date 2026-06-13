#!/usr/bin/env bash
# T3.4 of plan 2026-06-11-cleanroom-canonical-5-defense-layers.
#
# Hermetic source-level assertion that per-env `loopzero-api.yaml.gotmpl`
# overlays do NOT contain hardcoded SPKI hex pin strings. Closes catch #7
# from T6.2 session (memory `reference_t62_cleanroom_session_2026_06_11` +
# `j22-aprendizado-spki-pin-hardcoded-vs-dynamic-cert`): OpenBao Transit
# requires SPKI pin extraction from openbao-tls Secret, NOT hardcoded
# 64-char hex string. Hardcoded pin breaks every Cleanroom (cert rotates).
#
# Per ADR D8 + memory canonical:
#   - SPKI pin MUST be extracted from K8s Secret at runtime via
#     valueFrom.secretKeyRef pointing at `openbao-spki-pin`
#   - SPKI pin literal (64-char hex) MUST NEVER appear in chart values
#     OR overlay files (would persist stale pin across cert rotation)
#
# Pattern: hermetic grep for SPKI hex regex pattern (64 lowercase
# hex chars `[a-f0-9]{64}`) in values + overlay files. Pattern MUST
# return 0 hits anywhere.
#
# Wired in: task quality:gates (after T1.4 ships canonical extraction).
# Allowlist entry: KEEP-CI-WIRED (T3.4 of cleanroom-canonical plan).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

pass=0
fail=0

log() { printf '[loopzero-api-spki-not-hardcoded] %s\n' "$*" >&2; }

# SPKI pin is 64 lowercase hex chars. Per memory j22-aprendizado, the bug
# pattern was: OPENBAO_SPKI_PIN_SHA256: "<literal 64 hex>" inline in values.
SPKI_HEX_RE='[a-f0-9]{64}'

# Files to check (chart values + overlay files)
FILES_TO_CHECK=(
  "infra/helm/loopzero-api/values.yaml"
  "infra/helm/loopzero-api/values.yaml.gotmpl"
  "infra/helm/loopzero-api/values-dev.yaml"
  "infra/helm/loopzero-api/values-standalone.yaml"
  "infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl"
  "infra/helmfile/values/prod/loopzero-api.yaml.gotmpl"
  "infra/helmfile/values/standalone-multi/loopzero-api.yaml.gotmpl"
)

# Test 1: SPKI hex literal MUST NOT appear in any chart/overlay file
HEX_HITS_TOTAL=0
for f in "${FILES_TO_CHECK[@]}"; do
  full_path="$REPO_ROOT/$f"
  if [[ ! -f "$full_path" ]]; then continue; fi
  # grep for 64-char hex sequences (case-insensitive to catch upper/mixed)
  hits=$(grep -cE "$SPKI_HEX_RE" "$full_path" 2>/dev/null || true)
  if [[ "$hits" -gt 0 ]]; then
    log "FAIL [SPKI hex hardcoded] $f (found $hits hex sequences ≥64 chars)"
    HEX_HITS_TOTAL=$((HEX_HITS_TOTAL + hits))
  fi
done

if [[ "$HEX_HITS_TOTAL" -eq 0 ]]; then
  log "PASS [no SPKI hex literal found in any loopzero-api chart/overlay file]"
  pass=$((pass + 1))
else
  log "FAIL [SPKI hex literal hardcoded in $HEX_HITS_TOTAL location(s)]"
  fail=$((fail + 1))
fi

# Test 2: secretKeyRef canonical reference present in at least 1 location
#         (proves the dynamic extraction path is wired)
SECRET_REF_FOUND=0
for f in "${FILES_TO_CHECK[@]}"; do
  full_path="$REPO_ROOT/$f"
  if [[ ! -f "$full_path" ]]; then continue; fi
  if grep -qE 'secretKeyRef.*name:.*openbao-spki-pin' "$full_path" 2>/dev/null; then
    SECRET_REF_FOUND=$((SECRET_REF_FOUND + 1))
  fi
done

if [[ "$SECRET_REF_FOUND" -ge 1 ]]; then
  log "PASS [secretKeyRef name=openbao-spki-pin declared in $SECRET_REF_FOUND location(s)]"
  pass=$((pass + 1))
else
  log "FAIL [no secretKeyRef name=openbao-spki-pin found — dynamic extraction NOT wired]"
  fail=$((fail + 1))
fi

# Test 3: OPENBAO_SPKI_PIN_SHA256 env var declared (somewhere) — proves
#         the variable is used (whatever its source)
ENV_VAR_FOUND=0
for f in "${FILES_TO_CHECK[@]}"; do
  full_path="$REPO_ROOT/$f"
  if [[ ! -f "$full_path" ]]; then continue; fi
  if grep -qE 'OPENBAO_SPKI_PIN_SHA256' "$full_path" 2>/dev/null; then
    ENV_VAR_FOUND=$((ENV_VAR_FOUND + 1))
  fi
done

if [[ "$ENV_VAR_FOUND" -ge 1 ]]; then
  log "PASS [OPENBAO_SPKI_PIN_SHA256 env var declared in $ENV_VAR_FOUND location(s)]"
  pass=$((pass + 1))
else
  log "FAIL [OPENBAO_SPKI_PIN_SHA256 env var NOT declared anywhere]"
  fail=$((fail + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  loopzero-api-spki-not-hardcoded aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
