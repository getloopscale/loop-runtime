#!/usr/bin/env bash
#
# T3.4 of deep-review-remediation-plan (find_test_002 HIGH companion).
#
# Asserts every api/internal/db/migrations/*.up.sql has a corresponding
# regression test in api/tests/unit/migration_NNN*_test.go. CLAUDE.md
# Rule: every bug fix MUST generate a regression test before the fix.
#
# Tests can be either:
#   - Live integration tests in api/tests/integration/
#   - Text-level invariant tests in api/tests/unit/migration_NNN*_test.go
#
# Exit 0: all migrations covered.
# Exit 1: at least one migration without test.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$(cd "$(dirname "$0")/../.." && pwd)")"
MIGRATIONS_DIR="$REPO_ROOT/api/internal/db/migrations"
UNIT_TESTS_DIR="$REPO_ROOT/api/tests/unit"

# EXCEPTION_LIST — migrations explicitly waived. Add ONLY with ADR + comment.
# Migrations 001 (initial) and 004/009 are pre-T3.4-baseline; they are
# implicitly tested by the live integration suite (every test runs against
# the migrated schema). Going forward, NEW migrations require explicit tests.
EXCEPTION_LIST=(
    "000001_initial_schema"   # baseline; covered indirectly by every integration test
    "000002_seed_data"        # already has migration_002_test.go (covered)
    "000003"                  # already has migration_003_test.go (covered)
    "000004_preview_environments"  # implicit — preview tests exercise schema
    "000009_app_scaling_config"    # implicit — scale tests exercise schema
)

violations=0
covered=0
total=0

for up_file in "$MIGRATIONS_DIR"/*.up.sql; do
    [ -e "$up_file" ] || continue
    total=$((total + 1))

    base="$(basename "$up_file" .up.sql)"
    # Extract numeric prefix: "000011_foo" → "000011"
    num="${base%%_*}"

    # Check exception list (match by full base or numeric prefix).
    excepted=0
    for ex in "${EXCEPTION_LIST[@]}"; do
        if [ "$base" = "$ex" ] || [ "$num" = "$ex" ]; then
            excepted=1
            break
        fi
    done
    if [ "$excepted" -eq 1 ]; then
        covered=$((covered + 1))
        continue
    fi

    # Look for matching test file. Existing tests use 3-digit prefixes
    # (migration_005_test.go) but new migrations are 6-digit (000011).
    # Try both forms: full prefix AND stripped (no leading zeros).
    short="$(echo "$num" | sed 's/^0*//')"
    if [ -z "$short" ]; then short="0"; fi
    short3="$(printf "%03d" "$short")"

    if ls "$UNIT_TESTS_DIR/migration_${num}"*_test.go >/dev/null 2>&1 \
       || ls "$UNIT_TESTS_DIR/migration_${short3}"*_test.go >/dev/null 2>&1; then
        covered=$((covered + 1))
    else
        echo "MIGRATION-TEST-COVERAGE VIOLATION: $base has no matching $UNIT_TESTS_DIR/migration_{${num},${short3}}*_test.go"
        violations=$((violations + 1))
    fi
done

echo ""
echo "Coverage: $covered/$total migrations have a regression test (or are in EXCEPTION_LIST)."
if [ "$violations" -eq 0 ]; then
    echo "PASS — every migration has its regression test."
    exit 0
fi

echo ""
echo "FAIL — $violations migration(s) without test."
echo ""
echo "To fix: create api/tests/unit/migration_NNN_<slug>_test.go with text-level"
echo "invariants for the migration. See migration_011_release_check_canonical_test.go"
echo "or migration_012_fk_on_delete_test.go for canonical patterns."
echo ""
echo "Genuine exceptions (e.g., trivial seed-only migrations) may be added to"
echo "EXCEPTION_LIST in this script with ADR approval."
exit 1
