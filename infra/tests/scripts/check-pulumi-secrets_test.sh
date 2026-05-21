#!/usr/bin/env bash
# T6.3 — Hermetic gate: any Pulumi `export const X = pulumi.interpolate\`...\``
# that embeds a sensitive Output<string> MUST be wrapped in pulumi.secret(...).
#
# Plan 2026-05-21-deep-review-remediation T6.3 + ADR D3.
#
# Background: T0.4 fixed 4 drill DSN exports leaking ${drillPG.password}.
# This gate prevents regression of that class of bug across all Pulumi
# projects.
#
# Sensitive embedding patterns (case-insensitive substring):
#   .password, .secret, .token, .key (when value, not name)
#   adminPassword, apiToken, accessKey, secretAccessKey

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }
info() { echo "[pulumi-secrets] $*"; }

# Sensitive value substrings — match interpolated values that look like secrets.
SENSITIVE_PATTERNS=(
  '\.password'
  '\.secret'
  '\.token'
  '\.apiKey'
  '\.accessKey'
  '\.secretAccessKey'
  '\.privateKey'
  '\.adminPassword'
)

TOTAL_EXPORTS=0
SECRET_WRAPPED=0
OK_NON_SENSITIVE=0

for ts_file in $(find infra/pulumi*/src -name '*.ts' -not -path '*/__tests__/*' -not -path '*/node_modules/*' 2>/dev/null); do
  while IFS= read -r match; do
    [ -z "$match" ] && continue
    line_num="${match%%:*}"
    line_text="${match#*:}"
    TOTAL_EXPORTS=$((TOTAL_EXPORTS + 1))

    # Is the export already pulumi.secret()-wrapped?
    if echo "$line_text" | grep -q 'pulumi\.secret('; then
      SECRET_WRAPPED=$((SECRET_WRAPPED + 1))
      continue
    fi

    # Is the interpolation actually embedding a sensitive value?
    is_sensitive=0
    for pat in "${SENSITIVE_PATTERNS[@]}"; do
      if echo "$line_text" | grep -qE "$pat"; then
        is_sensitive=1
        break
      fi
    done

    if [ "$is_sensitive" -eq 0 ]; then
      OK_NON_SENSITIVE=$((OK_NON_SENSITIVE + 1))
      continue
    fi

    fail "$ts_file:$line_num — export with pulumi.interpolate embeds sensitive value but not wrapped in pulumi.secret()"
  done < <(grep -nE '^[[:space:]]*export const [a-zA-Z_]+\s*=\s*(pulumi\.secret\()?pulumi\.interpolate' "$ts_file" 2>/dev/null || true)
done

info "Total Pulumi interpolate exports: $TOTAL_EXPORTS"
info "Already pulumi.secret-wrapped: $SECRET_WRAPPED"
info "Non-sensitive (allowed bare): $OK_NON_SENSITIVE"
info "Violations: $VIOLATIONS"

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "" >&2
  echo "Fix: wrap each violating export in pulumi.secret():" >&2
  echo "  export const X = pulumi.secret(pulumi.interpolate\`...\`);" >&2
  echo "Per ADR D3: any Pulumi output embedding Output<string> of sensitive" >&2
  echo "value MUST be pulumi.secret-wrapped." >&2
  exit 1
fi

echo "[pulumi-secrets] PASS — all sensitive Pulumi exports are pulumi.secret-wrapped"
