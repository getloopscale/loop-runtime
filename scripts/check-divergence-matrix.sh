#!/usr/bin/env bash
# check-divergence-matrix.sh — Plan T5.2 gate.
#
# Verifies invariant: every code marker `LOCAL ONLY (LP-NNN)` has a matching
# entry in the divergence matrix. Flags:
#
#   1. Marker references unknown LP-NNN (typo OR matrix missing the entry)
#   2. Matrix entry exists but no code references it (orphan documentation)
#
# Exit codes:
#   0  matrix integrity GREEN
#   1  >= 1 violation
#   2  usage error / matrix missing
#
# 2026-05-20 path migration (CI/local parity bug fix): the original location
# 'docs/runbooks/local-prod-divergence-matrix.md' was deleted in the
# 2026-05-11 docs cleanup (commit fc1793dd). Canonical operational refs
# now live under .claude/knowledge-base/concepts/operations/. CI infra-gates.yml
# divergence-matrix job was silently failing with 'ERROR: ... not found'
# because this script path was not updated alongside the docs cleanup.
# Fix: primary path moved to knowledge-base; legacy path kept as fallback
# with WARN for transitional branches.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MATRIX="${REPO_ROOT}/.claude/knowledge-base/concepts/operations/local-prod-divergence-matrix.md"

# Backward-compat fallback to legacy path
if [[ ! -f "${MATRIX}" && -f "${REPO_ROOT}/docs/runbooks/local-prod-divergence-matrix.md" ]]; then
    MATRIX="${REPO_ROOT}/docs/runbooks/local-prod-divergence-matrix.md"
    echo "WARN: using legacy matrix path ${MATRIX} — please move to .claude/knowledge-base/concepts/operations/" >&2
fi

if [[ ! -f "${MATRIX}" ]]; then
    echo "ERROR: ${MATRIX} not found." >&2
    exit 2
fi

mapfile -t matrix_ids < <(grep -oE "LP-[0-9]{3}" "${MATRIX}" | sort -u)

mapfile -t code_markers < <(
    grep -rEon "LOCAL ONLY[[:space:]]+\(LP-[0-9]{3}\)" \
        "${REPO_ROOT}/api" \
        "${REPO_ROOT}/operators" \
        "${REPO_ROOT}/cli/src" \
        "${REPO_ROOT}/pkg" \
        "${REPO_ROOT}/infra" \
        "${REPO_ROOT}/scripts" \
        2>/dev/null \
        | grep -vE "node_modules/|/dist/|/vendor/|\.knip-cache/" \
        || true
)

declare -a violations=()

for marker in "${code_markers[@]}"; do
    id=$(echo "$marker" | grep -oE "LP-[0-9]{3}")
    if ! printf '%s\n' "${matrix_ids[@]}" | grep -qx "$id"; then
        violations+=("UNKNOWN_ID: ${marker}")
    fi
done

while IFS= read -r line; do
    id=$(echo "$line" | grep -oE "LP-[0-9]{3}" | head -1)
    [[ -z "$id" ]] && continue
    if echo "$line" | grep -q "OPEN"; then
        if ! printf '%s\n' "${code_markers[@]}" | grep -q "$id"; then
            violations+=("ORPHAN_MATRIX_ENTRY: ${id} (OPEN status but no code marker found)")
        fi
    fi
done < <(grep -E "^\| LP-[0-9]{3}" "${MATRIX}")

if [[ ${#violations[@]} -eq 0 ]]; then
    echo "GREEN: matrix integrity GREEN (${#matrix_ids[@]} entries, ${#code_markers[@]} markers)"
    exit 0
fi

echo "FAILED: ${#violations[@]} issue(s):"
for v in "${violations[@]}"; do
    echo "  ${v}"
done
exit 1
