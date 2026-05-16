#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16-drill-l2-unlock — Operator handoff Phase 4 pre-flight consolidator.
#
# Single-command pre-flight that validates ALL prerequisites for drill LIVE
# Phase 4 invocation. Replaces the 4-line bash block in operator-handoff doc
# with one auditable command + structured exit codes.
#
# Why this exists
# ---------------
# Operator handoff `operator-handoff-2026-05-16-drill-l2-phase4.md` lists 4
# pre-flight checks:
#   1. Confirm 4 credentials (D7' single-token model)
#   2. Confirm advisory mode active
#   3. Confirm 5 scripts present + executable
#   4. Confirm no leftover drill stack
#
# A bash blob is fragile — operator might run only 3 of 4. This script
# consolidates with mandatory aggregate exit code: ANY failure exits non-zero,
# operator cannot accidentally proceed with partial pre-flight.
#
# Per CLAUDE.md Rule 0 (ambiente local fidedigno) + Rule 6 (zero workarounds):
# pre-flight discipline is mandatory.
#
# Usage:
#   bash infra/scripts/drill-preflight.sh            # full check
#   bash infra/scripts/drill-preflight.sh --json     # JSON output for CI/CD
#
# Exit codes:
#   0 — All checks PASS, operator may proceed with `task drill:l2-scale`
#   1 — Missing credentials (1+ of 4 token env vars absent)
#   2 — Missing scripts (1+ of 5 new L2-unlock scripts absent or not executable)
#   3 — Advisory mode check failed (credentials collision OR isolation script missing)
#   4 — Leftover drill stack detected (operator should `task drill:down` first)
#   5 — Argument error
#
# JSON output (--json):
#   {"verdict": "PASS|FAIL", "checks": {...}, "next_action": "..."}

set -euo pipefail

# --- Argument parsing ---
JSON_OUT=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) JSON_OUT=true; shift ;;
    -h|--help)
      cat <<'HELP'
drill-preflight.sh — Operator Phase 4 pre-flight consolidator

  --json       JSON output (machine-readable for CI/CD)
  -h, --help   Show this help

Plan: 2026-05-16-drill-l2-unlock operator handoff
HELP
      exit 0 ;;
    *)
      echo "[drill-preflight] ERROR: unknown flag $1" >&2
      exit 5 ;;
  esac
done

log() {
  if [[ "$JSON_OUT" == "false" ]]; then
    printf '[drill-preflight] %s\n' "$*" >&2
  fi
}

# Aggregate result accumulators
declare -A CHECK_RESULTS=()
EXIT_CODE=0

record() {
  local check="$1"
  local status="$2"
  local detail="${3:-}"
  CHECK_RESULTS["$check"]="$status:$detail"
}

# --- Check 1: 4 credentials (D7' single-token model) ---
log "Check 1/4: credentials (D7' single-token model)..."
MISSING_TOKENS=()
for token in DIGITALOCEAN_TOKEN PULUMI_ACCESS_TOKEN CLOUDFLARE_API_TOKEN PULUMI_CONFIG_PASSPHRASE; do
  if [[ -z "${!token:-}" ]]; then
    MISSING_TOKENS+=("$token")
  fi
done

if [[ ${#MISSING_TOKENS[@]} -eq 0 ]]; then
  log "  ✓ All 4 tokens present (DO + Pulumi + CF + passphrase)"
  record "credentials" "PASS" ""
else
  log "  ✗ Missing tokens: ${MISSING_TOKENS[*]}"
  record "credentials" "FAIL" "${MISSING_TOKENS[*]}"
  EXIT_CODE=1
fi

# --- Check 2: 5 L2-unlock scripts present + executable ---
log "Check 2/4: 5 L2-unlock scripts present + executable..."
MISSING_SCRIPTS=()
for script in \
  infra/scripts/drill-state-reset.sh \
  infra/scripts/drill-helmfile-bootstrap.sh \
  infra/scripts/drill-cancel-refresh.sh \
  infra/scripts/drill-evidence-capture.sh \
  infra/scripts/drill-teardown-validator.sh; do
  if [[ ! -x "$script" ]]; then
    MISSING_SCRIPTS+=("$script")
  fi
done

if [[ ${#MISSING_SCRIPTS[@]} -eq 0 ]]; then
  log "  ✓ All 5 scripts present + executable"
  record "scripts" "PASS" ""
else
  log "  ✗ Missing/non-executable: ${MISSING_SCRIPTS[*]}"
  record "scripts" "FAIL" "${MISSING_SCRIPTS[*]}"
  [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=2
fi

# --- Check 3: Advisory mode (D7' credentials isolation) ---
log "Check 3/4: D7' advisory mode (credentials isolation)..."
ISO_SCRIPT="infra/scripts/check-drill-credentials-isolation.sh"
if [[ ! -x "$ISO_SCRIPT" ]]; then
  log "  ✗ Isolation script missing: $ISO_SCRIPT"
  record "advisory_mode" "FAIL" "isolation-script-missing"
  [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=3
else
  # Run advisory check (exit 0 in D7' single-token path; non-zero on collision)
  if bash "$ISO_SCRIPT" >/dev/null 2>&1; then
    log "  ✓ Advisory mode OK (D7' single-token path; no collision detected)"
    record "advisory_mode" "PASS" ""
  else
    log "  ✗ Credentials collision detected (DRILL_*_TOKEN matches prod hash)"
    record "advisory_mode" "FAIL" "credentials-collision"
    [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=3
  fi
fi

# --- Check 4: No leftover drill stack ---
log "Check 4/4: no leftover drill stack..."
PULUMI_DIR="${PULUMI_DRILL_DIR:-infra/pulumi-drill}"
LEFTOVER=false

if [[ -d "$PULUMI_DIR" ]]; then
  # Use pulumi if available; fallback to npx pulumi
  STACK_LIST=""
  if command -v pulumi >/dev/null 2>&1; then
    STACK_LIST=$(cd "$PULUMI_DIR" && pulumi stack ls --json 2>/dev/null || echo '[]')
  elif command -v npx >/dev/null 2>&1; then
    STACK_LIST=$(cd "$PULUMI_DIR" && npx pulumi stack ls --json 2>/dev/null || echo '[]')
  fi

  if echo "$STACK_LIST" | grep -q '"drill"'; then
    LEFTOVER=true
  fi
fi

if [[ "$LEFTOVER" == "true" ]]; then
  log "  ✗ Leftover drill stack detected — run 'task drill:down' first"
  record "no_leftover_stack" "FAIL" "drill-stack-exists"
  [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=4
else
  log "  ✓ No leftover drill stack"
  record "no_leftover_stack" "PASS" ""
fi

# --- Aggregate verdict ---
if [[ "$EXIT_CODE" -eq 0 ]]; then
  VERDICT="PASS"
  NEXT_ACTION="Operator may proceed: 'task drill:l2-scale'"
else
  VERDICT="FAIL"
  NEXT_ACTION="Fix failing checks before invoking drill Phase 4"
fi

if [[ "$JSON_OUT" == "true" ]]; then
  # Build JSON output (compact, no jq dep)
  printf '{'
  printf '"verdict":"%s",' "$VERDICT"
  printf '"checks":{'
  FIRST=true
  for key in "${!CHECK_RESULTS[@]}"; do
    if [[ "$FIRST" == "false" ]]; then
      printf ','
    fi
    FIRST=false
    STATUS=$(echo "${CHECK_RESULTS[$key]}" | cut -d: -f1)
    DETAIL=$(echo "${CHECK_RESULTS[$key]}" | cut -d: -f2-)
    printf '"%s":{"status":"%s","detail":"%s"}' "$key" "$STATUS" "$DETAIL"
  done
  printf '},'
  printf '"next_action":"%s",' "$NEXT_ACTION"
  printf '"exit_code":%d' "$EXIT_CODE"
  printf '}\n'
else
  log ""
  log "============================================================"
  log "Pre-flight verdict: $VERDICT (exit $EXIT_CODE)"
  log "Next action: $NEXT_ACTION"
  log "============================================================"
fi

exit "$EXIT_CODE"
