#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#159
# (loopzero promote imprime URL sem prefixo de tenant que 404).
#
# Slug:        promote-canonical-url
# Issue:       loopzero-cloud#159
# Wired in:    task test:dogfood:promote-canonical-url
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
    echo "[dogfood-promote-canonical-url] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-promote-canonical-url-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
    if ( cd "$REPO_ROOT/api" && go test ./internal/services/ -run "TestPromote_URL_UsesCanonicalNamingFormula_RedTeam159|TestPromote_URL_EmptyWhenNoEntryApp_RedTeam159|TestPromote_PublicDomain_PreferredOverBase|TestPromote_PublicDomainEmpty_FallsBackToBaseDomain|TestPromote_HappyPath" -count=1 -timeout 60s ) ; then
        record "S2 Go unit (promotion_orchestrator #159 pair + regression)" PASS
    else
        record "S2 Go unit (promotion_orchestrator #159 pair + regression)" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-promote-canonical-url_test.sh" ; then
        record "S3 LIVE bash 3/3" PASS
    else
        record "S3 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-promote-canonical-url] Result: $PASS PASS, $FAIL FAIL"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
