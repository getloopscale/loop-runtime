#!/usr/bin/env bash
# check-no-migrate-force.sh
#
# CORRECTIONS.txt item B enforcement gate (plan T1.2, EC-2 broader scope):
# Fail the build if any production code path contains the literal pattern
# `migrate force` UNLESS the match is explicitly allowlisted by a comment
# marker on the same line OR within 3 lines BEFORE the match.
#
# Allowlist marker syntaxes:
#   # allowlist: migrate force ...     (shell, YAML, Python)
#   // allowlist: migrate force ...    (Go, TypeScript)
#
# Scope (EC-2 v1.1 amendment): ALL text files under the repo root, EXCEPT
# explicit exclusion list:
#   .git/, node_modules/, _legacy/, .test-reports/,
#   .test-reports-cleanroom/, dist/, build/
#
# Binary files are skipped via grep -I.
#
# Test harness: infra/tests/check-no-migrate-force_test.sh (13 cases).
#
# Exit codes:
#   0 — all matches allowlisted, or zero matches
#   1 — usage error
#   2 — at least one un-allowlisted match found
#
# Usage:
#   bash infra/scripts/check-no-migrate-force.sh                  # scan repo
#   bash infra/scripts/check-no-migrate-force.sh --root /tmp/x   # scan custom root (testing)

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --root) ROOT="$2"; shift 2 ;;
    --root=*) ROOT="${1#*=}"; shift ;;
    -h|--help)
      sed -n '1,30p' "$0"; exit 0 ;;
    *)
      echo "usage: $0 [--root <dir>]" >&2
      exit 1
      ;;
  esac
done

# Path exclusion list — anchored to start of the path-relative-to-root.
# Documentation paths (.claude/, docs/, review-output/) are excluded because
# they legitimately reference the pattern (war journal, this audit, this
# plan, etc). The linter targets PRODUCTION code paths (api/, operators/,
# infra/, .github/, cli/, pkg/, tools/).
EXCLUDE_DIRS=(
  ".git"
  "node_modules"
  "_legacy"
  ".test-reports"
  ".test-reports-cleanroom"
  "dist"
  "build"
  ".pulumi"
  "vendor"
  ".claude"
  "docs"
  "review-output"
)

# Self-exclusion: this script's own documentation legitimately mentions
# the pattern; don't scan ourselves.
SELF_PATH="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

# Build prune expression for find.
prune_expr=""
for d in "${EXCLUDE_DIRS[@]}"; do
  if [[ -n "$prune_expr" ]]; then
    prune_expr="$prune_expr -o "
  fi
  prune_expr="${prune_expr}-path '*/${d}' -o -path '*/${d}/*'"
done

# Step 1: collect every line that matches "migrate force" (case-insensitive,
# allowing arbitrary whitespace between the two words).
#
# Important: do NOT use grep -r against the repo from outside the eval —
# the eval is needed for the prune expression to work properly with find.
MATCHES_FILE=$(mktemp)
trap 'rm -f "$MATCHES_FILE"' EXIT

# Match pattern (compatible with prior inline ci.yml:444 gate but extended):
#   migrate(\s+-\S+)*\s+force  — matches 'migrate force' AND 'migrate -path X force'.
# Tightened: word boundary before 'migrate' to skip 'autom_igrate force' false positives.
eval "find \"$ROOT\" -type d \\( $prune_expr \\) -prune -o -type f -print" \
  | grep -vF "$SELF_PATH" \
  | grep -vE 'infra/tests/load/CORRECTIONS\.txt$' \
  | grep -vE 'infra/tests/check-no-migrate-force_test\.sh$' \
  | xargs -d '\n' grep -HInEi -e '(^|[^a-zA-Z0-9])migrate([[:space:]]+-[^[:space:]]+)*[[:space:]]+force' 2>/dev/null \
  | grep -v "^Binary file" \
  > "$MATCHES_FILE" || true

# allowlist: migrate force — example marker for this script's own
# documentation lines above; gate must permit this file's docs.

if [[ ! -s "$MATCHES_FILE" ]]; then
  echo "[no-migrate-force] no matches — OK."
  exit 0
fi

# Step 2: for each match, check if there's an allowlist marker on the SAME
# line OR within 3 lines BEFORE.
#
# Allowlist regex: (#|//) optional whitespace, then "allowlist:" then
# whitespace, then "migrate force".
ALLOWLIST_RE='(^|[^a-zA-Z0-9])(#|//)[[:space:]]*allowlist:[[:space:]]+migrate[[:space:]]+force'

BLOCKED_FILE=$(mktemp)
trap 'rm -f "$MATCHES_FILE" "$BLOCKED_FILE"' EXIT

while IFS= read -r match_line; do
  # match_line format: <path>:<lineno>:<content>
  path="${match_line%%:*}"
  rest="${match_line#*:}"
  lineno="${rest%%:*}"
  content="${rest#*:}"

  # If the match line itself contains the allowlist marker → permit.
  if echo "$content" | grep -qE "$ALLOWLIST_RE"; then
    continue
  fi

  # Otherwise check up to 3 lines BEFORE for the marker.
  if [[ -f "$path" ]]; then
    start=$(( lineno - 3 ))
    if [[ $start -lt 1 ]]; then start=1; fi
    end=$(( lineno - 1 ))
    if [[ $end -lt $start ]]; then
      # Match on line 1; no lines before.
      echo "$match_line" >> "$BLOCKED_FILE"
      continue
    fi
    if sed -n "${start},${end}p" "$path" 2>/dev/null | grep -qE "$ALLOWLIST_RE"; then
      continue
    fi
  fi

  echo "$match_line" >> "$BLOCKED_FILE"
done < "$MATCHES_FILE"

if [[ ! -s "$BLOCKED_FILE" ]]; then
  echo "[no-migrate-force] all $(wc -l < "$MATCHES_FILE") matches allowlisted — OK."
  exit 0
fi

n_blocked=$(wc -l < "$BLOCKED_FILE")
msg="check-no-migrate-force: $n_blocked unallowlisted occurrence(s) of 'migrate force' in production code. Per CORRECTIONS.txt item B (2026-04-09 stress test war journal), 'migrate force' on production DB from inside a pod corrupts schema_migrations. Use psql for ad-hoc SQL instead, OR add the comment marker '# allowlist: migrate force (rationale)' on the same line or within 3 lines above the match if this is a legitimate heal/recovery path."
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
  echo "::error::$msg"
else
  echo "[no-migrate-force] BLOCK: $msg" >&2
fi
echo "[no-migrate-force] blocked matches:" >&2
cat "$BLOCKED_FILE" >&2
exit 2
