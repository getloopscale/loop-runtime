#!/usr/bin/env bash
# T3.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers.
#
# Hermetic source-level assertion that per-env `ory-hydra.yaml.gotmpl`
# overlays at `infra/helmfile/values/<env>/` declare canonical URLs
# matching the env contract. Closes catches #14/#15/#16 from T6.2 session
# (memory reference_t62_cleanroom_session_2026_06_11): Hydra default Helm
# values point at cluster-internal DNS, and Cleanroom destroys runtime
# patches — only declarative overlays per env are canonical.
#
# Per-env contract (per ADR D8 — absorbs EC-1):
#   - standalone-multi: cluster-internal OK (no edge); overlay MAY be empty
#     OR declare `http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444`
#   - dev-public: MUST declare `auth-dev.loopzero.example` (D5 sub-domain split)
#                + urls.device.verification → app-dev.loopzero.example/device-verify
#                + urls.device.success → app-dev.loopzero.example/device-success
#   - prod: MUST declare `auth.loopzero.example` (NO -dev suffix; regression guard)
#                + urls.device.verification → app.loopzero.example/device-verify
#                + urls.device.success → app.loopzero.example/device-success
#
# Pattern: hermetic per-env source grep (no helm/helmfile binary required,
# no cluster context). Mirrors canonical pattern from
# `infra/tests/helm/ory-hydra-device-urls_test.sh` (T2.1 of J-05 plan).
#
# Wired in: task quality:gates (after T1.1 ships canonical overlays).
# Allowlist entry: KEEP-CI-WIRED (T3.1 of cleanroom-canonical plan).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OVERLAY_DIR="$REPO_ROOT/infra/helmfile/values"

pass=0
fail=0

log() { printf '[ory-hydra-canonical-urls] %s\n' "$*" >&2; }

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
DEV_PUBLIC_OVERLAY="$OVERLAY_DIR/dev-public/ory-hydra.yaml.gotmpl"

assert_grep_in_file "dev-public urls.self.public uses auth-dev FQDN" \
  "$DEV_PUBLIC_OVERLAY" \
  '(auth-dev\.loopzero\.dev|publicAuthUrl)'

assert_grep_in_file "dev-public urls.device.verification points app-dev /device-verify" \
  "$DEV_PUBLIC_OVERLAY" \
  '(app-dev\.loopzero\.dev.*device-verify|uiBaseUrl.*device-verify)'

assert_grep_in_file "dev-public urls.device.success points app-dev /device-success" \
  "$DEV_PUBLIC_OVERLAY" \
  '(app-dev\.loopzero\.dev.*device-success|uiBaseUrl.*device-success)'

# Regression guard: dev-public overlay MUST NOT hardcode prod hostname
assert_no_grep_in_file "dev-public overlay does NOT leak prod auth.loopzero.example hostname" \
  "$DEV_PUBLIC_OVERLAY" \
  '"https://auth\.loopzero\.dev'

# === prod env ===
PROD_OVERLAY="$OVERLAY_DIR/prod/ory-hydra.yaml.gotmpl"

assert_grep_in_file "prod urls.self.public uses auth (NO -dev suffix)" \
  "$PROD_OVERLAY" \
  '("https://auth\.loopzero\.dev|publicAuthUrl)'

assert_grep_in_file "prod urls.device.verification points app /device-verify" \
  "$PROD_OVERLAY" \
  '(app\.loopzero\.dev.*device-verify|uiBaseUrl.*device-verify)'

assert_grep_in_file "prod urls.device.success points app /device-success" \
  "$PROD_OVERLAY" \
  '(app\.loopzero\.dev.*device-success|uiBaseUrl.*device-success)'

# Regression guard: prod overlay MUST NOT leak dev hostnames
assert_no_grep_in_file "prod overlay does NOT leak dev-public auth-dev.loopzero.example hostname" \
  "$PROD_OVERLAY" \
  'auth-dev\.loopzero\.dev'

assert_no_grep_in_file "prod overlay does NOT leak dev-public app-dev.loopzero.example hostname" \
  "$PROD_OVERLAY" \
  'app-dev\.loopzero\.dev'

# === standalone-multi env ===
# Per ADR D8: cluster-internal OK (no edge surface). Overlay MAY be empty
# OR declare cluster-internal FQDN. We assert: either file exists empty,
# OR if non-empty contains cluster-internal pattern (NOT a public FQDN).
SM_OVERLAY="$OVERLAY_DIR/standalone-multi/ory-hydra.yaml.gotmpl"

if [[ -f "$SM_OVERLAY" ]]; then
  if [[ -s "$SM_OVERLAY" ]]; then
    # Non-empty: regression guard — must NOT leak public FQDNs
    assert_no_grep_in_file "standalone-multi overlay does NOT leak public auth.loopzero.example" \
      "$SM_OVERLAY" \
      '(auth-dev\.loopzero\.dev|auth\.loopzero\.dev)'
  else
    log "PASS [standalone-multi overlay empty — cluster-internal default acceptable per ADR D8]"
    pass=$((pass + 1))
  fi
else
  log "FAIL [standalone-multi overlay file missing]: $SM_OVERLAY"
  fail=$((fail + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  ory-hydra-canonical-urls aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
