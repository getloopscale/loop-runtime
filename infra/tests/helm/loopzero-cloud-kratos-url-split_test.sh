#!/usr/bin/env bash
# T3.2 of plan 2026-06-11-cleanroom-canonical-5-defense-layers.
#
# Hermetic source-level assertion that per-env `loopzero-cloud.yaml.gotmpl`
# overlays declare the KRATOS_PUBLIC_URL vs KRATOS_INTERNAL_URL canonical
# split per ADR D8. Closes catch #20 from T6.2 session
# (memory reference_t62_cleanroom_session_2026_06_11): loopzero-cloud receives
# Kratos public URL where it needs the internal one (and vice-versa)
# causing redirect loops in J-05 device flow + session-cookie domain
# mismatch in J-04 recovery.
#
# Per-env contract (per ADR D8):
#   - standalone-multi: ambos podem ser cluster-internal (no edge surface).
#                       Overlay MAY be empty OR declare both as
#                       cluster-internal (regression guard: must NOT
#                       contain public FQDN).
#   - dev-public: KRATOS_PUBLIC_URL = `https://auth-dev.loopzero.example/*`
#                 KRATOS_INTERNAL_URL = `http://kratos.loopzero-auth.svc.cluster.local:4433`
#                 (per T1.2 + canonical contract from J-09 + J-22 LIVE chain)
#   - prod: KRATOS_PUBLIC_URL = `https://auth.loopzero.example/*` (NO -dev)
#           KRATOS_INTERNAL_URL = `http://kratos.loopzero-auth.svc.cluster.local:4433`
#
# Pattern: hermetic per-env source grep (mirrors T3.1 canonical pattern).
#
# Wired in: task quality:gates (after T1.2 ships).
# Allowlist entry: KEEP-CI-WIRED (T3.2 of cleanroom-canonical plan).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OVERLAY_DIR="$REPO_ROOT/infra/helmfile/values"

pass=0
fail=0

log() { printf '[loopzero-cloud-kratos-url-split] %s\n' "$*" >&2; }

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

assert_no_grep_in_file() {
  local desc="$1" file="$2" pattern="$3"
  if [[ ! -f "$file" ]]; then
    log "FAIL [$desc]: file not found: $file"
    fail=$((fail + 1))
    return
  fi
  if grep -qE "$pattern" "$file"; then
    log "FAIL [$desc]: forbidden pattern matched in $(basename "$(dirname "$file")")/$(basename "$file"): $pattern"
    fail=$((fail + 1))
  else
    log "PASS [$desc]"
    pass=$((pass + 1))
  fi
}

# === dev-public env ===
DEV_PUBLIC_OVERLAY="$OVERLAY_DIR/dev-public/loopzero-cloud.yaml.gotmpl"

assert_grep_in_file "dev-public declares KRATOS_PUBLIC_URL with https + auth-dev" \
  "$DEV_PUBLIC_OVERLAY" \
  'KRATOS_PUBLIC_URL.*https://auth-dev\.loopzero\.dev'

assert_grep_in_file "dev-public declares KRATOS_INTERNAL_URL with cluster-internal SVC" \
  "$DEV_PUBLIC_OVERLAY" \
  'KRATOS_INTERNAL_URL.*http://kratos\.loopzero-auth\.svc\.cluster\.local:4433'

# Regression guard: dev-public MUST NOT use https for INTERNAL (defeat split purpose)
assert_no_grep_in_file "dev-public KRATOS_INTERNAL_URL is NOT https (cluster-internal must be http)" \
  "$DEV_PUBLIC_OVERLAY" \
  'KRATOS_INTERNAL_URL.*https://'

# Regression guard: dev-public MUST NOT leak prod hostname in either var
assert_no_grep_in_file "dev-public KRATOS_*_URL does NOT leak prod auth.loopzero.example hostname" \
  "$DEV_PUBLIC_OVERLAY" \
  'KRATOS_(PUBLIC|INTERNAL)_URL.*https://auth\.loopzero\.dev'

# === prod env ===
PROD_OVERLAY="$OVERLAY_DIR/prod/loopzero-cloud.yaml.gotmpl"

assert_grep_in_file "prod declares KRATOS_PUBLIC_URL with https + auth (NO -dev)" \
  "$PROD_OVERLAY" \
  'KRATOS_PUBLIC_URL.*"?https://auth\.loopzero\.dev'

assert_grep_in_file "prod declares KRATOS_INTERNAL_URL with cluster-internal SVC" \
  "$PROD_OVERLAY" \
  'KRATOS_INTERNAL_URL.*http://kratos\.loopzero-auth\.svc\.cluster\.local:4433'

# Regression guard: prod MUST NOT use https for INTERNAL
assert_no_grep_in_file "prod KRATOS_INTERNAL_URL is NOT https" \
  "$PROD_OVERLAY" \
  'KRATOS_INTERNAL_URL.*https://'

# Regression guard: prod MUST NOT leak dev hostname
assert_no_grep_in_file "prod KRATOS_*_URL does NOT leak dev-public auth-dev hostname" \
  "$PROD_OVERLAY" \
  'KRATOS_(PUBLIC|INTERNAL)_URL.*auth-dev\.loopzero\.dev'

# === standalone-multi env ===
SM_OVERLAY="$OVERLAY_DIR/standalone-multi/loopzero-cloud.yaml.gotmpl"

if [[ -f "$SM_OVERLAY" ]]; then
  if [[ -s "$SM_OVERLAY" ]]; then
    # Non-empty: regression guard — must NOT leak public FQDNs
    assert_no_grep_in_file "standalone-multi KRATOS_*_URL does NOT leak public FQDN" \
      "$SM_OVERLAY" \
      'KRATOS_(PUBLIC|INTERNAL)_URL.*https://(auth-dev\.loopzero\.dev|auth\.loopzero\.dev)'
  else
    log "PASS [standalone-multi overlay empty — cluster-internal default acceptable per ADR D8]"
    pass=$((pass + 1))
  fi
else
  log "PASS [standalone-multi overlay file absent — chart default acceptable per ADR D8 (no edge surface)]"
  pass=$((pass + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  loopzero-cloud-kratos-url-split aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
