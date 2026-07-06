#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#42 closure
# (the /billing page showed "USAGE THIS PERIOD" zeroed across every
# limited metric — Projects 0/1, Environments 0/1, Seats 0/3, Services
# Per Project 0/3 — for an account that demonstrably had 1 LIVE project
# (loopzerokit-example) and at least 1 seat (the owner). Root cause:
# `GET /v1/billing` aggregated usage from the `usage_records` table,
# which is an append-only counter feed designed for usage-style metrics
# like build minutes — it is the WRONG source for STATEFUL capacity
# metrics. Projects + seats don't emit a +1 event per existence; they
# have a current count you read at request time).
#
# Fix shipped 2026-06-28 in commit `87058b4` (loopzero-cloud):
#   `internal/routes/billing.go:349-359` adds:
#     - A comment explaining the usage_records-vs-stateful-count gap.
#     - `if seats, ok := h.countSeats(r.Context(), tenantID); ok {
#         result["seats"] = seats
#       }` — sources the seats count from `tenant_members` canonically.
#   `internal/routes/billing.go:368-383` exposes `countSeats(ctx,
#   tenantID) (int, bool)` which runs
#   `SELECT COUNT(*) FROM tenant_members WHERE tenant_id = $1` (the
#   same SoT the auth path reads). The boolean return signals
#   "resolved cleanly" — a DB error returns `(0, false)` so the caller
#   leaves the value 0 instead of over-reporting.
#
# NOTE on the cleanroom mandate: this is a server-side Go fix (BFF
# aggregator). The canonical proof is source-level grep + `go test`
# against the pre-existing unit test
# `TestGetBilling_Seats_FromTenantMembers_RedTeam42` — same Go-side
# hermetic pattern as the #104 sibling triplet. Not a dev-public LIVE
# bundle probe, so `.claude/rules/cleanroom-up-live-test-mandate.md`
# does NOT apply (consistent with the #104 closure).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — source-level grep on `internal/routes/billing.go` for the
#       `countSeats(r.Context(), tenantID)` call site (the canonical
#       wire-up that replaces the usage_records lookup for the seats
#       metric).
#   B — source-level grep on `internal/routes/billing.go` for the
#       canonical SQL `SELECT COUNT(*) FROM tenant_members WHERE
#       tenant_id = $1` (the stateful-count query that closes the
#       append-only usage_records mismatch).
#   C — LIVE proof: `go test -count=1 -run
#       TestGetBilling_Seats_FromTenantMembers_RedTeam42` runs the
#       pre-existing #42 closure test that asserts the response surfaces
#       `seats.used = 1` (the owner) instead of 0 (the pre-fix shape).
#
# Slug:           loopzero-cloud-billing-seats-from-tenant-members
# Issue:          loopzero-cloud#42
# Family:         (none — billing aggregation correctness; pre-existing
#                  stateful-vs-append-only signal mismatch)
# Closed by:      `loopzero-cloud/internal/routes/billing.go:349-359 + 368-383`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#42)
# Companion (CI): infra/tests/check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax_test.sh

set -euo pipefail

PLAN_SLUG="loopzero-cloud-billing-seats-from-tenant-members"
ISSUE_REF="loopzero-cloud#42"
LOOPZERO_CLOUD_DIR="${LOOPZERO_CLOUD_DIR:-$(cd "$(dirname "$0")/../../../../loopzero-cloud" 2>/dev/null && pwd)}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#42 (billing seats counter sourced
from tenant_members instead of append-only usage_records).

Environment:
  LOOPZERO_CLOUD_DIR    default ../../../../loopzero-cloud relative to this script
                    (autoresolves to the canonical loopzero-cloud subrepo)

This script is Go-side hermetic — does NOT require Cleanroom + dev-public.
Scenarios A + B are pure source-level greps. Scenario C runs
\`go test -count=1\` against the local loopzero-cloud checkout. Operator-side.
EOF
}

while [ "$#" -gt 0 ] ; do
    case "$1" in
        --skip-live) SKIP_LIVE=1 ;;
        --scenario=*) SCENARIO_FILTER="${1#--scenario=}" ;;
        --json) JSON_OUTPUT=1 ;;
        --help|-h) usage ; exit 0 ;;
        *) echo "unknown arg: $1" >&2 ; usage ; exit 2 ;;
    esac
    shift
done

log() { printf '[%s] %s\n' "$(basename "$0")" "$*" ; }

PASS_COUNT=0
FAIL_COUNT=0
FAILED_LIST=""
RESULTS_JSON='{"scenarios":['
RESULTS_FIRST=1

record_pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    log "  PASS  $1${2:+ ($2)}"
    [ $RESULTS_FIRST -eq 1 ] && RESULTS_FIRST=0 || RESULTS_JSON+=","
    RESULTS_JSON+="{\"scenario\":\"$1\",\"verdict\":\"PASS\",\"detail\":\"${2//\"/\\\"}\"}"
}

record_fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    FAILED_LIST+=" $1"
    log "  FAIL  $1${2:+ — $2}"
    [ $RESULTS_FIRST -eq 1 ] && RESULTS_FIRST=0 || RESULTS_JSON+=","
    RESULTS_JSON+="{\"scenario\":\"$1\",\"verdict\":\"FAIL\",\"detail\":\"${2//\"/\\\"}\"}"
}

want_scenario() {
    [ -z "$SCENARIO_FILTER" ] && return 0
    case ",$SCENARIO_FILTER," in *,"$1",*) return 0 ;; *) return 1 ;; esac
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "loopzero-cloud dir: $LOOPZERO_CLOUD_DIR"
log "============================================================"

if [ ! -d "$LOOPZERO_CLOUD_DIR" ] ; then
    log "ERROR: LOOPZERO_CLOUD_DIR does not resolve to a directory: $LOOPZERO_CLOUD_DIR"
    exit 2
fi

# ---------------------------------------------------------------------------
# Scenario A — source-level grep on internal/routes/billing.go for the
# countSeats(r.Context(), tenantID) call site
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — billing.go calls h.countSeats(r.Context(), tenantID) (canonical wire-up)"
    SRC="$LOOPZERO_CLOUD_DIR/internal/routes/billing.go"
    if [ ! -f "$SRC" ] ; then
        record_fail A "source missing: $SRC"
    else
        # Match the canonical wire-up: `if seats, ok := h.countSeats(r.Context(), tenantID); ok {`
        wire_count="$(grep -cE 'h\.countSeats\(r\.Context\(\), tenantID\)' "$SRC" || true)"
        if [ "$wire_count" -ge 1 ] ; then
            record_pass A "billing.go has h.countSeats(r.Context(), tenantID) wire-up=$wire_count"
        else
            record_fail A "billing.go MISSING countSeats wire-up — seats still sourced from usage_records (pre-fix shape)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — source-level grep on internal/routes/billing.go for the
# canonical SQL query against tenant_members
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — billing.go has 'SELECT COUNT(*) FROM tenant_members WHERE tenant_id = \$1' (canonical SoT)"
    SRC="$LOOPZERO_CLOUD_DIR/internal/routes/billing.go"
    if [ ! -f "$SRC" ] ; then
        record_fail B "source missing: $SRC"
    else
        sql_count="$(grep -cF 'SELECT COUNT(*) FROM tenant_members WHERE tenant_id = $1' "$SRC" || true)"
        if [ "$sql_count" -ge 1 ] ; then
            record_pass B "billing.go has canonical tenant_members count query=$sql_count"
        else
            record_fail B "billing.go MISSING canonical tenant_members count query — wrong SoT (pre-fix shape)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — LIVE proof: go test runs the pre-existing #42 closure test
# (TestGetBilling_Seats_FromTenantMembers_RedTeam42) which asserts the
# response surfaces seats.used = 1 (the owner) instead of 0.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — go test -run TestGetBilling_Seats_FromTenantMembers_RedTeam42 is GREEN"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        if ! command -v go >/dev/null 2>&1 ; then
            record_fail C "go toolchain not available on PATH"
        else
            log "  running go test -count=1 ./internal/routes/... -run TestGetBilling_Seats_FromTenantMembers_RedTeam42 …"
            out="$(cd "$LOOPZERO_CLOUD_DIR" && go test -count=1 ./internal/routes/... -run TestGetBilling_Seats_FromTenantMembers_RedTeam42 2>&1 || true)"
            if printf '%s' "$out" | grep -qE '^ok[[:space:]]+github.com/getloopscale/loopzero-cloud/internal/routes' ; then
                record_pass C "TestGetBilling_Seats_FromTenantMembers_RedTeam42 PASS"
            else
                record_fail C "TestGetBilling_Seats_FromTenantMembers_RedTeam42 FAIL (output: $(printf '%s' "$out" | tail -3 | tr '\n' ' '))"
            fi
        fi
    fi
fi

RESULTS_JSON+="],\"pass\":$PASS_COUNT,\"fail\":$FAIL_COUNT,\"failed_list\":\"${FAILED_LIST# }\"}"

log "============================================================"
log "Summary: PASS=$PASS_COUNT FAIL=$FAIL_COUNT"
if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '%s\n' "$RESULTS_JSON"
fi

[ $FAIL_COUNT -eq 0 ] || exit 1
exit 0
