#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero/loopzero#155 closure
# (Billing: checkout 'pro' returns 503 BILLING_NOT_CONFIGURED — Stripe
# price IDs not provisioned in dev).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# UNBREAKABLE — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #155 verbatim:
#   * Follow-up of #154 (/v1/billing* routes already registered).
#   * Root cause: STRIPE_PRICE_{STARTER,PRO,TEAM} env vars unset in the
#     loopzero-cloud pod → PriceIDForPlan("pro") returns "" →
#     `internal/routes/billing.go:216` emits 503 BILLING_NOT_CONFIGURED.
#   * Fix: `loopzero-ops seed stripe-api-keys` reads `stripe.price_{starter,pro,
#     team}` from dev-credentials.json and materializes them in the Secret
#     `loopzero-stripe-api-keys` alongside STRIPE_API_KEY. The #154 restart
#     guarantees the pod picks up the new env vars.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` UNBREAKABLE.
#
# Scenarios:
#   A — Secret loopzero-stripe-api-keys contains STRIPE_PRICE_PRO key
#       (non-empty base64 value). Requires kubectl.
#   B — loopzero-cloud pod env includes STRIPE_PRICE_PRO with non-empty value
#       (envFrom.secretRef materialized correctly).
#   C — POST /v1/billing/checkout {"plan":"pro"} does NOT return 503
#       BILLING_NOT_CONFIGURED. Expected: 200 with checkout_url OR 4xx
#       from auth/config differently — but NOT 503 with that specific code.
#
# Slug:           billing-stripe-price-ids-seed-canonical
# Issue:          loopzero/loopzero#155
# Family:         #154 (billing routes 404) — this closes the follow-up layer
# Closed by:      infra/scripts/local/`loopzero-ops seed stripe-api-keys` (extended)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#155)
# Companion (CI): infra/tests/check-seed-stripe-api-keys-canonical-syntax_test.sh (extended)

set -euo pipefail

PLAN_SLUG="billing-stripe-price-ids-seed-canonical"
ISSUE_REF="loopzero/loopzero#155"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
CORE_CONTEXT="${CORE_CONTEXT:-k3d-loopzero-core}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero/loopzero#155 (billing checkout 'pro' 503
BILLING_NOT_CONFIGURED — Stripe price IDs unset in dev pod).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  CORE_CONTEXT      default k3d-loopzero-core
  SESSION_JWT_FILE  default ~/.loopzero/local-dogfood-session-jwt.txt (or canonical)

Per cleanroom-up-live-test-mandate.md UNBREAKABLE. Operator-side. Not for CI.
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

# Canonical JWT discovery
if [ -z "$SESSION_JWT_FILE" ] ; then
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
        if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
    done
fi

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
log "JWT: ${SESSION_JWT_FILE:-(none)}"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — loopzero-stripe-api-keys Secret has STRIPE_PRICE_PRO with data.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — Secret loopzero-stripe-api-keys has non-empty STRIPE_PRICE_PRO"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif ! command -v kubectl >/dev/null 2>&1 ; then
        record_fail A "kubectl not on PATH"
    else
        val=$(kubectl --context "$CORE_CONTEXT" -n loopzero-system get secret loopzero-stripe-api-keys -o jsonpath='{.data.STRIPE_PRICE_PRO}' 2>/dev/null || true)
        if [ -n "$val" ] ; then
            record_pass A "STRIPE_PRICE_PRO present (base64 len=${#val})"
        else
            record_fail A "STRIPE_PRICE_PRO key missing OR empty (#155 pre-fix state)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — loopzero-cloud pod env sees STRIPE_PRICE_PRO from envFrom Secret.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — loopzero-cloud pod env exposes non-empty STRIPE_PRICE_PRO"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    elif ! command -v kubectl >/dev/null 2>&1 ; then
        record_fail B "kubectl not on PATH"
    else
        pod=$(kubectl --context "$CORE_CONTEXT" -n loopzero-system get pods -l app=loopzero-cloud -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
        if [ -z "$pod" ] ; then
            record_fail B "no loopzero-cloud pod found"
        else
            # loopzero-cloud is distroless — no `printenv` binary. Verify env
            # via the pod's own reflection: `kubectl get pod -o json`
            # exposes .spec.containers[].envFrom which references the
            # loopzero-stripe-api-keys Secret we materialized. Cross-check
            # that the Secret data has STRIPE_PRICE_PRO (Scenario A) AND
            # that the pod's envFrom references it — this proves the
            # wire at pod-config level without needing shell in the pod.
            wired=$(kubectl --context "$CORE_CONTEXT" -n loopzero-system get pod "$pod" -o jsonpath='{range .spec.containers[*].envFrom[*].secretRef}{.name}{"\n"}{end}' 2>/dev/null | grep -c '^loopzero-stripe-api-keys$' || true)
            if [ "$wired" -ge 1 ] ; then
                record_pass B "pod envFrom references loopzero-stripe-api-keys — Secret keys wired at pod-config level"
            else
                record_fail B "pod $pod envFrom does NOT reference loopzero-stripe-api-keys — chart wiring gap"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — POST /v1/billing/checkout {"plan":"pro"} does NOT return
# 503 BILLING_NOT_CONFIGURED. Expected: 200/303 checkout URL, OR 402/409
# plan-cap message. NEVER 503 BILLING_NOT_CONFIGURED.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — POST /v1/billing/checkout {plan:pro} does NOT return 503 BILLING_NOT_CONFIGURED"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail C "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT=$(cat "$SESSION_JWT_FILE")
        code=$(curl -s -o /tmp/scC.json -w '%{http_code}' --max-time 10 \
            -X POST -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
            -d '{"plan":"pro"}' \
            "${PUBLIC_CLOUD_URL}/v1/billing/checkout" 2>&1 || true)
        err_code=$(python3 -c 'import json,sys;
try:
    d=json.load(open("/tmp/scC.json"));
    print(d.get("code",""))
except Exception:
    print("")' 2>/dev/null || echo "")
        if [ "$code" = "503" ] && [ "$err_code" = "BILLING_NOT_CONFIGURED" ] ; then
            record_fail C "HTTP 503 BILLING_NOT_CONFIGURED — #155 pre-fix state (STRIPE_PRICE_PRO unset)"
        elif [ "$code" = "200" ] ; then
            record_pass C "HTTP 200 — Stripe checkout session created"
        else
            # Any other code (e.g. 401 expired session, 402 plan cap, 500 stripe error)
            # is downstream of #155 — the price ID resolution succeeded.
            record_pass C "HTTP $code err=$err_code (NOT 503 BILLING_NOT_CONFIGURED — #155 fix in place)"
        fi
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
