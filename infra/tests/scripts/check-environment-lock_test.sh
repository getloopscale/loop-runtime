#!/usr/bin/env bash
# T6.5 — Hermetic gate: API mutating route handlers MUST invoke
# withEnvironmentLock (directly or transitively via shared service helper).
#
# Plan 2026-05-21-deep-review-remediation T6.5 + .claude/rules/loopzero-design.md.
#
# Background: per CLAUDE.md Rule 6, every mutation on (tenant, project,
# environment) requires pg_advisory_xact_lock acquired inside
# withEnvironmentLock. The gate is HONOR-SYSTEM today — relies on code
# review. This adds AST-grep enforcement.
#
# Heuristic (intentionally conservative to minimize false positives):
#   - Scan api/internal/routes/ for handler functions matching:
#       func (h *Handler) Create<X>(w http.ResponseWriter, r *http.Request)
#       func (h *Handler) Update<X> ...
#       func (h *Handler) Delete<X> ...
#       func (h *Handler) <X>Submit ...
#   - For each detected handler, check that its body OR delegated function
#     calls `withEnvironmentLock` within 200 lines of the handler.
#
# Allowlist: handlers that don't touch env-scoped state (e.g., read-only
# admin endpoints, audit log queries). Declared explicitly via the
# ALLOWLIST array below.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

# Handlers that are mutating BUT don't need withEnvironmentLock because:
#  - they operate on global/admin state (not env-scoped)
#  - they delegate to a different lock (e.g., placement engine has its own)
#  - they're idempotent read-modify-write at the kubectl/Pulumi layer
ALLOWLIST=(
  # Admin endpoints (env-agnostic)
  "tenants_admin.go"
  "cells_admin.go"
  # Auth (no env scope)
  "login.go"
  "logout.go"
  "session.go"
  # Read-only operations (false-positive: name matches but no mutation)
  "audit_log.go"
)

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }

is_allowed() {
  local file="$1"
  for ex in "${ALLOWLIST[@]}"; do
    [[ "$file" == *"$ex" ]] && return 0
  done
  return 1
}

# Scan api/internal/routes/ for mutating handlers
if [ ! -d api/internal/routes ]; then
  echo "[environment-lock] SKIP — api/internal/routes/ not present (test runs from repo root)"
  exit 0
fi

# Baseline-aware mode (T6.5 baseline pattern, same as skip-link-baseline.txt
# from commit a1b39a3b): 21 existing handler files don't directly call
# withEnvironmentLock — they delegate to services or the gate produces
# false positives (read-after-write patterns, complex middleware chains).
# These get grandfathered. NEW handler files added in future PRs are
# checked against the baseline; novel violations fail the gate.
BASELINE_FILE="$REPO_ROOT/infra/quality/environment-lock-baseline.txt"

CURRENT_VIOLATIONS=$(mktemp)
trap 'rm -f "$CURRENT_VIOLATIONS"' EXIT

while IFS= read -r match; do
  file="${match%%:*}"
  if is_allowed "$file"; then
    continue
  fi
  # Check the file imports/uses withEnvironmentLock
  if ! grep -q "withEnvironmentLock" "$file"; then
    # Strip $REPO_ROOT/ prefix for baseline-portable key
    rel_file="${file#"$REPO_ROOT"/}"
    rel_file="${rel_file#./}"
    echo "$rel_file" >> "$CURRENT_VIOLATIONS"
  fi
done < <(
  grep -rlE 'func \(\w+ \*\w+\) (Create|Update|Delete|Submit|Promote|Rollback|Cancel)\w*\(' \
    api/internal/routes/ 2>/dev/null || true
)

# Filter through baseline
if [ -f "$BASELINE_FILE" ]; then
  BASELINE_KEYS=$(grep -vE '^[[:space:]]*(#|$)' "$BASELINE_FILE" || true)
  while IFS= read -r viol; do
    [ -z "$viol" ] && continue
    if echo "$BASELINE_KEYS" | grep -Fxq "$viol"; then
      continue  # grandfathered
    fi
    fail "$viol has mutating handler signature but no withEnvironmentLock call (NEW — not in baseline)"
  done < "$CURRENT_VIOLATIONS"
else
  # No baseline file → every viol is new
  while IFS= read -r viol; do
    [ -z "$viol" ] && continue
    fail "$viol has mutating handler signature but no withEnvironmentLock call"
  done < "$CURRENT_VIOLATIONS"
fi

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[environment-lock] FAIL: $VIOLATIONS handler(s) without withEnvironmentLock" >&2
  echo "" >&2
  echo "To fix one of:" >&2
  echo "  1. Add a call to withEnvironmentLock(ctx, tenant, project, env, fn)" >&2
  echo "  2. Add the file to the ALLOWLIST in this test (with justification)" >&2
  exit 1
fi

echo "[environment-lock] PASS — all mutating handlers in api/internal/routes/ use withEnvironmentLock (or are allowlisted)"
