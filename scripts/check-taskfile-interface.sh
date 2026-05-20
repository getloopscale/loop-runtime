#!/usr/bin/env bash
# check-taskfile-interface.sh — Plan T1.3 condition: validate that public
# Taskfile commands resolve identically before/after split.
#
# Per MEET-2026-05-03-062 (MEET-A) DX engineer condition #5: the Taskfile
# interface MUST be preserved across the split. If a public command goes
# missing during migration, this script fails CI.
#
# Usage:
#   bash scripts/check-taskfile-interface.sh
#
# Exit codes:
#   0  all expected commands resolve
#   1  ≥ 1 expected command missing
#   2  task binary not installed

set -euo pipefail

if ! command -v task >/dev/null 2>&1; then
    echo "ERROR: task binary not found. Install: https://taskfile.dev/installation/" >&2
    exit 2
fi

# Public commands that MUST resolve. These are referenced by:
#   - CI workflows (.github/workflows/*.yml)
#   - Developer onboarding docs (docs/runbooks/local-dev.md)
#   - CLAUDE.md (rule 9, Development Commands section)
#
# Adding to this list = strengthening the contract. Removing = breaking change.
EXPECTED=(
    setup
    doctor
    test
    k3d:heal
    dev:up
    db:migrate
    db:reset
    validate:matrix
    parity:preflight
    cli:link
    cli:build
)

# Resolve via `task --list-all`; format is "* <name>:    description"
mapfile -t available < <(task --list-all 2>/dev/null | awk '/^\*[[:space:]]/ {sub(/:$/, "", $2); print $2}')

declare -a missing=()
for cmd in "${EXPECTED[@]}"; do
    found=0
    for a in "${available[@]}"; do
        if [[ "$a" == "$cmd" ]]; then
            found=1
            break
        fi
    done
    if [[ $found -eq 0 ]]; then
        missing+=("$cmd")
    fi
done

if [[ ${#missing[@]} -eq 0 ]]; then
    echo "✅ Taskfile interface preserved — ${#EXPECTED[@]} public commands resolve."
    exit 0
fi

echo "❌ Taskfile interface BROKEN — ${#missing[@]} public command(s) missing:"
for m in "${missing[@]}"; do
    echo "  - $m"
done
echo
echo "Action: ensure these commands are defined in Taskfile.yml or in an included taskfiles/*.yml."
echo "If an EXPECTED command was intentionally renamed/removed, update both this script AND CI consumers."
exit 1
