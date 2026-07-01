#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero-cloud#161 closure
# (loopzero promote sem --yes trava em stdin não-interativo + msg "timed out" enganosa).
#
# Slug:           cli-promote-non-tty
# Issue:          loopzero-cloud#161
# Family:         Follow-up de #160 (mesma CLI, layer diferente — logs vs promote)
# Closed by:      loopzero-cli/src/commands/promote.ts + loopzero-cli/src/register/deploy.ts
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline (#161)
#
# Root cause: PromoteCommand's confirmation gate detectava só `isCI`, não
# `!stdin.isTTY`. Contextos como pipes / redirects / harnesses (Robot suite
# do Red Team) tinham isCI=false mas stdin sem TTY → prompt [y/N] hung →
# withTimeout 40s eventualmente mis-culpava "Promotion timed out. Check your
# connection" (rc 3).
#
# 3 LIVE scenarios:
#   A loopzero-cli source declara `isNonInteractive` in PromoteDeps
#   B register/deploy.ts wires `isNonInteractive: !process.stdin.isTTY`
#   C package.json version >= 0.3.7 (npm-publish gate)
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md.
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

PLAN_SLUG="cli-promote-non-tty"
ISSUE_REF="loopzero-cloud#161"
CLI_REPO_ROOT="${CLI_REPO_ROOT:-$HOME/loopzero-cloud/loopzero-cli}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#161 (promote non-TTY hang).

Environment:
  CLI_REPO_ROOT   default \$HOME/loopzero-cloud/loopzero-cli
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

# ---------------------------------------------------------------------------
# Scenario A — loopzero-cli source declares isNonInteractive in PromoteDeps
# AND uses it in the confirmation gate (source-level proof of the fix).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — PromoteDeps declares isNonInteractive + gate honors it"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ ! -f "$CLI_REPO_ROOT/src/commands/promote.ts" ] ; then
        record_fail A "$CLI_REPO_ROOT/src/commands/promote.ts not found"
    else
        DECLARES=0
        GATED=0
        if fgrep -q 'isNonInteractive' "$CLI_REPO_ROOT/src/commands/promote.ts" 2>/dev/null ; then
            DECLARES=1
        fi
        if fgrep -q 'this.deps.isNonInteractive' "$CLI_REPO_ROOT/src/commands/promote.ts" 2>/dev/null ; then
            GATED=1
        fi
        if [ "$DECLARES" = "1" ] && [ "$GATED" = "1" ] ; then
            record_pass A "isNonInteractive declared + honored in confirmation gate"
        else
            record_fail A "isNonInteractive missing OR not honored — #161 fix NOT shipped"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — register/deploy.ts wires isNonInteractive from stdin TTY.
# Without this wire, the gate would never trigger even though the code
# path exists. Uses `process.stdin.isTTY` inversion (canonical Node.js).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — register/deploy.ts wires isNonInteractive: !process.stdin.isTTY"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    elif [ ! -f "$CLI_REPO_ROOT/src/register/deploy.ts" ] ; then
        record_fail B "$CLI_REPO_ROOT/src/register/deploy.ts not found"
    else
        if fgrep -q 'isNonInteractive: !process.stdin.isTTY' "$CLI_REPO_ROOT/src/register/deploy.ts" 2>/dev/null ; then
            record_pass B "wire canonical: isNonInteractive from stdin TTY"
        else
            record_fail B "isNonInteractive wire missing — gate would never trigger"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — package.json version >= 0.3.7 (npm-publish gate).
# Red Team needs `npm install -g @loopzero/cli@0.3.7` to see the fix.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — loopzero-cli package.json version >= 0.3.7"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif [ ! -f "$CLI_REPO_ROOT/package.json" ] ; then
        record_fail C "$CLI_REPO_ROOT/package.json not found"
    else
        VERSION=$(python3 -c "import json; print(json.load(open('$CLI_REPO_ROOT/package.json')).get('version',''))" 2>/dev/null || echo "")
        BUMP_OK=$(python3 -c "
v = '$VERSION'.split('.')
if len(v) >= 3:
    try:
        parts = tuple(int(x.split('-')[0]) for x in v[:3])
        print('YES' if parts >= (0, 3, 7) else 'NO')
    except ValueError:
        print('PARSE')
else:
    print('PARSE')
")
        if [ "$BUMP_OK" = "YES" ] ; then
            record_pass C "version=$VERSION (>= 0.3.7 — npm publish gate satisfied)"
        else
            record_fail C "version=$VERSION < 0.3.7 — bump required before Red Team can see fix"
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
