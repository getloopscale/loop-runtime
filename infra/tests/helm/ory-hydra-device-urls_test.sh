#!/usr/bin/env bash
# T2.1 of plan 2026-06-05-dashboard-journey-j05-device-success
#                                      (amended 2026-06-05 after LIVE Hydra crash).
#
# Hermetic source-level assertion that `infra/helm/ory-hydra/values.yaml.gotmpl`
# routes `urls.self.issuer` via `publicAuthUrl` so dev-public / drill / prod
# emit a public-FQDN issuer (the EC-10 contract: Hydra `verification_uri`
# must NOT leak the cluster-internal `*.svc.cluster.local` URL — caught LIVE
# 2026-06-05 against https://cloud-dev.loopzero.example).
#
# AMENDED 2026-06-05: original T2.1 attempted to set `urls.post_device_done_url`.
# Hydra v2 schema rejects that key (`additionalProperties not allowed`),
# crashing the rollout. Device-flow landing redirect is controlled by client
# OAuth `redirect_uris` + Hydra's built-in consent-complete page, NOT a
# server-level config. T2.1 D1 reduces to the `urls.self.issuer` override
# — the LIVE bug EC-10 actually caught.
#
# Asserts:
#   1. `issuer:` key is present exactly once under `urls.self`.
#   2. The issuer value resolves via `publicAuthUrl` gotmpl override.
#   3. Fallback default is the cluster-internal FQDN.
#   4. Trailing slash is appended (Hydra requires `iss` to end in `/`).
#   5. Comment cites plan ID + EC-10 LIVE provenance for auditability.
#   6. `post_device_done_url` is ABSENT (regression guard — adding it
#      back would crash Hydra rollout).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
TARGET="$REPO_ROOT/infra/helm/ory-hydra/values.yaml.gotmpl"

pass=0
fail=0

log() { printf '[ory-hydra-device-urls] %s\n' "$*" >&2; }

if [[ ! -f "$TARGET" ]]; then
  log "FAIL: target file not found: $TARGET"
  exit 1
fi

assert_grep() {
  local desc="$1" pattern="$2"
  if grep -qE "$pattern" "$TARGET"; then
    log "PASS [$desc]"
    pass=$((pass + 1))
  else
    log "FAIL [$desc]: pattern not matched: $pattern"
    fail=$((fail + 1))
  fi
}

assert_no_grep() {
  local desc="$1" pattern="$2"
  if grep -qE "$pattern" "$TARGET"; then
    log "FAIL [$desc]: forbidden pattern matched: $pattern"
    fail=$((fail + 1))
  else
    log "PASS [$desc]"
    pass=$((pass + 1))
  fi
}

# (1) issuer key present exactly once.
issuer_count=$(grep -cE '^[[:space:]]+issuer:' "$TARGET" || true)
if [[ "$issuer_count" -eq 1 ]]; then
  log "PASS [single issuer key] (count=$issuer_count)"
  pass=$((pass + 1))
else
  log "FAIL [single issuer key]: expected 1 got $issuer_count"
  fail=$((fail + 1))
fi

# (2) issuer uses publicAuthUrl gotmpl override.
assert_grep "issuer uses publicAuthUrl gotmpl override" \
  'issuer:.*publicAuthUrl'

# (3) Default fallback is the cluster-internal FQDN.
# Implementation uses `{{ if ... }}{{ else }}http://ory-hydra-public...{{ end }}`
# pattern (not the Helm `default` helper). Either pattern is valid Go template
# fallback; assert the FALLBACK STRING is present on the same line as `issuer:`.
assert_grep "issuer default fallback to cluster-internal FQDN" \
  'issuer:.*ory-hydra-public\.loopzero-auth\.svc\.cluster\.local'

# (4) Trailing slash present in the value template.
assert_grep "issuer ends with trailing slash" \
  'issuer:.*}/"\s*$'

# (5) Comment cites plan ID + EC-10 evidence.
assert_grep "comment cites plan J-05 + EC-10" \
  '# Plan J-05.*EC-10'

# (6) post_device_done_url is ABSENT (regression guard — Hydra v2 schema rejects).
assert_no_grep "no post_device_done_url key (Hydra v2 schema rejects)" \
  '^[[:space:]]+post_device_done_url:'

# (7) urls.self.public key present (required for verification_uri host).
public_count=$(grep -cE '^[[:space:]]+public:[[:space:]]+"\{\{' "$TARGET" || true)
if [[ "$public_count" -ge 1 ]]; then
  log "PASS [urls.self.public key present] (count=$public_count)"
  pass=$((pass + 1))
else
  log "FAIL [urls.self.public key present]: expected ≥1 got $public_count"
  fail=$((fail + 1))
fi

# (8) urls.self.public also uses publicAuthUrl override (same FQDN as issuer).
# Same Helm template if-else fallback pattern as (3); assert publicAuthUrl
# reference + cluster-internal FQDN on the same `public:` line.
assert_grep "urls.self.public uses publicAuthUrl override" \
  'public:.*publicAuthUrl.*ory-hydra-public\.loopzero-auth\.svc\.cluster\.local'

# (9) urls.device.verification points at dashboard /device-verify (NOT Hydra
#     /oauth2/fallbacks/device — that page is literally the "config missing"
#     error page in Hydra v2.6).
assert_grep "urls.device.verification → dashboard /device-verify" \
  'verification:.*uiBaseUrl.*device-verify'

# (10) urls.device.success → dashboard /device-success.
assert_grep "urls.device.success → dashboard /device-success" \
  'success:.*uiBaseUrl.*device-success'

log ""
log "════════════════════════════════════════════════════════"
log "  ory-hydra-device-urls aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
