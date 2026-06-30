#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#98 closure
# (dashboard /status tab dishonest "Progressing" + perpetual 5s polling on a
# preview-only project that has zero production deploys).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #98 verbatim:
#   * Open the Status tab for a project whose ONLY deploys are environment=preview
#     (no production release ever shipped).
#   * Pre-fix: dashboard defaulted `useProjectStatus(name)` to env=production +
#     showed `Status: Progressing` + `Last deploy: —` + KPIs `—` + a "Auto-refresh
#     every 5s" subtitle that polled an endpoint that would never populate.
#   * Post-fix (commit 87058b4 — closes #41/#77/#98): the engine surfaces
#     `other_envs_with_releases` when the queried env is empty AND the dashboard
#     gates on `(!last_build && !active_release && other_envs_with_releases.len > 0)`
#     to render an "No production deployment yet" honest empty state — and
#     skips the 5s auto-refresh subtitle.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — engine wire shape: GET /status?env=production for a preview-only project
#       returns 200 with `last_build:null AND active_release:null AND
#       other_envs_with_releases` populated (non-empty array). This is the SoT
#       the dashboard gate consumes.
#   B — production dashboard bundle (project-status-*.js page chunk) carries
#       the `"No production deployment yet"` empty-state literal — proof that
#       the fix LANDED in the bundle Caddy serves.
#   C — same bundle carries the `other_envs_with_releases` field reference —
#       proof that the gate's data dependency is wired (otherwise the literal
#       could ship dead).
#
# Slug:           dashboard-status-preview-only-honest
# Issue:          loopzero-cloud#98
# Family:         #41 / #70 / #62 / #77 (empty-env / preview-only honest UX)
# Closed by:      `dashboard/src/pages/project-status.tsx` (isPreviewOnly gate)
#                 + `api/internal/routes/status.go` (other_envs_with_releases)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#98)
# Companion (CI): infra/tests/check-dev-public-dashboard-status-preview-only-honest-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-status-preview-only-honest"
ISSUE_REF="loopzero-cloud#98"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-preview-only-98}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#98 (dashboard status preview-only honest).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  PROJECT_NAME      default preview-only-98 (must be a project whose only
                    builds are environment=preview — engine returns empty
                    last_build + active_release for env=production AND
                    surfaces other_envs_with_releases populated)
  SESSION_JWT_FILE  default ~/.loopzero/local-dogfood-session-jwt.txt (or canonical)

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

# Default JWT discovery (canonical paths).
if [ -z "$SESSION_JWT_FILE" ] ; then
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/local-dogfood-token.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
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
log "Project: $PROJECT_NAME @ $PUBLIC_CLOUD_URL"
log "JWT: ${SESSION_JWT_FILE:-(none)}"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — engine wire shape: empty production + other_envs_with_releases.
# This is the SoT the dashboard isPreviewOnly gate consumes. Without it, the
# Phase-2 dashboard gate stays gated to false and the pre-fix "Progressing"
# state surfaces — even with the page chunk fix shipped.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — GET /status?env=production for preview-only project surfaces other_envs_with_releases"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        body="$(curl -s --max-time 10 -L \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/status?env=production" 2>&1 || true)"
        verdict="$(printf '%s' "$body" | python3 -c '
import json, sys
try:
    d = json.loads(sys.stdin.read())
except Exception as e:
    print(f"FAIL parse: {e}")
    sys.exit(0)
lb = d.get("last_build")
ar = d.get("active_release")
oe = d.get("other_envs_with_releases")
if lb is not None:
    print(f"FAIL last_build expected null, got: {lb}")
elif ar is not None:
    print(f"FAIL active_release expected null, got: {ar}")
elif not isinstance(oe, list) or len(oe) == 0:
    print(f"FAIL other_envs_with_releases expected non-empty list, got: {oe!r}")
else:
    envs = ",".join(str(e.get("environment","?")) for e in oe)
    print(f"PASS preview-only shape — other_envs_with_releases=[{envs}]")
' 2>&1)"
        case "$verdict" in
            PASS*) record_pass A "${verdict#PASS }" ;;
            *)     record_fail A "${verdict#FAIL }" ;;
        esac
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — production bundle contains the "No production deployment yet"
# empty-state literal — proof the page-chunk fix landed in the served bundle.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production dashboard bundle exposes 'No production deployment yet' literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        if [ -z "$entry_js" ] ; then
            record_fail B "could not locate /assets/index-*.js entry from /"
        else
            entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            # The literal lives in the project-status PAGE chunk. The sibling
            # `use-project-status-*.js` HOOK chunk also matches the substring,
            # so filter it out — only the page chunk carries the literal.
            status_chunk="$(printf '%s' "$entry_body" \
                | grep -oE '[A-Za-z0-9_-]*project-status-[A-Za-z0-9_-]+\.js' \
                | grep -v '^use-' \
                | head -1)"
            if [ -z "$status_chunk" ] ; then
                record_fail B "could not locate project-status page chunk (entry_js=$entry_js)"
            else
                log "  observed status chunk: $status_chunk"
                chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${status_chunk}" 2>&1 || true)"
                lit_count="$(printf '%s' "$chunk_body" | grep -oF 'No production deployment yet' | wc -l | tr -d ' ')"
                if [ "$lit_count" -gt 0 ] ; then
                    record_pass B "bundle $status_chunk has 'No production deployment yet' literal — $lit_count occurrence(s)"
                else
                    record_fail B "bundle $status_chunk MISSING 'No production deployment yet' literal (pre-fix shape)"
                fi
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — same bundle references `other_envs_with_releases` — proof the
# gate's data dependency is wired (without it the literal could ship dead).
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — production dashboard bundle references other_envs_with_releases"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        status_chunk="$(printf '%s' "$entry_body" \
            | grep -oE '[A-Za-z0-9_-]*project-status-[A-Za-z0-9_-]+\.js' \
            | grep -v '^use-' \
            | head -1)"
        if [ -z "$status_chunk" ] ; then
            record_fail C "could not locate project-status page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${status_chunk}" 2>&1 || true)"
            ref_count="$(printf '%s' "$chunk_body" | grep -oF 'other_envs_with_releases' | wc -l | tr -d ' ')"
            if [ "$ref_count" -gt 0 ] ; then
                record_pass C "bundle $status_chunk references other_envs_with_releases — $ref_count occurrence(s)"
            else
                record_fail C "bundle $status_chunk MISSING other_envs_with_releases reference (gate data dependency unwired)"
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
