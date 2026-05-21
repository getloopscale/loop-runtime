#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T0.1 (EC-09 fix) — Drill credentials isolation pre-flight.
#
# Catastrophic risk per D7: if DRILL_DO_TOKEN, DRILL_PULUMI_TOKEN, or
# DRILL_CF_TOKEN equals the corresponding prod token, drill teardown could
# accidentally destroy production resources. This script is the runtime
# safety check invoked by skill /drill-test Etapa 1.1 pre-flight AND by
# drill-prod-identical-soak.yml workflow before any provision step.
#
# Compares SHA256 first-8-chars of DRILL_* tokens vs prod tokens. If hashes
# match (or prod token present in same env as drill token), exits 1 with
# clear error. If isolation confirmed OR prod token absent (drill-only env),
# exits 0.
#
# Usage:
#   bash infra/scripts/check-drill-credentials-isolation.sh
#
# Exit codes:
#   0 — Isolation confirmed OR prod tokens absent (safe to proceed)
#   1 — Drill token EQUALS prod token (CATASTROPHIC; abort drill)
#   2 — DRILL_DO_TOKEN missing (cannot proceed)

set -euo pipefail

log() { printf '[check-drill-credentials-isolation] %s\n' "$*" >&2; }

# --- D7 amendment (sponsor override 2026-05-16) ---
# Original D7 mandated separate DRILL_DO_TOKEN scoped to drill account.
# Sponsor amended D7 → D7' to use single DIGITALOCEAN_TOKEN per pragmatic
# operator setup (DO API doesn't support native per-token project scoping
# in public API; isolation comes from DO project `loopzero-drill` + tag filter
# `loopzero-drill-*` + state bucket `loopzero-drill-pulumi-state`).
#
# Behavior:
# - If DRILL_DO_TOKEN set + distinct from prod: enforce isolation (strict)
# - If DRILL_DO_TOKEN absent: use DIGITALOCEAN_TOKEN as drill credential
#   (sponsor-approved override; advisory warning emitted but exit 0)
# - If DRILL_DO_TOKEN matches prod: still CATASTROPHIC (no degraded path
#   acceptable when explicit-but-identical tokens are configured)
#
# Strict mode override: set DRILL_REQUIRE_ISOLATED_TOKENS=true to restore
# original D7 enforcement (separate DRILL_DO_TOKEN required).

STRICT_MODE="${DRILL_REQUIRE_ISOLATED_TOKENS:-false}"

# --- Required inputs ---
if [[ -z "${DRILL_DO_TOKEN:-}" ]]; then
  # D7' sponsor override path: drill uses DIGITALOCEAN_TOKEN
  if [[ -z "${DIGITALOCEAN_TOKEN:-}" ]]; then
    log "ERROR: neither DRILL_DO_TOKEN nor DIGITALOCEAN_TOKEN set"
    log "  Drill cannot proceed without a DO API token (one of the two required)."
    exit 2
  fi
  if [[ "$STRICT_MODE" == "true" ]]; then
    log "ERROR (strict mode): DRILL_DO_TOKEN env var required when DRILL_REQUIRE_ISOLATED_TOKENS=true"
    exit 2
  fi
  log "ADVISORY: DRILL_DO_TOKEN not set; using DIGITALOCEAN_TOKEN per D7' sponsor override"
  log "  Blast-radius bounded by DO project 'loopzero-drill' + tag 'loopzero-drill-*' filter"
  log "  Pulumi state bucket 'loopzero-drill-pulumi-state' (separate from prod state)"
  log "  drill-teardown-validator.sh + drill-watchdog.sh ONLY act on tagged drill resources"
  log "✓ Sponsor-approved override path; proceed with single-token model"
  exit 0
fi

# --- Hash helper (first 8 chars of SHA256, sufficient for collision detection) ---
hash_short() {
  printf '%s' "$1" | sha256sum | cut -c1-8
}

# --- Compare drill DO token vs prod DO token (if present) ---
DRILL_DO_HASH=$(hash_short "$DRILL_DO_TOKEN")

if [[ -n "${DO_TOKEN:-}" ]] || [[ -n "${DIGITALOCEAN_TOKEN:-}" ]]; then
  PROD_DO_TOKEN="${DO_TOKEN:-${DIGITALOCEAN_TOKEN:-}}"
  PROD_DO_HASH=$(hash_short "$PROD_DO_TOKEN")
  if [[ "$DRILL_DO_HASH" == "$PROD_DO_HASH" ]]; then
    log "CATASTROPHIC: DRILL_DO_TOKEN hash matches prod DO_TOKEN/DIGITALOCEAN_TOKEN!"
    log "  Drill teardown could destroy production resources."
    log "  PER D7: drill MUST use separate token scoped to drill account."
    log "  Action: revoke shared token; create new drill-scoped token; reconfigure DRILL_DO_TOKEN."
    exit 1
  fi
  log "✓ DRILL_DO_TOKEN distinct from prod (hashes ${DRILL_DO_HASH} vs ${PROD_DO_HASH})"
else
  log "✓ No prod DO_TOKEN in this env (drill-only context) — isolation trivially confirmed"
fi

# --- Compare drill Pulumi token (if present) ---
if [[ -n "${DRILL_PULUMI_TOKEN:-}" ]]; then
  DRILL_PULUMI_HASH=$(hash_short "$DRILL_PULUMI_TOKEN")
  if [[ -n "${PULUMI_ACCESS_TOKEN:-}" ]]; then
    PROD_PULUMI_HASH=$(hash_short "$PULUMI_ACCESS_TOKEN")
    if [[ "$DRILL_PULUMI_HASH" == "$PROD_PULUMI_HASH" ]]; then
      log "CATASTROPHIC: DRILL_PULUMI_TOKEN hash matches prod PULUMI_ACCESS_TOKEN!"
      log "  Drill destroy could modify prod stack state."
      exit 1
    fi
    log "✓ DRILL_PULUMI_TOKEN distinct from prod"
  else
    log "✓ DRILL_PULUMI_TOKEN present; no prod PULUMI_ACCESS_TOKEN to compare"
  fi
fi

# --- Compare drill CF token (if present) ---
if [[ -n "${DRILL_CF_TOKEN:-}" ]]; then
  DRILL_CF_HASH=$(hash_short "$DRILL_CF_TOKEN")
  if [[ -n "${CLOUDFLARE_API_TOKEN:-}" ]]; then
    PROD_CF_HASH=$(hash_short "$CLOUDFLARE_API_TOKEN")
    if [[ "$DRILL_CF_HASH" == "$PROD_CF_HASH" ]]; then
      log "CATASTROPHIC: DRILL_CF_TOKEN hash matches prod CLOUDFLARE_API_TOKEN!"
      log "  Drill could modify prod loopzero.example CF Zone."
      exit 1
    fi
    log "✓ DRILL_CF_TOKEN distinct from prod"
  else
    log "✓ DRILL_CF_TOKEN present; no prod CLOUDFLARE_API_TOKEN to compare"
  fi
fi

log "✓ All drill credentials confirmed isolated per D7. Safe to proceed."
exit 0
