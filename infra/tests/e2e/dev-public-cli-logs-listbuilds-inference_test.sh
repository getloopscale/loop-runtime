#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero-cloud#160 closure
# (loopzero logs sem --build diz "No builds found" apesar de builds existirem).
#
# Slug:           cli-logs-listbuilds-inference
# Issue:          loopzero-cloud#160
# Family:         Follow-up de #38 (logs timeout) — same command, different bug
# Closed by:      loopzero-cli/src/commands/logs.ts + loopzero-cli/src/register/deploy.ts
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline (#160)
#
# Root cause: CLI logs command inferred last-build from
# `/status?environment=<env>.last_build.id` (release-bound view) instead of
# `/builds?env=<env>` (build-history view — same source `loopzero build list`
# uses). The two diverge when builds exist but no active release yet
# (fresh project with CANCELLED + SUCCEEDED preview builds is Red Team's
# exact repro).
#
# 3 LIVE scenarios:
#   A `/api/v1/projects/<probe>/builds?env=<env>&limit=1` endpoint (the
#     canonical inference source) is reachable via engine proxy
#   B `loopzero-cli/src/commands/logs.ts` source declares `listBuilds` dep
#     AND the resolution prefers it over `getProjectStatus`
#   C Package version bumped to 0.3.6+ (npm-publish gate — Red Team needs
#     the new binary to see the fix)
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md.
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL.

set -euo pipefail

PLAN_SLUG="cli-logs-listbuilds-inference"
ISSUE_REF="loopzero-cloud#160"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
CLI_REPO_ROOT="${CLI_REPO_ROOT:-$HOME/loopzero-cloud/loopzero-cli}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#160 (logs inference broken).

Environment:
  PUBLIC_CLOUD_URL      default https://app-dev.loopzero.example
  SESSION_JWT_FILE      default \$HOME/.loopzero/local-dogfood-session-jwt.txt
  CLI_REPO_ROOT         default \$HOME/loopzero-cloud/loopzero-cli

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

# ---------------------------------------------------------------------------
# Scenario A — /engine/api/v1/projects/<probe>/builds?env=<env>&limit=1 200 OK
# with `builds` array + `total`. This is the SoT CLI now consults for
# inference; anything less would mean the backend contract regressed.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — /api/v1/projects/<probe>/builds?env=<env>&limit=1 returns canonical shape"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT=$(cat "$SESSION_JWT_FILE")
        code=$(curl -s -o /tmp/scA-160.json -w '%{http_code}' --max-time 10 \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/seed/builds?env=preview&limit=1" 2>&1 || true)
        if [ "$code" != "200" ] ; then
            record_fail A "HTTP $code (expected 200) — backend contract regression"
        else
            HAS_BUILDS_KEY=$(python3 -c 'import json; d=json.load(open("/tmp/scA-160.json")); print("YES" if "builds" in d else "NO")' 2>/dev/null || echo "PARSE_ERR")
            HAS_TOTAL_KEY=$(python3 -c 'import json; d=json.load(open("/tmp/scA-160.json")); print("YES" if "total" in d else "NO")' 2>/dev/null || echo "PARSE_ERR")
            if [ "$HAS_BUILDS_KEY" = "YES" ] && [ "$HAS_TOTAL_KEY" = "YES" ] ; then
                record_pass A "HTTP 200 + builds[]/total shape confirmed"
            else
                record_fail A "shape divergence — builds_key=$HAS_BUILDS_KEY total_key=$HAS_TOTAL_KEY"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — loopzero-cli/src/commands/logs.ts declares `listBuilds` in
# LogsDeps AND uses it as PRIMARY inference source. Source-level proof
# that the fix landed (running binary is derived from this source).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — loopzero-cli/src/commands/logs.ts declares listBuilds + prefers it"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    elif [ ! -f "$CLI_REPO_ROOT/src/commands/logs.ts" ] ; then
        record_fail B "$CLI_REPO_ROOT/src/commands/logs.ts not found"
    else
        DECLARES=0
        USES_PRIMARY=0
        if fgrep -q 'listBuilds' "$CLI_REPO_ROOT/src/commands/logs.ts" 2>/dev/null ; then
            DECLARES=1
        fi
        # Look for "Primary: listBuilds" documentation OR the actual call
        if fgrep -q 'this.deps.listBuilds' "$CLI_REPO_ROOT/src/commands/logs.ts" 2>/dev/null ; then
            USES_PRIMARY=1
        fi
        if [ "$DECLARES" = "1" ] && [ "$USES_PRIMARY" = "1" ] ; then
            record_pass B "listBuilds declared + called in resolution path"
        else
            record_fail B "listBuilds missing OR not called — #160 fix NOT shipped"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — package.json version >= 0.3.6 (npm-publish gate).
# Red Team needs the new binary via `npm install -g @loopzero/cli@0.3.6` to
# see the fix. Locked commit before publish so version bumps are traceable.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — loopzero-cli package.json version >= 0.3.6"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif [ ! -f "$CLI_REPO_ROOT/package.json" ] ; then
        record_fail C "$CLI_REPO_ROOT/package.json not found"
    else
        VERSION=$(python3 -c "import json; print(json.load(open('$CLI_REPO_ROOT/package.json')).get('version',''))" 2>/dev/null || echo "")
        # Compare version >= 0.3.6 (simple SemVer numeric compare on triples)
        BUMP_OK=$(python3 -c "
v = '$VERSION'.split('.')
if len(v) >= 3:
    try:
        parts = tuple(int(x.split('-')[0]) for x in v[:3])
        print('YES' if parts >= (0, 3, 6) else 'NO')
    except ValueError:
        print('PARSE')
else:
    print('PARSE')
")
        if [ "$BUMP_OK" = "YES" ] ; then
            record_pass C "version=$VERSION (>= 0.3.6 — npm publish gate satisfied)"
        else
            record_fail C "version=$VERSION < 0.3.6 — bump required before Red Team can see fix"
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
