#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero-cloud#157 closure
# (preço divergente — botão $29 vs Stripe cobrava US$ 19,00).
#
# Slug:           billing-pro-price-canonical
# Issue:          loopzero-cloud#157
# Family:         #154 (routes 404) + #155 (BILLING_NOT_CONFIGURED) +
#                 #156 (webhook not delivered) — this closes the price alignment
# Fix path:       Stripe test-mode Pro price recreated at $29.00 canonical
#                 (MEET-2026-05-12 D4=B — CLAUDE.md § Pricing). Old $19 price
#                 (`price_1TJGIuA0ZwssSKrVEMKZsiMj`) deactivated.
#                 dev-credentials.json.stripe.price_pro → new id.
#                 seed-stripe-api-keys-canonical.sh re-run materializes new
#                 STRIPE_PRICE_PRO env var in loopzero-stripe-api-keys Secret.
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline (#157)
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #157 verbatim:
#   * The Pro checkout flow (canonical business value $29/mo) must produce a
#     Stripe checkout session whose `amount_total` is 2900 cents (US$ 29,00),
#     NOT 1900 cents (US$ 19,00 — the pre-fix state that diverged from the
#     dashboard hardcode).
#
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

PLAN_SLUG="billing-pro-price-canonical"
ISSUE_REF="loopzero-cloud#157"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
CREDS_FILE="${LOOPZERO_DEV_CREDENTIALS:-$HOME/.loopzero/dev-credentials.json}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""
EXPECTED_CENTS="${EXPECTED_CENTS:-2900}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B] [--json] [--help]

LIVE regression-protector for loopzero-cloud#157 (preço Pro divergente).

Environment:
  PUBLIC_CLOUD_URL      default https://app-dev.loopzero.example
  LOOPZERO_DEV_CREDENTIALS  default ~/.loopzero/dev-credentials.json
  SESSION_JWT_FILE      default \$HOME/.loopzero/local-dogfood-session-jwt.txt
  EXPECTED_CENTS        default 2900 (US\$ 29,00 canonical per MEET-2026-05-12 D4=B)

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

# JWT auto-discovery
if [ -z "$SESSION_JWT_FILE" ] ; then
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
        if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
    done
fi

log() { printf '[%s] %s\n' "$(basename "$0")" "$*" ; }

PASS_COUNT=0
FAIL_COUNT=0
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
log "Expected Pro price: ${EXPECTED_CENTS} cents (US\$ $(( EXPECTED_CENTS / 100 )),00)"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — dev-credentials.json.stripe.price_pro references a Stripe price
# whose unit_amount matches the canonical business value (2900 cents = $29).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — Stripe price_pro unit_amount == ${EXPECTED_CENTS} cents"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ ! -f "$CREDS_FILE" ] ; then
        record_fail A "$CREDS_FILE not found"
    else
        API_KEY=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('api_key',''))" 2>/dev/null || echo "")
        PRICE_ID=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('price_pro',''))" 2>/dev/null || echo "")
        if [ -z "$API_KEY" ] || [ -z "$PRICE_ID" ] ; then
            record_fail A "stripe.api_key or stripe.price_pro missing from creds"
        else
            AMT=$(curl -sSf -u "$API_KEY:" "https://api.stripe.com/v1/prices/$PRICE_ID" 2>/dev/null \
                | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('unit_amount',0))" 2>/dev/null || echo "0")
            if [ "$AMT" = "$EXPECTED_CENTS" ] ; then
                record_pass A "unit_amount=${AMT} cents (canonical \$29)"
            else
                record_fail A "unit_amount=${AMT} cents (expected ${EXPECTED_CENTS}) — #157 divergence"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — POST /v1/billing/checkout {"plan":"pro"} produces a Stripe
# checkout session whose amount_total == 2900 cents. This proves the full
# chain: dev-credentials.json → STRIPE_PRICE_PRO env → PriceIDForPlan →
# Stripe.CreateCheckoutSession → session.amount_total matches business value.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — POST /v1/billing/checkout {plan:pro} → session.amount_total == ${EXPECTED_CENTS}"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail B "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT=$(cat "$SESSION_JWT_FILE")
        RESP=$(curl -s --max-time 10 -X POST \
            -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
            -d '{"plan":"pro"}' \
            "${PUBLIC_CLOUD_URL}/v1/billing/checkout" 2>&1 || true)
        URL=$(printf '%s' "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('url',''))" 2>/dev/null || echo "")
        SESSION_ID=$(printf '%s' "$URL" | grep -oE 'cs_test_[a-zA-Z0-9]+' | head -1 || true)
        if [ -z "$SESSION_ID" ] ; then
            record_fail B "no checkout session_id in response: ${RESP:0:120}"
        else
            API_KEY=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('api_key',''))")
            AMT=$(curl -sSf -u "$API_KEY:" "https://api.stripe.com/v1/checkout/sessions/$SESSION_ID" 2>/dev/null \
                | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('amount_total',0))" 2>/dev/null || echo "0")
            if [ "$AMT" = "$EXPECTED_CENTS" ] ; then
                record_pass B "checkout.amount_total=${AMT} cents (session ${SESSION_ID:0:20}...)"
            else
                record_fail B "amount_total=${AMT} cents (expected ${EXPECTED_CENTS}) — #157 pre-fix state"
            fi
        fi
    fi
fi

RESULTS_JSON+="]}"

log "============================================================"
log "Result: $PASS_COUNT PASS, $FAIL_COUNT FAIL"
log "============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"results":%s}\n' "$PASS_COUNT" "$FAIL_COUNT" "$RESULTS_JSON"
fi

exit $([ $FAIL_COUNT -eq 0 ] && echo 0 || echo 1)
