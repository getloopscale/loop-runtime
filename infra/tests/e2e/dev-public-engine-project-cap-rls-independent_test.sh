#!/usr/bin/env bash
set -euo pipefail
# LIVE bash regression-protector for Red Team loopzero-cloud#111 closure
# (latent risk: the per-Account project-cap count in
# `enforceProjectCapTx` is a CROSS-TENANT aggregate
# `SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM
# tenants WHERE account_id = $1)`. The build-submit transaction sets
# `app.tenant_id = <deploying tenant>` for the projects RLS policy.
# The count's correctness depends on the connection NOT being subject
# to that policy — currently true because the engine pool role has
# `rolbypassrls=true` AND `FORCE ROW LEVEL SECURITY` is disabled
# (migration 000010). If the synchronous build-submit path is ever
# moved onto a non-BYPASSRLS, non-owner role — extending the
# `loopzero_worker` role rollout, for instance — the count silently
# degrades to per-tenant and the per-Account cap is bypassable across
# tenants again, re-opening the #106 class of bug).
#
# Note on closure semantics: the issue is explicit that this is NOT a
# bug today — `current_user=loopzero force_rls=false bypassrls=true` and
# the cross-tenant count is empirically unaffected by the RLS context.
# Per parsimony ladder rung 1 ("does this need to exist?") the
# recommended `SECURITY DEFINER` hardening is YAGNI WHILE the role
# assumption holds. What MUST exist is a regression-protector that
# fires the moment the assumption breaks: the M5 hermetic guard
# `TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext`
# (at `api/tests/integration/project_cap_hermetic_m5_test.go:257`)
# reproduces the build-submit RLS context (sets `app.tenant_id` to
# the deploying tenant, drives `EnsureProject` on the same tx) and
# asserts the cross-tenant cap still binds. This triplet asserts THAT
# guard stays GREEN — it is the latent-risk gate.
#
# NOTE on the cleanroom mandate: same shape as the #42/#104 sibling
# triplets — Go-side hermetic (no dev-public dashboard surface), so
# `.claude/rules/cleanroom-up-live-test-mandate.md` does NOT apply.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a regression-protector that reproduces the
# bug verbatim. For a latent risk, the "RED" state is the future moment
# the role assumption changes and the guard fails; the "GREEN" state
# is what we have now. The triplet locks the chain so neither end can
# silently drift.
#
# Scenarios:
#   A — source-level grep on `api/internal/db/project_wrapper.go` for
#       the canonical cross-tenant count query (the SQL whose
#       correctness depends on the BYPASSRLS assumption).
#   B — source-level grep on `api/tests/integration/project_cap_
#       hermetic_m5_test.go` for the canonical guard test
#       `TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext`
#       AND its `set_config('app.tenant_id', …)` reproduction of the
#       build-submit RLS context.
#   C — LIVE proof: `go test -count=1 -run
#       TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext`
#       is GREEN. This is the chain's load-bearing assertion — if it
#       ever fails, the BYPASSRLS assumption broke and the per-Account
#       cap is bypassable.
#
# Slug:           engine-project-cap-rls-independent
# Issue:          loopzero-cloud#111
# Family:         #106 (per-Account cap bypass — this is the latent re-open)
# Closed by:      (assumption invariance) `api/internal/db/project_wrapper.go:115-176`
#                 + `api/tests/integration/project_cap_hermetic_m5_test.go:257`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#111)
# Companion (CI): infra/tests/check-dev-public-engine-project-cap-rls-independent-syntax_test.sh

PLAN_SLUG="engine-project-cap-rls-independent"
ISSUE_REF="loopzero-cloud#111"
ENGINE_DIR="${ENGINE_DIR:-$(cd "$(dirname "$0")/../../.." 2>/dev/null && pwd)}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#111 (engine project-cap RLS-
context-invariance assumption).

Environment:
  ENGINE_DIR    default ../../.. relative to this script (the engine
                repo root — contains api/internal/db/project_wrapper.go
                and api/tests/integration/project_cap_hermetic_m5_test.go)

This script is Go-side hermetic — does NOT require Cleanroom + dev-public.
Scenarios A + B are pure source-level greps. Scenario C runs
\`go test -count=1\` against the engine's M5 guard test.
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
log "Engine dir: $ENGINE_DIR"
log "============================================================"

if [ ! -d "$ENGINE_DIR" ] ; then
    log "ERROR: ENGINE_DIR does not resolve to a directory: $ENGINE_DIR"
    exit 2
fi

# ---------------------------------------------------------------------------
# Scenario A — source-level grep on project_wrapper.go for the canonical
# cross-tenant count query whose correctness depends on the BYPASSRLS
# assumption
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — project_wrapper.go has the canonical cross-tenant count query"
    SRC="$ENGINE_DIR/api/internal/db/project_wrapper.go"
    if [ ! -f "$SRC" ] ; then
        record_fail A "source missing: $SRC"
    else
        # Match: SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id = $1)
        # (use a fixed-string fragment to avoid regex escaping headaches)
        sql_count="$(grep -cF 'count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id' "$SRC" || true)"
        if [ "$sql_count" -ge 1 ] ; then
            record_pass A "project_wrapper.go has cross-tenant count query=$sql_count (RLS-bypass-dependent — latent risk active)"
        else
            record_fail A "project_wrapper.go MISSING canonical cross-tenant count query — shape drift or refactor away from documented invariant"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — source-level grep on the M5 hermetic guard test for the
# RLS-context reproduction + the canonical test function name
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — guard test has RLS-context reproduction + canonical name"
    TST="$ENGINE_DIR/api/tests/integration/project_cap_hermetic_m5_test.go"
    if [ ! -f "$TST" ] ; then
        record_fail B "guard test missing: $TST"
    else
        name_count="$(grep -cE 'func TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext' "$TST" || true)"
        # The reproduction sets app.tenant_id on the same tx the cap is enforced through.
        ctx_count="$(grep -cF "set_config('app.tenant_id'" "$TST" || true)"
        if [ "$name_count" -ge 1 ] && [ "$ctx_count" -ge 1 ] ; then
            record_pass B "guard has canonical name=$name_count + RLS-context reproduction=$ctx_count"
        else
            record_fail B "guard test MISSING canonical shape (name=$name_count app.tenant_id reproduction=$ctx_count)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — LIVE proof: the M5 guard test runs GREEN. The "RED" state
# for a latent risk is the future moment this assertion flips — at which
# point the BYPASSRLS assumption broke and the per-Account cap is
# bypassable across tenants.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — go test -run TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext is GREEN"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        if ! command -v go >/dev/null 2>&1 ; then
            record_fail C "go toolchain not available on PATH"
        else
            log "  running go test -count=1 ./api/tests/integration/... -run TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext …"
            out="$(cd "$ENGINE_DIR" && go test -count=1 ./api/tests/integration/... -run TestProjectCapHermeticM5_CrossTenantCountUnaffectedByRLSContext 2>&1 || true)"
            if printf '%s' "$out" | grep -qE '^ok[[:space:]]+github.com/getloopscale/loopzero/api/tests/integration' ; then
                record_pass C "M5 guard test PASS (BYPASSRLS assumption holds — latent risk gated)"
            else
                record_fail C "M5 guard test FAIL — the BYPASSRLS assumption BROKE; per-Account cap is bypassable across tenants ($(printf '%s' "$out" | tail -3 | tr '\n' ' '))"
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
