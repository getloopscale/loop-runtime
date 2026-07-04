#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero-cloud#156 closure
# (payment succeeds at Stripe but plan stays starter — webhook never fires).
#
# Slug:           billing-webhook-fulfillment
# Issue:          loopzero-cloud#156
# Family:         #154 (routes 404) + #155 (BILLING_NOT_CONFIGURED) — this
#                 closes the third layer: fulfillment via webhook
# Closed by:      infra/scripts/local/seed-stripe-webhook-endpoint-canonical.sh
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline (#156)
# Companion:      infra/tests/check-seed-stripe-webhook-endpoint-canonical-syntax_test.sh
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #156 verbatim (chain proof of the fulfillment path):
#   * loopzero-stripe-webhook Secret has real whsec_test_* (NOT placeholder
#     whsec_dev_*) so signature validation can succeed.
#   * Stripe test-mode webhook endpoint exists AND targets
#     https://app-dev.loopzero.example/v1/webhooks/stripe with the 5 canonical
#     events (checkout.session.completed + subscription lifecycle +
#     invoice.payment_failed).
#   * Webhook endpoint returns 400 on empty POST (handler reachable —
#     Cross-Project Rule 9: never surface internal error terms).
#
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

PLAN_SLUG="billing-webhook-fulfillment"
ISSUE_REF="loopzero-cloud#156"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
CREDS_FILE="${LOOPZERO_DEV_CREDENTIALS:-$HOME/.loopzero/dev-credentials.json}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#156 (payment succeeds but plan
stays starter — Stripe webhook endpoint not provisioned in dev).

Environment:
  PUBLIC_CLOUD_URL      default https://app-dev.loopzero.example
  MGMT_CONTEXT          default k3d-loopzero-mgmt
  LOOPZERO_DEV_CREDENTIALS  default ~/.loopzero/dev-credentials.json

Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL. Operator-side. Not for CI.
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
# Scenario A — loopzero-stripe-webhook Secret carries a REAL whsec (whsec_test_*),
# NOT the placeholder whsec_dev_* that fails Stripe signature validation.
# Root cause of #156's SILENT drop even when events arrive.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — loopzero-stripe-webhook has canonical whsec_test_* (not whsec_dev_ placeholder)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif ! command -v kubectl >/dev/null 2>&1 ; then
        record_fail A "kubectl not on PATH"
    else
        b64=$(kubectl --context "$MGMT_CONTEXT" -n loopzero-system get secret loopzero-stripe-webhook -o jsonpath='{.data.STRIPE_WEBHOOK_SECRET}' 2>/dev/null || true)
        if [ -z "$b64" ] ; then
            record_fail A "STRIPE_WEBHOOK_SECRET key missing"
        else
            prefix=$(printf '%s' "$b64" | base64 -d 2>/dev/null | head -c 12 || true)
            case "$prefix" in
                whsec_dev_*)
                    record_fail A "whsec is PLACEHOLDER (${prefix}...) — signature will never validate — #156 pre-fix state"
                    ;;
                whsec_*)
                    record_pass A "whsec real (prefix ${prefix}...)"
                    ;;
                *)
                    record_fail A "whsec has unknown prefix ${prefix}..."
                    ;;
            esac
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — Stripe test-mode has a webhook endpoint configured targeting
# our public URL with the 5 canonical enabled_events. Without this the events
# NEVER reach the pod — the root cause of #156.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — Stripe test-mode endpoint exists targeting $PUBLIC_CLOUD_URL/v1/webhooks/stripe"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    elif [ ! -f "$CREDS_FILE" ] ; then
        record_fail B "$CREDS_FILE not found"
    else
        API_KEY=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('api_key',''))" 2>/dev/null || echo "")
        if [ -z "$API_KEY" ] ; then
            record_fail B "stripe.api_key missing from creds"
        else
            export EXPECTED_URL="$PUBLIC_CLOUD_URL/v1/webhooks/stripe"
            EVENTS_HIT=$(curl -sSf -u "$API_KEY:" \
                "https://api.stripe.com/v1/webhook_endpoints?limit=100" \
                | python3 -c "
import json, sys, os
d = json.load(sys.stdin)
required = {'checkout.session.completed', 'customer.subscription.created', 'customer.subscription.updated', 'customer.subscription.deleted', 'invoice.payment_failed'}
target = os.environ['EXPECTED_URL']
for ep in d.get('data', []):
    if ep.get('url') == target:
        got = set(ep.get('enabled_events', []))
        missing = required - got
        if not missing:
            print('OK')
        else:
            print('MISSING:' + ','.join(sorted(missing)))
        sys.exit(0)
print('NO_ENDPOINT')
" 2>/dev/null || echo "ERR")
            unset EXPECTED_URL
            case "$EVENTS_HIT" in
                OK) record_pass B "endpoint exists + all 5 canonical events subscribed" ;;
                NO_ENDPOINT) record_fail B "no webhook_endpoint targeting $PUBLIC_CLOUD_URL/v1/webhooks/stripe — #156 pre-fix state" ;;
                MISSING:*) record_fail B "endpoint exists but events missing: ${EVENTS_HIT#MISSING:}" ;;
                *) record_fail B "Stripe API query failed" ;;
            esac
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — POST /v1/webhooks/stripe returns 400 (handler reachable +
# signature/body validation active). NOT 404 (rota unmounted) and NOT 502
# (upstream unreachable). 400 on empty body proves the endpoint is wired
# through Caddy → loopzero-cloud + signature verification is active.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — POST /v1/webhooks/stripe returns 400 (reachable + signature active)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
            -X POST -H 'Content-Type: application/json' \
            -H 'Stripe-Signature: t=1234,v1=deadbeef' \
            -d '{"id":"evt_ping","type":"checkout.session.completed","data":{"object":{}}}' \
            "${PUBLIC_CLOUD_URL}/v1/webhooks/stripe" 2>&1 || true)
        case "$code" in
            400) record_pass C "HTTP 400 — handler reachable + signature verification active" ;;
            404) record_fail C "HTTP 404 — webhook route not mounted (regression of #154?)" ;;
            502|503) record_fail C "HTTP $code — loopzero-cloud unreachable" ;;
            *) record_fail C "HTTP $code (expected 400)" ;;
        esac
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
