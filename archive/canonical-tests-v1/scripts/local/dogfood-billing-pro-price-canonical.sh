#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#157 (preço divergente
# — botão $29 vs Stripe cobrando US$ 19,00).
#
# Slug:        billing-pro-price-canonical
# Issue:       loopzero-cloud#157
# Wired in:    task test:dogfood:billing-pro-price-canonical
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

usage() { cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]
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
    echo "[dogfood-billing-pro-price-canonical] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-billing-pro-price-canonical-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-billing-pro-price-canonical_test.sh" ; then
        record "S2 LIVE bash 2/2" PASS
    else
        record "S2 LIVE bash 2/2" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-billing-pro-price-canonical] Result: $PASS PASS, $FAIL FAIL"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
