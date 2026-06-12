#!/usr/bin/env bash
# T3.3 of plan 2026-06-11-cleanroom-canonical-5-defense-layers.
#
# Hermetic source-level assertion that per-env `promtail.yaml.gotmpl`
# overlays declare canonical Loki sink URL + scrape_configs pipeline_stages
# per ADR D8. Closes catches #11/#12 from T6.2 session (memory
# `reference_t62_cleanroom_session_2026_06_11` + `j17-aprendizado-promtail-
# loki-pipeline-canonical-via-helmfile`): Promtail→Loki pipeline default
# values use wrong sink (k3d local IP cross-VPC silent fail in prod) +
# scrape_configs missing pipeline_stages canonical para J-17 marker filtering.
#
# Per-env contract (per ADR D8):
#   - standalone-multi: in-cluster sink `http://loki.loopzero-system.svc.cluster.local:3100`
#   - dev-public: in-cluster sink same as standalone (single-cluster runtime)
#   - prod: cross-cluster sink pointing at DOKS internal LB (per
#           j17-aprendizado memory: cross-VPC explicit URL)
#
# Plus J-17 marker-filter contract (4-layer pipeline per CLAUDE.md):
#   - scrape_configs.pipeline_stages must include `::loopzero::` marker capture
#     OR overlay explicitly declares marker labels
#
# Wired in: task quality:gates (after T1.3 ships).
# Allowlist entry: KEEP-CI-WIRED (T3.3 of cleanroom-canonical plan).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OVERLAY_DIR="$REPO_ROOT/infra/helmfile/values"

pass=0
fail=0

log() { printf '[promtail-canonical-pipeline] %s\n' "$*" >&2; }

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
DEV_PUBLIC_OVERLAY="$OVERLAY_DIR/dev-public/promtail.yaml.gotmpl"

assert_grep_in_file "dev-public declares Loki in-cluster sink URL" \
  "$DEV_PUBLIC_OVERLAY" \
  '(loki\.loopzero-system\.svc\.cluster\.local:3100|/loki/api/v1/push)'

# Regression guard: dev-public MUST NOT use k3d localhost IP (would fail cross-VPC in prod migration)
assert_no_grep_in_file "dev-public Loki sink does NOT use k3d localhost IP (cross-VPC fail trap)" \
  "$DEV_PUBLIC_OVERLAY" \
  'http://(127\.0\.0\.1|172\.[0-9]+\.[0-9]+\.[0-9]+|host\.docker\.internal)'

# === prod env ===
PROD_OVERLAY="$OVERLAY_DIR/prod/promtail.yaml.gotmpl"

assert_grep_in_file "prod declares Loki sink (in-cluster OR cross-cluster DOKS LB)" \
  "$PROD_OVERLAY" \
  '(loki\.loopzero-system\.svc\.cluster\.local|/loki/api/v1/push)'

# Regression guard: prod MUST NOT use k3d localhost (j17-aprendizado canonical lesson)
assert_no_grep_in_file "prod Loki sink does NOT use k3d localhost IP" \
  "$PROD_OVERLAY" \
  'http://(127\.0\.0\.1|172\.[0-9]+\.[0-9]+\.[0-9]+|host\.docker\.internal)'

# === standalone-multi env ===
SM_OVERLAY="$OVERLAY_DIR/standalone-multi/promtail.yaml.gotmpl"

assert_grep_in_file "standalone-multi declares Loki in-cluster sink" \
  "$SM_OVERLAY" \
  '(loki\.loopzero-system\.svc\.cluster\.local|/loki/api/v1/push)'

# === Cross-env J-17 marker filter contract ===
# At least one env overlay must declare pipeline_stages OR marker handling for J-17 4-layer pipeline.
# Test: at least 1 of the 3 overlays contains pipeline_stages canonical.
PIPELINE_STAGES_FOUND=0
for env in dev-public prod standalone-multi; do
  ovl="$OVERLAY_DIR/$env/promtail.yaml.gotmpl"
  if [[ -f "$ovl" ]] && grep -qE 'pipeline_stages|::loopzero::' "$ovl" 2>/dev/null; then
    PIPELINE_STAGES_FOUND=$((PIPELINE_STAGES_FOUND + 1))
  fi
done

if [[ "$PIPELINE_STAGES_FOUND" -ge 1 ]]; then
  log "PASS [J-17 marker pipeline_stages declared in ≥1 env overlay] (count=$PIPELINE_STAGES_FOUND)"
  pass=$((pass + 1))
else
  log "FAIL [J-17 marker pipeline_stages not declared in any env overlay]"
  fail=$((fail + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  promtail-canonical-pipeline aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
