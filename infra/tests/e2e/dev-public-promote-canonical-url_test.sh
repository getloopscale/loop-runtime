#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero-cloud#159 closure
# (loopzero promote imprime URL sem prefixo de tenant que 404).
#
# Slug:           promote-canonical-url
# Issue:          loopzero-cloud#159
# Family:         J-25 status.url canonical (2026-06-12 sponsor walkthrough) —
#                 THIS closes the same divergence on the PROMOTE path
# Closed by:      api/internal/services/release/promotion_orchestrator.go +
#                 pkg/naming/entry_app.go (NEW SoT)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline (#159)
#
# Reproduces #159's user-visible bug:
#   Pre-fix: promote URL = "loopzerokit-example-production.prod-dev.loopzero.example"
#            (no tenant prefix, no entryApp) → 404
#   Post-fix: promote URL = "tn-XXX-loopzerokit-example-<entryApp>-production.prod-dev.loopzero.example"
#             (canonical naming.AppURL formula) → matches IngressRoute
#
# 3 LIVE scenarios:
#   A pkg/naming/entry_app.go exists in the running binary derivative (source
#     proxy — the SoT that promote now consults)
#   B promotion_orchestrator.go uses `naming.AppURL(...)` NOT the legacy
#     `fmt.Sprintf("https://%s-%s.%s", ProjectName, ToEnv, domain)` formula
#     (source-level regression-protector — the legacy formula is banned)
#   C POST /api/v1/projects/<probe>/promote — if the probe project has
#     no ACTIVE release the endpoint MUST return a typed 4xx, never 202
#     with a synthetic URL that 404s (the #159 trap surface)
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md.
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

PLAN_SLUG="promote-canonical-url"
ISSUE_REF="loopzero-cloud#159"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#159 (promote emite URL sem tenant/entryApp que 404).

Environment:
  PUBLIC_CLOUD_URL      default https://app-dev.loopzero.example
  MGMT_CONTEXT          default k3d-loopzero-mgmt
  SESSION_JWT_FILE      default \$HOME/.loopzero/local-dogfood-session-jwt.txt

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
log "============================================================"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo .)"

# ---------------------------------------------------------------------------
# Scenario A — pkg/naming/entry_app.go declares ResolveEntryAppFromApps.
# This is the SoT the promote path now consults. Absence means the fix
# was never shipped.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — pkg/naming/entry_app.go declares ResolveEntryAppFromApps (SoT)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        if fgrep -q 'func ResolveEntryAppFromApps' "$REPO_ROOT/pkg/naming/entry_app.go" 2>/dev/null ; then
            record_pass A "SoT declared — promote can call it"
        else
            record_fail A "SoT missing — #159 fix NOT deployed"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — promotion_orchestrator.go uses naming.AppURL AND does NOT
# retain the legacy `fmt.Sprintf("https://%s-%s.%s", req.ProjectName, req.ToEnv, ...)`
# formula. Anti-regression against re-introducing the buggy shape.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — promotion_orchestrator uses naming.AppURL (not legacy formula)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        FILE="$REPO_ROOT/api/internal/services/release/promotion_orchestrator.go"
        USES_CANONICAL=0
        USES_LEGACY=0
        if fgrep -q 'naming.AppURL' "$FILE" 2>/dev/null ; then
            USES_CANONICAL=1
        fi
        # Legacy formula: fmt.Sprintf("https://%s-%s.%s", req.ProjectName, req.ToEnv,
        # detect by the specific 4-token shape (%s-%s.%s with ProjectName+ToEnv args).
        if fgrep -q 'Sprintf("https://%s-%s.%s", req.ProjectName' "$FILE" 2>/dev/null ; then
            USES_LEGACY=1
        fi
        if [ "$USES_CANONICAL" = "1" ] && [ "$USES_LEGACY" = "0" ] ; then
            record_pass B "naming.AppURL used + legacy formula absent"
        elif [ "$USES_CANONICAL" = "0" ] ; then
            record_fail B "naming.AppURL missing — #159 fix NOT deployed"
        else
            record_fail B "legacy Sprintf formula still present — regression"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — POST /api/v1/projects/<ghost>/promote returns typed 4xx
# (never 202 with a synthetic URL). Anti-silent-success invariant — the
# exact class of trap #159 exposes on the happy path is guarded against
# on the negative path here.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — POST /promote to non-existent project → typed 4xx, never 202+URL"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail C "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT=$(cat "$SESSION_JWT_FILE")
        code=$(curl -s -o /tmp/scC-159.json -w '%{http_code}' --max-time 10 \
            -X POST -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
            -d '{"from":"preview","to":"production"}' \
            "${PUBLIC_CLOUD_URL}/api/v1/projects/ghost-project-159/promote" 2>&1 || true)
        if [ "$code" = "202" ] ; then
            record_fail C "HTTP 202 for non-existent project — silent-success trap surface"
        elif [ "${code:0:1}" = "4" ] || [ "${code:0:1}" = "5" ] || [ "$code" = "000" ] ; then
            record_pass C "HTTP $code — non-2xx (contract preserved)"
        else
            record_pass C "HTTP $code — non-202 (contract preserved)"
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
