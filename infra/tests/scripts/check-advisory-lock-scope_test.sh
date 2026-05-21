#!/usr/bin/env bash
# T6.6 — Hermetic gate: forbid pg_advisory_lock( (session-scoped) usage.
# Only pg_advisory_xact_lock( (transaction-scoped) is allowed.
#
# Plan 2026-05-21-deep-review-remediation T6.6 + CLAUDE.md Rule 7.
#
# Background: session-scoped locks persist beyond transaction boundaries.
# With pgx pooling, a session lock acquired by request A can leak to
# request B that picks up the same pooled connection — silent deadlock
# OR cross-request mutex violation. Per CLAUDE.md Rule 7, ONLY
# pg_advisory_xact_lock is allowed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }

# Search code (not test files, not comments-only contexts).
# Match `pg_advisory_lock(` BUT NOT `pg_advisory_xact_lock(`.
while IFS= read -r match; do
  [ -z "$match" ] && continue
  # Skip pure-comment lines
  content="${match##*:}"
  if [[ "$content" =~ ^[[:space:]]*(//|#) ]]; then
    continue
  fi
  fail "$match"
done < <(
  grep -rEn "pg_advisory_lock\(" \
    api/ infra/scripts/ infra/helmfile/scripts/ 2>/dev/null \
    | grep -v "pg_advisory_xact_lock" \
    | grep -vE "_test\.|/test_helpers/" \
    || true
)

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[advisory-lock-scope] FAIL: $VIOLATIONS session-scoped pg_advisory_lock( call(s)" >&2
  echo "" >&2
  echo "Fix: replace with pg_advisory_xact_lock( — transaction-scoped" >&2
  echo "Per CLAUDE.md Rule 7, only the xact variant is allowed (pool-safe)." >&2
  exit 1
fi

echo "[advisory-lock-scope] PASS — no session-scoped pg_advisory_lock( calls in production code"
