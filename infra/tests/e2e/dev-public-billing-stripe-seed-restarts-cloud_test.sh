#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero/loopzero#154 closure
# (Billing: upgrade Starter→Pro morto — /v1/billing e /v1/billing/checkout
# retornam 404).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #154 verbatim:
#   * Root cause: seed-stripe-api-keys-canonical.sh (Step 15b) materializes
#     Secret loopzero-stripe-api-keys but does NOT roll out loopzero-cloud pod.
#     The pod boot-order runs BEFORE the seed → cfg.StripeAPIKey empty
#     at startup → billingHandler == nil → /v1/billing routes never
#     registered → 404 "page not found" text/plain from the chi router.
#   * Pre-fix Red Team observation (2026-07-01 ~15:00Z):
#       POST /v1/billing/checkout body {"plan":"pro"} → 404
#       GET /v1/billing                                → 404
#       body: "404 page not found"  (text/plain, not JSON)
#   * Post-fix: seed-stripe-api-keys-canonical.sh restarts loopzero-cloud
#     post-Secret; billing enabled; /v1/billing* respond 401 without JWT
#     (route exists) OR 200 with valid JWT.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — GET /v1/billing responds 401 JSON (NOT 404 text/plain) — the route
#       is registered; auth guard rejects unauth request with typed error.
#   B — POST /v1/billing/checkout responds 401 JSON (NOT 404) — the
#       critical upgrade endpoint is wired.
#   C — loopzero-cloud logs contain "billing enabled" (NOT "STRIPE_API_KEY
#       not set — billing endpoints disabled") — proves the boot-order
#       fix took effect. Requires kubectl access.
#
# Slug:           billing-stripe-seed-restarts-cloud
# Issue:          loopzero/loopzero#154
# Family:         seed-broker-canonical.sh established the canonical restart pattern
# Closed by:      infra/scripts/local/seed-stripe-api-keys-canonical.sh (rollout restart)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#154)
# Companion (CI): infra/tests/check-seed-stripe-api-keys-canonical-syntax_test.sh (extended with #154 assertions)

set -euo pipefail

PLAN_SLUG="billing-stripe-seed-restarts-cloud"
ISSUE_REF="loopzero/loopzero#154"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero/loopzero#154 (billing 404 upgrade dead).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  MGMT_CONTEXT      default k3d-loopzero-mgmt

Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: run AFTER Cleanroom TOTAL
+ UP + seed-post-cleanroom-all.sh. Operator-side. Not for CI.
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
log "PUBLIC_CLOUD_URL: $PUBLIC_CLOUD_URL"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — GET /v1/billing returns 401 JSON (route exists, auth guard on).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — GET /v1/billing responds 401 JSON (route registered)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        # No JWT sent — should be 401 JSON (route wired) not 404 text/plain (route absent).
        code=$(curl -s -o /tmp/scA.txt -w '%{http_code}' --max-time 8 "${PUBLIC_CLOUD_URL}/v1/billing" 2>&1 || true)
        ct=$(curl -s -I --max-time 8 "${PUBLIC_CLOUD_URL}/v1/billing" 2>&1 | awk 'tolower($1)=="content-type:"{print $2}' | head -1 | tr -d '\r')
        body_head=$(head -c 200 /tmp/scA.txt | tr -d '\n' | tr -d '\r')
        if [ "$code" = "401" ] && [[ "$ct" == application/json* ]] ; then
            record_pass A "HTTP 401 JSON — route wired"
        elif [ "$code" = "404" ] ; then
            record_fail A "HTTP 404 (route absent — #154 pre-fix state)"
        else
            record_fail A "HTTP $code ct=$ct body=${body_head:0:80}"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — POST /v1/billing/checkout returns 401 JSON (critical endpoint).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — POST /v1/billing/checkout responds 401 JSON (critical route)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        code=$(curl -s -o /tmp/scB.txt -w '%{http_code}' --max-time 8 \
            -X POST -H 'Content-Type: application/json' \
            -d '{"plan":"pro"}' \
            "${PUBLIC_CLOUD_URL}/v1/billing/checkout" 2>&1 || true)
        ct=$(curl -s -I --max-time 8 "${PUBLIC_CLOUD_URL}/v1/billing/checkout" 2>&1 | awk 'tolower($1)=="content-type:"{print $2}' | head -1 | tr -d '\r')
        body_head=$(head -c 200 /tmp/scB.txt | tr -d '\n' | tr -d '\r')
        if [ "$code" = "401" ] || [ "$code" = "405" ] ; then
            # 401 = auth guard fires; 405 = wrong method for GET-only allow. Both prove route registered.
            record_pass B "HTTP $code (route wired)"
        elif [ "$code" = "404" ] ; then
            record_fail B "HTTP 404 — #154 core regression: checkout endpoint not registered"
        else
            record_fail B "HTTP $code ct=$ct body=${body_head:0:80}"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — loopzero-cloud logs contain "billing enabled" (boot-order fix).
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — loopzero-cloud pod logs report 'billing enabled' post-seed"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif ! command -v kubectl >/dev/null 2>&1 ; then
        record_fail C "kubectl not on PATH"
    else
        # Cleanroom 2026-07-04 fix: loopzero-cloud emits "billing enabled" ONCE
        # at boot; if the pod's been up long enough the emission line has
        # rotated out of a --tail=200 window. Grab BOTH pods (2 replicas),
        # widen to --tail=5000, AND include --since to catch startup logs.
        # Dump to file — variable capture corrupts on JSON single-quotes.
        tc_log_file=$(mktemp -t scC.log.XXXXXX)
        kubectl --context "$MGMT_CONTEXT" -n loopzero-system logs deploy/loopzero-cloud --all-containers --tail=5000 --since=24h > "$tc_log_file" 2>/dev/null || true
        if grep -q '"msg":"billing enabled"' "$tc_log_file" ; then
            record_pass C "log line \"billing enabled\" present"
        elif grep -q "STRIPE_API_KEY not set" "$tc_log_file" ; then
            record_fail C "log line 'STRIPE_API_KEY not set — billing endpoints disabled' — pod booted before seed (#154 pre-fix)"
        else
            record_fail C "neither 'billing enabled' nor 'STRIPE_API_KEY not set' in recent logs — unexpected state"
        fi
        rm -f "$tc_log_file"
    fi
fi

RESULTS_JSON+="]}"

log "============================================================"
log "Result: $PASS_COUNT PASS, $FAIL_COUNT FAIL"
[ -n "$FAILED_LIST" ] && log "Failed:${FAILED_LIST}"
log "============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"results":%s}\n' "$PASS_COUNT" "$FAIL_COUNT" "$RESULTS_JSON"
fi

exit $([ $FAIL_COUNT -eq 0 ] && echo 0 || echo 1)
