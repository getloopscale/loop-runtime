#!/usr/bin/env bash
# check-action-pinning.sh — INF-G18 gate: enforce SHA pinning of GitHub Actions.
#
# Per `.claude/rules/pre-release-governance.md` and Plan ADR D3, all `uses:`
# references in `.github/workflows/*.yml` MUST pin to a 40-char commit SHA,
# never to a mutable tag. The tj-actions/changed-files supply-chain attack
# of March 2025 demonstrated that tag-pinning is exploitable.
#
# Exemptions:
#   1. Internal reusable workflows: `uses: ./.github/workflows/*.yml` (relative path)
#      — these don't traverse a registry, so SHA-pinning is N/A.
#   2. Explicit override: a comment `# pragma: allow-tag-pin` immediately
#      preceding the `uses:` line declares the line is exempt. Should be rare.
#
# Exit codes:
#   0  all `uses:` lines are SHA-pinned or exempted
#   1  ≥ 1 violation found
#   2  usage error

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKFLOWS_DIR="${REPO_ROOT}/.github/workflows"

if [[ ! -d "${WORKFLOWS_DIR}" ]]; then
    echo "ERROR: ${WORKFLOWS_DIR} not found." >&2
    exit 2
fi

# A `uses:` line takes one of these shapes:
#   1. uses: <owner>/<repo>@<sha40>             — SHA-pinned (compliant)
#   2. uses: <owner>/<repo>@<sha40> # v<x.y.z>  — SHA-pinned with version comment
#   3. uses: <owner>/<repo>@v<x.y.z>            — TAG-PINNED (violation)
#   4. uses: <owner>/<repo>@<branch>            — branch ref (treated as violation)
#   5. uses: ./.github/workflows/foo.yml        — internal reusable (exempt)
#
# Algorithm: extract the ref token after `@`, before space/EOL. If it's exactly
# 40 hex characters, COMPLIANT. Otherwise (tag, semver, branch, short hash),
# VIOLATION unless `# pragma: allow-tag-pin` precedes the line.
USES_LINE_RE='^[[:space:]]*uses:[[:space:]]+([^[:space:]]+)'

violations=()

# Pragma window: any of the last N comment-only lines preceding `uses:` may
# carry the pragma. Walking up to 4 prior lines accommodates patterns like:
#   # pragma: allow-tag-pin
#   # TODO(INF-G18 / plan T1.1): Renovate Bot resolves to @<sha40> on first run.
#   uses: foo/bar@v1
PRAGMA_LOOKBACK=4

while IFS= read -r workflow; do
    declare -a prev_lines=()
    line_num=0
    while IFS= read -r line; do
        line_num=$((line_num + 1))

        # Maintain a sliding window of the prior PRAGMA_LOOKBACK lines.
        prev_lines+=("$line")
        if [[ ${#prev_lines[@]} -gt $((PRAGMA_LOOKBACK + 1)) ]]; then
            prev_lines=("${prev_lines[@]:1}")
        fi

        if [[ ! "$line" =~ $USES_LINE_RE ]]; then
            continue
        fi

        ref="${BASH_REMATCH[1]}"

        # Skip internal reusable workflows (relative paths).
        if [[ "$ref" == ./* ]] || [[ "$ref" == .github/* ]]; then
            continue
        fi

        # Skip lines without an @ — Docker images don't use this format here,
        # but defensive against malformed yaml comments.
        if [[ "$ref" != *@* ]]; then
            continue
        fi

        # Extract token after @.
        token="${ref##*@}"

        # Compliant when token is exactly 40 hex chars (commit SHA).
        if [[ "$token" =~ ^[0-9a-f]{40}$ ]]; then
            continue
        fi

        # Pragma override: look back up to PRAGMA_LOOKBACK lines for the marker.
        # Iteration only considers comment-only lines (#-prefixed); a non-comment
        # line breaks the pragma association.
        pragma_found=0
        # Iterate prev_lines in reverse, excluding the current line itself (last).
        for ((idx = ${#prev_lines[@]} - 2; idx >= 0; idx--)); do
            pl="${prev_lines[$idx]}"
            # Stop if we hit a non-comment, non-blank line (pragma association ends).
            if [[ ! "$pl" =~ ^[[:space:]]*(#|$) ]]; then
                break
            fi
            if [[ "$pl" =~ ^[[:space:]]*\#[[:space:]]*pragma:[[:space:]]*allow-tag-pin ]]; then
                pragma_found=1
                break
            fi
        done
        if [[ $pragma_found -eq 1 ]]; then
            continue
        fi

        violations+=("${workflow}:${line_num}: ${line}")
    done < "$workflow"
done < <(find "${WORKFLOWS_DIR}" -maxdepth 1 -name "*.yml" -type f | sort)

if [[ ${#violations[@]} -eq 0 ]]; then
    echo "✅ INF-G18 GREEN — 0 tag-pinned actions in $(find "${WORKFLOWS_DIR}" -maxdepth 1 -name '*.yml' | wc -l) workflows."
    exit 0
fi

echo "❌ INF-G18 FAILED — ${#violations[@]} tag-pinned action(s):"
echo
for v in "${violations[@]}"; do
    echo "  ${v}"
done
echo
echo "Action: replace each \`@v<N>\` with \`@<sha40>\` (40-char commit SHA)."
echo "Look up SHA via: gh api repos/<owner>/<repo>/git/refs/tags/<tag> | jq -r .object.sha"
echo
echo "If a line is genuinely exempt, prepend the line with: # pragma: allow-tag-pin"
echo "(rare — only for internal reusable workflows or other documented edge cases)"
exit 1
