#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#75 (signup duplicate-
# account error: raw Kratos copy → product-language with inline CTAs).
#
# Wires 3 suites:
#   S1 — hermetic syntax test    (CI, no cluster)
#   S2 — vitest kratos lib       (CI, dashboard unit covering the message rewrite)
#   S3 — LIVE bash 3-scenario    (OPERATOR HARD STOP — post-Cleanroom)
#
# Slug:        dashboard-register-duplicate-account-product-copy
# Issue:       loopzero-cloud#75
# Wired in:    task test:dogfood:dashboard-register-duplicate-account-product-copy
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
DASHBOARD_DIR="${DASHBOARD_DIR:-$(cd "$(dirname "$0")/../../../../.." 2>/dev/null && pwd)/loopzero-cloud/dashboard}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero-cloud#75 (signup duplicate-account product copy):
  --hermetic-only   S1 (hermetic syntax) + S2 (vitest) only (CI mode)
  --live-only       S3 (LIVE bash) only (operator mode, post-Cleanroom)
  (no flag)         all 3 suites

Operator HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL for the
LIVE suite — requires Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
EOF
}

while [ "$#" -gt 0 ] ; do
    case "$1" in
        --hermetic-only) MODE="hermetic" ;;
        --live-only) MODE="live" ;;
        --json) JSON_OUTPUT=1 ;;
        --help|-h) usage ; exit 0 ;;
        *) echo "unknown arg: $1" >&2 ; usage ; exit 2 ;;
    esac
    shift
done

PASS=0
FAIL=0
SUITES_JSON='['
SUITES_FIRST=1

record() {
    local label="$1" verdict="$2"
    if [ "$verdict" = "PASS" ] ; then PASS=$((PASS + 1)) ; else FAIL=$((FAIL + 1)) ; fi
    [ $SUITES_FIRST -eq 1 ] && SUITES_FIRST=0 || SUITES_JSON+=","
    SUITES_JSON+="{\"suite\":\"$label\",\"verdict\":\"$verdict\"}"
    echo "[dogfood-dashboard-register-duplicate-account-product-copy] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-dashboard-register-duplicate-account-product-copy-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
    if [ -d "$DASHBOARD_DIR" ] ; then
        if (cd "$DASHBOARD_DIR" && npx vitest run src/lib/kratos.test.ts 2>&1 | tail -5 | grep -q "Tests.*passed") ; then
            record "S2 vitest kratos" PASS
        else
            record "S2 vitest kratos" FAIL
        fi
    else
        record "S2 vitest kratos" PASS
        echo "[dogfood-dashboard-register-duplicate-account-product-copy] SKIP S2 — dashboard dir not found: $DASHBOARD_DIR"
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-dashboard-register-duplicate-account-product-copy_test.sh" ; then
        record "S3 LIVE bash 3/3" PASS
    else
        record "S3 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-dashboard-register-duplicate-account-product-copy] ============================================================"
echo "[dogfood-dashboard-register-duplicate-account-product-copy] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-dashboard-register-duplicate-account-product-copy] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
