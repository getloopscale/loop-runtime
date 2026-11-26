#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
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

# T2.2-followup (plan 2026-05-21-deep-review-remediation): adopt the shared
# dry-run lib. preflight.sh itself performs zero destructive operations
# (only read-only pulumi stack ls + grep), but it is the canonical entry-
# point that operators invoke BEFORE `task drill:l2-scale` (which writes
# state via Pulumi up + helmfile sync). Accepting `--dry-run` here lets
# the operator pipeline preview end-to-end intent before any DO billing
# starts. When DRY_RUN=1, all 4 checks still execute (they are read-only
# diagnostics) but the verdict line names the dry-run mode explicitly so
# downstream automation knows to propagate the flag to drill:l2-scale.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/dry-run.sh disable=SC1091
source "$SCRIPT_DIR/../lib/dry-run.sh"
parse_dry_run_flag "$@"
if [ "${#REMAINING_ARGS[@]}" -gt 0 ]; then
  set -- "${REMAINING_ARGS[@]}"
else
  set --
fi

# --- Argument parsing ---
JSON_OUT=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) JSON_OUT=true; shift ;;
    -h|--help)
      cat <<'HELP'
drill-preflight.sh — Operator Phase 4 pre-flight consolidator

  --json       JSON output (machine-readable for CI/CD)
  --dry-run    Mark verdict as DRY-RUN preview (checks still run — they are
               read-only diagnostics; downstream `task drill:l2-scale` must
               also receive --dry-run to stay non-destructive)
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

# --- Auto-source .env if present (operator credentials convention) ---
# Per user directive 2026-05-16 + dogfood SKILL.md pattern: 4 D7' credentials
# live in .env at repo root. We extract via grep+cut (NOT shell `source` —
# avoids arbitrary code execution risk). Existing env vars take precedence
# (operator-set vars override .env).
#
# SECURITY: NEVER use `bash -x` or `set -x` on this script — would leak token
# values via stderr. Trace files containing token values must be deleted
# immediately + token rotated (incident 2026-05-16 iter 10).
#
# Fix for pipefail abort: grep returns 1 on no-match; with `set -o pipefail`
# the pipeline fails. We use `set +e/+pipefail` block around the extraction.
ENV_FILE="${DRILL_ENV_FILE:-.env}"

# Helper to extract value from .env safely (no pipefail abort)
_extract_env_val() {
  local key="$1"
  local file="$2"
  local v
  set +e
  set +o pipefail
  v=$(grep -E "^${key}=" "$file" 2>/dev/null | head -1 | cut -d= -f2-)
  set -e
  set -o pipefail
  # Strip optional surrounding quotes
  v="${v%\"}"
  v="${v#\"}"
  v="${v%\'}"
  v="${v#\'}"
  printf '%s' "$v"
}

if [[ -f "$ENV_FILE" ]]; then
  log "Loading credentials from $ENV_FILE (only vars not already set)..."
  for token in DIGITALOCEAN_TOKEN PULUMI_ACCESS_TOKEN CLOUDFLARE_API_TOKEN PULUMI_CONFIG_PASSPHRASE; do
    if [[ -z "${!token:-}" ]]; then
      val=$(_extract_env_val "$token" "$ENV_FILE")
      if [[ -n "$val" ]]; then
        export "${token}=${val}"
      fi
      val=""
    fi
  done

  # SPACES_* → AWS_* auto-mapping (iter 11 LIVE bug fix):
  # Pulumi S3 backend (s3://loop-runtime-drill-pulumi-state) uses aws-sdk which
  # looks for AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY env vars. DO Spaces
  # credentials are stored as SPACES_* in .env (per prod workflow pattern
  # infra-bootstrap.yml + infra-drift.yml + infra-destroy.yml). Auto-map
  # SPACES_* → AWS_* if AWS_* not already set. NEVER overrides operator-set.
  for pair in "SPACES_ACCESS_KEY_ID:AWS_ACCESS_KEY_ID" \
              "SPACES_SECRET_ACCESS_KEY:AWS_SECRET_ACCESS_KEY"; do
    src="${pair%%:*}"
    dst="${pair##*:}"
    if [[ -z "${!dst:-}" ]]; then
      val=$(_extract_env_val "$src" "$ENV_FILE")
      if [[ -n "$val" ]]; then
        export "${dst}=${val}"
      fi
      val=""
    fi
  done
fi

# --- Check 1: credentials (D7' single-token model) ---
# Backend-aware classification per Pulumi.yaml `s3://loop-runtime-drill-pulumi-state`:
#   MANDATORY (S3 backend requires):
#     - DIGITALOCEAN_TOKEN — DO API (clusters/PG/Spaces/VPC provisioning)
#     - CLOUDFLARE_API_TOKEN — CF Zone provisioning
#     - PULUMI_CONFIG_PASSPHRASE — S3 state encryption (without this `pulumi up` fails immediately)
#     - AWS_ACCESS_KEY_ID — DO Spaces S3 backend access (or SPACES_ACCESS_KEY_ID auto-mapped)
#     - AWS_SECRET_ACCESS_KEY — DO Spaces S3 backend secret (or SPACES_SECRET_ACCESS_KEY auto-mapped)
#   ADVISORY (only required for Pulumi Cloud backend, NOT S3):
#     - PULUMI_ACCESS_TOKEN — used if pulumi-cloud backend; absent OK with S3 backend
log "Check 1/4: credentials (D7' single-token model, backend-aware)..."
MANDATORY_TOKENS=(DIGITALOCEAN_TOKEN CLOUDFLARE_API_TOKEN PULUMI_CONFIG_PASSPHRASE AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY)
ADVISORY_TOKENS=(PULUMI_ACCESS_TOKEN)
MISSING_TOKENS=()
MISSING_ADVISORY=()

for token in "${MANDATORY_TOKENS[@]}"; do
  if [[ -z "${!token:-}" ]]; then
    MISSING_TOKENS+=("$token")
  fi
done

for token in "${ADVISORY_TOKENS[@]}"; do
  if [[ -z "${!token:-}" ]]; then
    MISSING_ADVISORY+=("$token")
  fi
done

if [[ ${#MISSING_TOKENS[@]} -eq 0 ]]; then
  if [[ ${#MISSING_ADVISORY[@]} -eq 0 ]]; then
    log "  ✓ All 4 tokens present (DO + CF + passphrase + Pulumi Cloud)"
    record "credentials" "PASS" ""
  else
    log "  ✓ 3/3 mandatory tokens present (S3 backend); advisory missing: ${MISSING_ADVISORY[*]}"
    log "    (PULUMI_ACCESS_TOKEN is only needed for Pulumi Cloud backend; drill uses S3 — OK)"
    record "credentials" "PASS" "advisory-missing:${MISSING_ADVISORY[*]}"
  fi
else
  log "  ✗ Missing MANDATORY tokens: ${MISSING_TOKENS[*]}"
  if [[ ${#MISSING_ADVISORY[@]} -gt 0 ]]; then
    log "    (advisory also missing: ${MISSING_ADVISORY[*]} — OK with S3 backend)"
  fi
  record "credentials" "FAIL" "mandatory-missing:${MISSING_TOKENS[*]}"
  EXIT_CODE=1
fi

# --- Check 2: 5 L2-unlock scripts present + executable ---
log "Check 2/4: 5 L2-unlock scripts present + executable..."
MISSING_SCRIPTS=()
for script in \
  infra/scripts/drill/state-reset.sh \
  infra/scripts/drill/drill-helmfile-bootstrap.sh \
  infra/scripts/drill/cancel-refresh.sh \
  infra/scripts/drill/evidence-capture.sh \
  infra/scripts/drill/teardown-validator.sh; do
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
# Isolation script exit codes:
#   0 = OK (D7' single-token model — DIGITALOCEAN_TOKEN with project/tag/bucket isolation)
#   1 = CATASTROPHIC collision (legacy DRILL_*_TOKEN still set AND matches prod hash)
#   2 = No DO token at all (DIGITALOCEAN_TOKEN unset)
#
# T0.5 (plan 2026-05-21-deep-review-remediation): D7' sponsor override (2026-05-16)
# replaced the dual-token model with a single shared DIGITALOCEAN_TOKEN — isolation
# is at DO project + tag (loop-runtime-drill-*) + state bucket level, NOT token level.
# The legacy drill-prefixed DO token is deprecated; the collision case (exit 1)
# is preserved only for operators migrating from the pre-D7' workflow.
#
# Skip check 3 if check 1 already reported missing credentials (exit 1) —
# isolation check would just re-report the same missing-token symptom with
# misleading "collision" interpretation.
log "Check 3/4: D7' advisory mode (credentials isolation)..."
# Migrated to the Go gate `loop-runtime-ops quality drill-creds-isolation`
# (pkg/quality/drillcredisolation) — same tri-state exit contract:
# 0 OK / 1 catastrophic / 2 missing. The bash check-drill-credentials-isolation.sh
# was deleted (MIGRATE-GO-PUREFN). See docs/operations.md.
if [[ "${MISSING_TOKENS[*]:-}" == *"DIGITALOCEAN_TOKEN"* ]]; then
  log "  ⊘ Skipped (DIGITALOCEAN_TOKEN missing — covered by check 1)"
  record "advisory_mode" "SKIP" "no-do-token-check-1-covered"
else
  PF_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
  set +e
  if command -v loop-runtime-ops >/dev/null 2>&1; then
    loop-runtime-ops quality drill-creds-isolation >/dev/null 2>&1
  else
    ( cd "$PF_REPO_ROOT/cmd/loop-runtime-ops" && go run . quality drill-creds-isolation ) >/dev/null 2>&1
  fi
  ISO_RC=$?
  set -e
  case "$ISO_RC" in
    0)
      log "  ✓ Advisory mode OK (D7' single-token path; no collision detected)"
      record "advisory_mode" "PASS" ""
      ;;
    1)
      log "  ✗ CATASTROPHIC: DRILL_*_TOKEN matches prod hash (drill teardown could destroy prod)"
      record "advisory_mode" "FAIL" "credentials-collision"
      [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=3
      ;;
    2)
      log "  ✗ No DO token configured (DIGITALOCEAN_TOKEN unset — per D7' single-token model)"
      record "advisory_mode" "FAIL" "no-do-token"
      [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=3
      ;;
    *)
      log "  ✗ Isolation check unknown exit $ISO_RC"
      record "advisory_mode" "FAIL" "unknown-isolation-exit-$ISO_RC"
      [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=3
      ;;
  esac
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

  # Iter 11 LIVE bug fix: distinguish empty stack (resourceCount=0, OK to proceed)
  # from non-empty stack (resourceCount>0, real leftover requiring teardown).
  # Empty stack registration is the legitimate post-state-reset state — `pulumi up`
  # will populate it with fresh resources.
  if echo "$STACK_LIST" | grep -q '"drill"'; then
    # Extract resourceCount for drill stack via python (no jq dependency)
    RESOURCE_COUNT=$(echo "$STACK_LIST" | python3 -c '
import sys, json
try:
    stacks = json.load(sys.stdin)
    for s in stacks:
        if s.get("name") == "drill":
            print(s.get("resourceCount", 0))
            sys.exit(0)
    print(0)
except Exception:
    print(0)
' 2>/dev/null || echo 0)
    if [[ "$RESOURCE_COUNT" -gt 0 ]]; then
      LEFTOVER=true
    fi
  fi
fi

if [[ "$LEFTOVER" == "true" ]]; then
  log "  ✗ Leftover drill stack detected with $RESOURCE_COUNT resources — run 'task drill:down' first"
  record "no_leftover_stack" "FAIL" "drill-stack-has-${RESOURCE_COUNT}-resources"
  [[ "$EXIT_CODE" -eq 0 ]] && EXIT_CODE=4
else
  if echo "${STACK_LIST:-}" | grep -q '"drill"'; then
    log "  ✓ Empty drill stack registration (OK — pulumi up will populate)"
  else
    log "  ✓ No drill stack registration"
  fi
  record "no_leftover_stack" "PASS" ""
fi

# --- Aggregate verdict ---
if [[ "$EXIT_CODE" -eq 0 ]]; then
  VERDICT="PASS"
  if [[ "${DRY_RUN:-0}" == "1" ]]; then
    NEXT_ACTION="DRY-RUN preview OK. To actually provision: rerun WITHOUT --dry-run, then 'task drill:l2-scale'"
  else
    NEXT_ACTION="Operator may proceed: 'task drill:l2-scale'"
  fi
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
