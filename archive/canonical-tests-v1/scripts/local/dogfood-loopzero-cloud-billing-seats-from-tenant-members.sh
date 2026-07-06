#!/usr/bin/env bash
# Aggregate dogfood wrapper for Red Team loopzero-cloud#42 (billing seats
# counter sourced from tenant_members instead of append-only
# usage_records).
#
# Wires 2 suites:
#   S1 — hermetic syntax test    (CI, no go, no kubectl)
#   S2 — LIVE bash 3-scenario    (Go-side hermetic — no Cleanroom needed)
#
# NOTE on the cleanroom mandate: this triplet is Go-side hermetic. The
# cleanroom-up-live-test-mandate INQUEBRÁVEL only applies to LIVE tests
# against the dev-public dashboard surface. The #42 fix is a pure
# server-side billing aggregator wire-up exercised by `go test` (the
# pre-existing TestGetBilling_Seats_FromTenantMembers_RedTeam42 unit
# test), runnable on any developer workstation. Same shape as the #104
# sibling triplet.
#
# Slug:        loopzero-cloud-billing-seats-from-tenant-members
# Issue:       loopzero-cloud#42
# Wired in:    task test:dogfood:loopzero-cloud-billing-seats-from-tenant-members
# Registered:  dogfood-all.sh WRAPPERS array

set -euo pipefail

MODE="${MODE:-}"
JSON_OUTPUT=0
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--hermetic-only|--live-only] [--json] [--help]

Aggregate dogfood for loopzero-cloud#42 (billing seats from tenant_members):
  --hermetic-only   S1 (hermetic syntax) only (CI mode)
  --live-only       S2 (LIVE bash, Go-side) only
  (no flag)         both suites
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
    echo "[dogfood-loopzero-cloud-billing-seats-from-tenant-members] $verdict: $label"
}

if [ "$MODE" != "live" ] ; then
    if bash "$REPO_ROOT/infra/tests/check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax_test.sh" ; then
        record "S1 hermetic syntax" PASS
    else
        record "S1 hermetic syntax" FAIL
    fi
fi

if [ "$MODE" != "hermetic" ] ; then
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-loopzero-cloud-billing-seats-from-tenant-members_test.sh" ; then
        record "S2 LIVE bash 3/3" PASS
    else
        record "S2 LIVE bash 3/3" FAIL
    fi
fi

SUITES_JSON+="]"

echo "[dogfood-loopzero-cloud-billing-seats-from-tenant-members] ============================================================"
echo "[dogfood-loopzero-cloud-billing-seats-from-tenant-members] Result: $PASS PASS, $FAIL FAIL"
echo "[dogfood-loopzero-cloud-billing-seats-from-tenant-members] ============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"suites":%s}\n' "$PASS" "$FAIL" "$SUITES_JSON"
fi

exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
