#!/usr/bin/env bash
# Branch protection rule activation (EC-1 CRITICAL FIX).
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T7.4.
#
# Reads .github/branch-protection-config.yml + applies via gh api.
# Idempotent: re-running with same config is no-op.
#
# Phased rollout per ADR: enable on `develop` first (advisory 7d), then `main` (blocking).
#
# Env vars:
#   CONFIG_FILE  — YAML config (default: .github/branch-protection-config.yml)
#   BRANCH       — target branch (default: develop)
#   DRY_RUN      — "1" → preview JSON, no API call
#   REPO         — owner/name (default: loopruntime/loopruntime)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$REPO_ROOT/.github/branch-protection-config.yml}"
BRANCH="${BRANCH:-develop}"
DRY_RUN="${DRY_RUN:-0}"
REPO="${REPO:-loopruntime/loopruntime}"

if [ ! -f "$CONFIG_FILE" ]; then
  echo "[branch-protection] ERROR: config not found: $CONFIG_FILE" >&2
  exit 1
fi

# Parse YAML config (require yq)
if ! command -v yq >/dev/null 2>&1; then
  echo "[branch-protection] ERROR: yq required (install via 'go install github.com/mikefarah/yq/v4@latest')" >&2
  exit 1
fi

# Extract required status checks for this branch
CHECKS=$(yq ".branches.${BRANCH}.required_status_checks[]" "$CONFIG_FILE" 2>/dev/null || true)
if [ -z "$CHECKS" ]; then
  echo "[branch-protection] no checks defined for branch '$BRANCH' in $CONFIG_FILE — skip"
  exit 0
fi

echo "[branch-protection] $BRANCH required status checks:"
echo "$CHECKS" | sed 's/^/  /'

# Build JSON payload
CHECK_JSON_LIST=$(echo "$CHECKS" | python3 -c "
import sys, json
items = [line.strip() for line in sys.stdin if line.strip()]
print(json.dumps(items))
")

PAYLOAD=$(python3 -c "
import json
checks = $CHECK_JSON_LIST
payload = {
    'required_status_checks': {
        'strict': True,
        'contexts': checks,
    },
    'enforce_admins': False,
    'required_pull_request_reviews': {
        'dismiss_stale_reviews': True,
        'require_code_owner_reviews': False,
        'required_approving_review_count': 1,
    },
    'restrictions': None,
}
print(json.dumps(payload, indent=2))
")

if [ "$DRY_RUN" = "1" ]; then
  echo "[branch-protection] DRY_RUN — payload:"
  echo "$PAYLOAD"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "[branch-protection] ERROR: gh CLI required (install: https://cli.github.com)" >&2
  exit 1
fi

echo "[branch-protection] applying via gh api…"
echo "$PAYLOAD" | gh api -X PUT "/repos/$REPO/branches/$BRANCH/protection" --input - >/dev/null
echo "[branch-protection] PASS — branch protection updated for $BRANCH"
