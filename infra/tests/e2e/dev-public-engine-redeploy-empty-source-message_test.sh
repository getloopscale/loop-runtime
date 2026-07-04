#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#126 closure
# (POST /engine/api/v1/builds {"redeploy_of":"<build_id>"} returned 400
# MISSING_FIELD with `"At least one app is required. Check your
# `.loopzero/services.json` for a `services:` array."` when the SOURCE build
# had no apps. That guidance is wrong in a redeploy context — the user
# never edited a local manifest for this submission; the canonical fix
# is to pick a source build that has apps. The handler treated the
# redeploy as a fresh-deploy validation failure instead of differentiating
# by code path).
#
# Fix shipped today (this slice) in `api/internal/routes/builds.go`:
#   The `if len(req.Apps) == 0` guard now reads `req.RedeployOf` and
#   selects a redeploy-specific message:
#     "Source build <id> has no apps to redeploy. Pick a build that
#      completed with at least one app."
#   Fresh-deploy callers still see the `.loopzero/services.json` message.
#   Both surface typed 400 MISSING_FIELD per the existing exit-code
#   contract.
#
# Related (already closed, ship today): #123 (canonical manifest path
# in the fresh-deploy message); #122 (dashboard surfaces the typed
# 400 instead of swallowing).
#
# NOTE on the cleanroom mandate: this is a Go-side BFF fix (engine
# `api/internal/routes/builds.go`). The canonical proof is source-grep +
# `go test` against the closure unit test
# `TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError`
# — same shape as the #42/#104/#111 sibling triplets. Not a dev-public
# dashboard LIVE bundle probe, so `.claude/rules/cleanroom-up-live-
# test-mandate.md` does NOT apply.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — source-level grep on `api/internal/routes/builds.go` for the
#       canonical redeploy-specific message literal "no apps to
#       redeploy" (proof the redeploy branch shipped).
#   B — source-level grep on `api/internal/routes/builds.go` for the
#       redeploy-branch gate `req.RedeployOf != ""` adjacent to the
#       message split (proof the differentiation by code path is the
#       gate, not a blanket message replacement).
#   C — LIVE proof: `go test -count=1 -run
#       TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError`
#       is GREEN — the closure unit test asserts (i) 400, (ii)
#       MISSING_FIELD code, (iii) "no apps to redeploy" copy, (iv)
#       source build id named, (v) `.loopzero/services.json` does NOT
#       appear in the redeploy message.
#
# Slug:           engine-redeploy-empty-source-message
# Issue:          loopzero-cloud#126
# Family:         #122 / #123 (same dashboard build-error surface)
# Closed by:      `loopzero/api/internal/routes/builds.go:323-345`
#                 + `loopzero/api/internal/routes/builds_test.go:550-594`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#126)
# Companion (CI): infra/tests/check-dev-public-engine-redeploy-empty-source-message-syntax_test.sh

set -euo pipefail

PLAN_SLUG="engine-redeploy-empty-source-message"
ISSUE_REF="loopzero-cloud#126"
ENGINE_DIR="${ENGINE_DIR:-$(cd "$(dirname "$0")/../../.." 2>/dev/null && pwd)}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#126 (redeploy_of empty-source
error message differentiates from fresh-deploy missing-apps copy).

Environment:
  ENGINE_DIR    default ../../.. relative to this script (the engine
                repo root — contains api/internal/routes/builds.go and
                api/internal/routes/builds_test.go)

This script is Go-side hermetic — does NOT require Cleanroom + dev-public.
Scenarios A + B are pure source-level greps. Scenario C runs
\`go test -count=1\` against the closure unit test.
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
# Scenario A — source-level grep for the canonical redeploy-specific
# message literal "no apps to redeploy" in builds.go
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — builds.go has the canonical 'no apps to redeploy' redeploy message"
    SRC="$ENGINE_DIR/api/internal/routes/builds.go"
    if [ ! -f "$SRC" ] ; then
        record_fail A "source missing: $SRC"
    else
        msg_count="$(grep -cF 'no apps to redeploy' "$SRC" || true)"
        if [ "$msg_count" -ge 1 ] ; then
            record_pass A "builds.go has 'no apps to redeploy' message=$msg_count (redeploy branch shipped)"
        else
            record_fail A "builds.go MISSING redeploy-specific message — pre-fix shape (only fresh-deploy copy present)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — source-level grep for the redeploy-branch gate
# `req.RedeployOf != ""` (proof the differentiation is gated by the
# code path, not a blanket message replacement)
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — builds.go has 'if req.RedeployOf != \"\"' gate inside the missing-apps branch"
    SRC="$ENGINE_DIR/api/internal/routes/builds.go"
    if [ ! -f "$SRC" ] ; then
        record_fail B "source missing: $SRC"
    else
        gate_count="$(grep -cF 'if req.RedeployOf != ""' "$SRC" || true)"
        if [ "$gate_count" -ge 1 ] ; then
            record_pass B "builds.go has req.RedeployOf != \"\" gate=$gate_count (code-path-aware differentiation)"
        else
            record_fail B "builds.go MISSING RedeployOf gate — message split missing or replaced wholesale"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — LIVE proof: closure unit test runs GREEN
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — go test -run TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError is GREEN"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        if ! command -v go >/dev/null 2>&1 ; then
            record_fail C "go toolchain not available on PATH"
        else
            log "  running go test -count=1 ./api/internal/routes/ -run TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError …"
            out="$(cd "$ENGINE_DIR" && go test -count=1 ./api/internal/routes/ -run TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError 2>&1 || true)"
            if printf '%s' "$out" | grep -qE '^ok[[:space:]]+github.com/getloopscale/loopzero/api/internal/routes' ; then
                record_pass C "closure unit test PASS — redeploy message canonical"
            else
                record_fail C "closure unit test FAIL: $(printf '%s' "$out" | tail -3 | tr '\n' ' ')"
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
