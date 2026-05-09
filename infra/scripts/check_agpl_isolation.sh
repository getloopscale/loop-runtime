#!/usr/bin/env bash
#
# T9.1 of deep-review-remediation-plan (find_dep_madmin_agpl MEDIUM).
#
# Lints Go imports for AGPL-3.0 packages outside the explicit allow-list.
# Per ADR-0048, the only acceptable AGPL import is `minio/madmin-go` in
# `operators/internal/minio/provisioner.go` — the operator-binary-only
# trust boundary.
#
# This script prevents AGPL leakage into:
#   - api/    (Apache-2.0 boundary; tenants interact directly)
#   - cli/    (Apache-2.0 boundary; tenants run this on their machine)
#   - cloud/  (Apache-2.0 boundary; embedded as middleware)
#   - pkg/    (shared library; AGPL would propagate to api+cli+operators)
#
# Exit 0: no AGPL violations.
# Exit 1: AGPL package imported outside allow-list.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"

# AGPL packages we know about. Add any new AGPL discovery here.
AGPL_PATTERNS=(
    "minio/madmin-go"
)

# Allow-list: AGPL imports explicitly permitted (ADR-required).
# Format: <import-path>:<file-path>
# Each entry MUST link to an ADR.
ALLOWLIST=(
    "minio/madmin-go:operators/internal/minio/provisioner.go"  # ADR-0048
)

violations=0

for pattern in "${AGPL_PATTERNS[@]}"; do
    while IFS= read -r match; do
        file="${match%%:*}"
        # Strip REPO_ROOT prefix for canonical path
        rel="${file#$REPO_ROOT/}"

        # Check allow-list
        excepted=0
        for entry in "${ALLOWLIST[@]}"; do
            allowed_path="${entry##*:}"
            allowed_pattern="${entry%:*}"
            if [[ "$rel" == "$allowed_path" ]] && [[ "$pattern" == "$allowed_pattern" ]]; then
                excepted=1
                break
            fi
        done

        if [ "$excepted" -eq 0 ]; then
            echo "AGPL VIOLATION: $rel imports $pattern — outside ADR-0048 allow-list"
            violations=$((violations + 1))
        fi
    done < <(grep -rln "\"github.com/${pattern}" "$REPO_ROOT/api" "$REPO_ROOT/cli" "$REPO_ROOT/cloud" "$REPO_ROOT/pkg" "$REPO_ROOT/operators" 2>/dev/null | grep -v "_test\.go" || true)
done

if [ "$violations" -eq 0 ]; then
    echo "PASS — no AGPL imports outside ADR-0048 allow-list."
    exit 0
fi

echo ""
echo "FAIL — $violations AGPL violation(s)."
echo ""
echo "AGPL-3.0 packages have copyleft network-distribution requirements (§13)."
echo "If the import is necessary, either:"
echo "  (a) re-implement the needed subset using an Apache-2.0 alternative, OR"
echo "  (b) add the import to ALLOWLIST in this script with a NEW ADR + legal sign-off."
echo ""
echo "Existing ADR: docs/meetings/adr/0048-madmin-agpl-license-decision.md"
exit 1
