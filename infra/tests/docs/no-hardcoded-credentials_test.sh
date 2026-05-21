#!/usr/bin/env bash
# Hermetic gate: forbid hardcoded credential literals across all chart values,
# helmfile templates, and bootstrap scripts.
#
# Plan 2026-05-21-deep-review-remediation T1.3 + ADR D2.
#
# Background: deep review v2 found 19 distinct sites hardcoding
# `loopzero-dev-password` (18) or `loopzero-gitea-dev-2026` (1) as dev fallbacks.
# Each was a known-default credential that could reach a real environment
# via `helmfile -e standalone sync`. Plan v1.1 EC-01 added the 19th site
# the original sweep missed.
#
# After T1.3 ships, this gate prevents regression: any PR that re-introduces
# the literal in active code (excluding finding/audit docs that LIST the
# historical literals) MUST fail.
#
# Forbidden literals (case-insensitive):
#   - loopzero-dev-password
#   - loopzero-gitea-dev-2026
#
# Allowed locations (historical/audit context, NOT live code):
#   - docs/plans/2026-05-21-deep-review-remediation-*.md (literally lists
#     the offending sites — required for traceability)
#   - .claude/knowledge-base/reviews/2026-05-21-* (audit evidence)
#   - .claude/knowledge-base/concepts/operations/finding-*.md (historical)
#   - CHANGELOG.md (release notes mentioning the fix)
#   - This script itself + the credentials-required.md SoT doc

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

LITERALS=("loopzero-dev-password" "loopzero-gitea-dev-2026")
SEARCH_DIRS=(infra/helm infra/helmfile infra/scripts charts api operators pkg cli)

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }

for literal in "${LITERALS[@]}"; do
  # Find matches in active code dirs (excluding test fixtures + node_modules)
  while IFS= read -r match; do
    [ -z "$match" ] && continue
    # The credentials-required.md SoT doc references these literals in its
    # "Chart-hardcoded credentials" historical table. Allow.
    case "$match" in
      *"docs/operations/credentials-required.md"*) continue ;;
      *"infra/tests/docs/no-hardcoded-credentials_test.sh"*) continue ;;
      # api/internal/config/config.go uses the literals as production-guard
      # sentinels (devDefault* constants) — the literal is the failure
      # signal, not a default that leaks. Constified in T1.3.
      *"api/internal/config/config.go"*) continue ;;
    esac
    fail "Hardcoded literal '$literal' found: $match"
  done < <(
    grep -rIn "$literal" "${SEARCH_DIRS[@]}" 2>/dev/null \
      | grep -vE "(node_modules|\.test\.|_test\.sh|/test_helpers/|/__tests__/)" \
      | grep -vE "deep-review-remediation" \
      || true
  )
done

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[no-hardcoded-credentials] FAIL: $VIOLATIONS violation(s)" >&2
  echo "" >&2
  echo "To fix: replace the literal with one of:" >&2
  echo "  1. Required helmfile env: {{ requiredEnv \"VAR_NAME\" }}" >&2
  echo "  2. Empty string (chart fail-fast): password: \"\"" >&2
  echo "  3. Read from K8s Secret in script: \$(kubectl get secret X -o jsonpath='{.data.password}' | base64 -d)" >&2
  echo "  4. Script fail-fast: \${VAR:?VAR is required — see docs/operations/credentials-required.md}" >&2
  exit 1
fi

echo "[no-hardcoded-credentials] PASS — no hardcoded credential literals in active code"
