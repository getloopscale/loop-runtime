#!/usr/bin/env bash
#
# T4.1 of deep-review-remediation-plan (closes find_cicd_001 HIGH).
#
# Lints all .github/workflows/*.yml for non-local actions referenced by tag
# instead of commit SHA. Tag-pinned actions create a supply-chain takeover
# surface: an attacker with push access to the source repo can replace
# the tag's target without any version bump in our workflows.
#
# Allowed:
#   - SHA-pinned: `uses: owner/repo@<40-hex>`
#   - Local actions: `uses: ./...`
#   - Local reusable workflows: `uses: ./.github/workflows/...`
#
# Forbidden (default):
#   - `uses: owner/repo@v1` / `@v2.6.1` / `@main` / `@latest`
#
# Exception list: actions explicitly tracked as floating-pin under
# INF-G18 (see docs/runbooks/action-sha-pinning.md). These appear in
# `EXCEPTION_PATTERNS` below and SHOULD be reviewed quarterly.
#
# Exit 0: all non-local actions are SHA-pinned (or in exception list).
# Exit 1: at least one tag-pinned action found outside the exception list.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"
WORKFLOWS_DIR="$REPO_ROOT/.github/workflows"
ACTIONS_DIR="$REPO_ROOT/.github/actions"

# EXCEPTION_PATTERNS — actions explicitly allowed to use tag pinning.
# Each entry is a substring match against the `uses:` line. Add only with
# explicit ADR / governance approval. Empty by default; the goal is to
# eventually pin every action to SHA + Renovate digest updates.
EXCEPTION_PATTERNS=()

violations=0
total_uses=0

while IFS= read -r line_with_file; do
    # line_with_file = "<path>:<lineno>:<content>"
    file="${line_with_file%%:*}"
    rest="${line_with_file#*:}"
    lineno="${rest%%:*}"
    content="${rest#*:}"

    total_uses=$((total_uses + 1))

    # Strip leading whitespace, comments, "- " / "uses: " prefix
    trimmed="$(echo "$content" | sed -E 's/^[[:space:]]*-?[[:space:]]*uses:[[:space:]]*//; s/[[:space:]]*#.*$//')"

    # Skip local actions and reusable workflows
    case "$trimmed" in
        ./*) continue ;;
        '') continue ;;
    esac

    # Honor `# pragma: allow-tag-pin` placed within 3 lines BEFORE the uses:
    # line. Existing convention in the repo (see ci.yml TruffleHog comment).
    pragma_start=$((lineno - 3))
    if [ "$pragma_start" -lt 1 ]; then pragma_start=1; fi
    if sed -n "${pragma_start},${lineno}p" "$file" | grep -q "pragma: allow-tag-pin"; then
        continue
    fi

    # Must contain @<40-hex> for SHA pinning
    if ! echo "$trimmed" | grep -qE '@[a-f0-9]{40}'; then
        # Check exception list
        excepted=0
        for pat in "${EXCEPTION_PATTERNS[@]}"; do
            if [[ "$trimmed" == *"$pat"* ]]; then
                excepted=1
                break
            fi
        done

        if [ "$excepted" -eq 0 ]; then
            echo "TAG-PIN VIOLATION: $file:$lineno: $trimmed"
            violations=$((violations + 1))
        fi
    fi
done < <(grep -nE '^[[:space:]]*-?[[:space:]]*uses:' "$WORKFLOWS_DIR"/*.yml "$ACTIONS_DIR"/*/action.yml 2>/dev/null || true)

echo ""
echo "Scanned $total_uses 'uses:' references."
if [ "$violations" -eq 0 ]; then
    echo "PASS — all non-local actions are SHA-pinned."
    exit 0
fi

echo "FAIL — $violations tag-pinned action(s) found."
echo ""
echo "To fix: replace the tag with a commit SHA. Example:"
echo "  uses: actions/checkout@v4"
echo "  → uses: actions/checkout@a5ac7e51b41094c92402da3b24376905380afc29  # v4.1.6"
echo ""
echo "Renovate (configured with pinDigests: true) auto-bumps SHAs weekly."
echo ""
echo "Genuine exceptions (e.g., quickly-evolving sigstore/cosign actions)"
echo "may be added to EXCEPTION_PATTERNS in this script with ADR approval."
exit 1
