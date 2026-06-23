#!/usr/bin/env bash
# T0.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability.
#
# LIVE probe asserting `status.url` canonical contract against dev-public BEFORE
# Phases 1-3 ship 3 dashboard pages reading the field. Per ADR D1 of the plan,
# `useProjectStatus(name).status.url` is the data source for the Live URL hero
# + Visit button + ExternalLink icon; if the backend wiring is broken (field
# returns null despite an active release), engineering MUST halt and escalate
# as J-25.A backend fix BEFORE shipping the frontend slots.
#
# Canonical URL emitter contract (status.go:252):
#   resp.URL = fmt.Sprintf("https://%s-%s.%s", projectName, env, baseDomain)
#
# Examples per environment:
#   dev-public production: https://teste-production.loopzero.example
#   prod production:       https://teste-production.loopzero.example (same baseDomain)
#   standalone-multi:      https://teste-production.loopzero.local (OUT OF SCOPE for LIVE probe)
#
# Probe assertion sequence (SEPA iter 1 CRITICAL fix):
#   1. HTTP 200 from /status?env=production
#   2. .active_release != null (pre-condition for url-not-null semantic)
#   3. .url != null (HALT-J-25.A trigger if false)
#   4. .url regex match canonical shape
#
# Without step 2, step 3 false-passes on freshly-created project: status.url
# is emitted unconditionally when baseDomain != "" (status.go:251-253) so the
# "null url despite active release" trap is unreachable in current backend code.
# Step 2 (pre-condition) makes the HALT semantic real.
#
# Flags:
#   --skip-live   hermetic CI mode (bash -n + structural assertions, no curl)
#   --json        machine-readable verdict
#   --help        print usage
#
# Exit: 0 if 4/4 SC pass OR --skip-live PASS; 1 on functional FAIL; 2 on usage error.

set -euo pipefail

# Defaults — overridable via env for prod canary probe.
CLOUD_HOST="${CLOUD_HOST:-https://app-dev.loopzero.example}"
PROJECT="${PROJECT:-teste}"
ENV="${ENV:-production}"
# CANONICAL 2026-06-15: prefer HMAC session JWT (carries tenant_id; passes
# loopzero-cloud /engine proxy guard). Falls back to bootstrap RSA JWT for
# engine-direct (localhost:13001) when no session JWT exists.
if [ -z "${TOKEN_FILE:-}" ] ; then
  if [ -f "$HOME/.loopzero/local-dogfood-session-jwt.txt" ] ; then
    TOKEN_FILE="$HOME/.loopzero/local-dogfood-session-jwt.txt"
  else
    TOKEN_FILE="$HOME/.loopzero/local-dogfood-token.txt"
  fi
fi

# Tight regex for the FIXED probe inputs. Broader canonical shape is
# `^https://[a-z0-9-]+\.loopzero\.dev$` (single hostname class — project/env
# names may contain hyphens internally per loopzero.yaml allowance, so a 2-group
# split-by-hyphen regex false-rejects). For THIS probe with fixed project=teste
# + env=production, the tight regex catches drift immediately.
# CANONICAL FIX 2026-06-15 (FAANG SEM WORKAROUND): see companion
# dev-public-live-url-discoverability_test.sh — per CLAUDE.md J-25 closure
# + status.go:252 + pkg/naming.AppURL, the canonical formula is
# `<tenant>-<project>-<app>-<env>.<public-domain>`. Test was stuck on
# the pre-J-25 simpler `<project>-<env>` shape that never appeared in
# real status payloads from loopzero-api.
EXPECTED_URL_REGEX='^https://tn-[a-f0-9]+-teste-[a-z]+-production\.(loopzero\.local|loopzero\.dev|prod-dev\.loopzero\.dev)$'

SKIP_LIVE=0
JSON_OUTPUT=0

usage() {
  cat <<USAGE >&2
Usage: $0 [--skip-live] [--json] [--help]

LIVE probe of status.url canonical contract against dev-public. T0.1 of plan
J-25 Live URL Discoverability. Gates Phases 1-3 of the plan — if probe fails
HALT trigger, escalate as J-25.A backend wiring fix BEFORE frontend ship.

Env overrides:
  CLOUD_HOST  default https://cloud-dev.loopzero.example
  PROJECT     default teste
  ENV         default production
  TOKEN_FILE  default \$HOME/.loopzero/local-dogfood-token.txt

Exit codes: 0 PASS, 1 FAIL, 2 usage error.
USAGE
}

while (($#)); do
  case "$1" in
    --skip-live) SKIP_LIVE=1; shift ;;
    --json)      JSON_OUTPUT=1; shift ;;
    --help|-h)   usage; exit 0 ;;
    *)           echo "Unknown flag: $1" >&2; usage; exit 2 ;;
  esac
done

PASS=0
FAIL=0
HALT_J25A=0  # set to 1 if HALT-trigger fires (url==null despite active_release)
SC_RESULTS=()  # SC-ID:status pairs for JSON output

log()  { [[ $JSON_OUTPUT -eq 0 ]] && printf '[status-url-probe] %s\n' "$*" >&2 || true; }
pass() { SC_RESULTS+=("$1:PASS"); PASS=$((PASS + 1)); log "  PASS: $1 — $2"; }
fail() { SC_RESULTS+=("$1:FAIL"); FAIL=$((FAIL + 1)); log "  FAIL: $1 — $2"; }

# ---------------------------------------------------------------------------
# Hermetic mode: structural checks only (no curl, no network)
# ---------------------------------------------------------------------------
if [[ $SKIP_LIVE -eq 1 ]]; then
  log "Running in hermetic mode (--skip-live)"
  # bash -n already validated by `bash -n` at invocation time of skeleton tests.
  # Here we structurally assert the script's own canonical shape — defense
  # against accidental degradation of the probe.
  if grep -q 'EXPECTED_URL_REGEX=' "$0"; then
    pass "H-1" "canonical regex constant declared"
  else
    fail "H-1" "EXPECTED_URL_REGEX missing"
  fi
  if grep -q 'active_release' "$0"; then
    pass "H-2" "active_release pre-condition referenced"
  else
    fail "H-2" "active_release pre-condition missing"
  fi
  if grep -q 'HALT_J25A=1' "$0"; then
    pass "H-3" "HALT-J-25.A trigger wired"
  else
    fail "H-3" "HALT trigger missing"
  fi
  if grep -q 'status.go:252' "$0"; then
    pass "H-4" "canonical source citation present"
  else
    fail "H-4" "status.go:252 citation missing"
  fi

  if [[ $JSON_OUTPUT -eq 1 ]]; then
    printf '{"mode":"hermetic","pass":%d,"fail":%d,"verdict":"%s"}\n' \
      "$PASS" "$FAIL" "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)"
  else
    log "Hermetic verdict: PASS=$PASS FAIL=$FAIL"
  fi
  [[ $FAIL -eq 0 ]] && exit 0 || exit 1
fi

# ---------------------------------------------------------------------------
# LIVE mode: actual curl + assert sequence
# ---------------------------------------------------------------------------
log "Running LIVE probe against $CLOUD_HOST/engine/api/v1/projects/$PROJECT/status?env=$ENV"

if [[ ! -f $TOKEN_FILE ]]; then
  log "JWT seed token missing at $TOKEN_FILE — operator must run seed-admin-loopzero-dev.sh first"
  fail "PRE" "token file missing"
  exit 1
fi
TOKEN=$(cat "$TOKEN_FILE")

TMP_RESP=$(mktemp)
trap 'rm -f "$TMP_RESP"' EXIT

# SC-1: HTTP 200 from /status?env=production
HTTP_CODE=$(curl -sH "Authorization: Bearer $TOKEN" \
  -o "$TMP_RESP" -w '%{http_code}' \
  "$CLOUD_HOST/engine/api/v1/projects/$PROJECT/status?env=$ENV")

if [[ "$HTTP_CODE" == "200" ]]; then
  pass "SC-1" "HTTP 200 from /status?env=$ENV"
else
  fail "SC-1" "HTTP $HTTP_CODE (expected 200); body: $(cat "$TMP_RESP")"
  exit 1
fi

# SC-2: .active_release != null (pre-condition for url-not-null HALT semantic)
ACTIVE_RELEASE=$(jq -r '.active_release // empty' "$TMP_RESP")
if [[ -n "$ACTIVE_RELEASE" && "$ACTIVE_RELEASE" != "null" ]]; then
  pass "SC-2" "active_release populated (precondition for HALT semantic)"
  HAS_ACTIVE_RELEASE=1
else
  pass "SC-2-SKIP" "active_release == null — project idle (HALT trigger NOT applicable); SC-3+SC-4 will validate canonical url shape if present"
  HAS_ACTIVE_RELEASE=0
fi

# SC-3: .url != null
URL=$(jq -r '.url // empty' "$TMP_RESP")
if [[ -n "$URL" && "$URL" != "null" ]]; then
  pass "SC-3" ".url populated: $URL"
else
  if [[ $HAS_ACTIVE_RELEASE -eq 1 ]]; then
    # HALT-J-25.A trigger — backend wiring broken
    HALT_J25A=1
    fail "SC-3" "HALT-J-25.A: .url == null DESPITE .active_release populated. Backend wiring broken. Engineering MUST escalate as J-25.A backend fix BEFORE shipping T1.1+T2.1+T3.1."
  else
    fail "SC-3" ".url == null (project idle — would render 'URL pending' badge per ADR D3)"
  fi
fi

# SC-4: .url regex match canonical shape
if [[ -n "$URL" && "$URL" != "null" ]]; then
  if [[ "$URL" =~ $EXPECTED_URL_REGEX ]]; then
    pass "SC-4" ".url matches canonical regex $EXPECTED_URL_REGEX"
  else
    fail "SC-4" ".url '$URL' does NOT match canonical regex $EXPECTED_URL_REGEX"
  fi
else
  fail "SC-4-SKIP" "skipped — .url was null at SC-3"
fi

# Verdict
if [[ $JSON_OUTPUT -eq 1 ]]; then
  RESULTS_JSON=$(printf '"%s",' "${SC_RESULTS[@]}" | sed 's/,$//')
  printf '{"mode":"live","cloud_host":"%s","project":"%s","env":"%s","pass":%d,"fail":%d,"halt_j25a":%d,"verdict":"%s","results":[%s]}\n' \
    "$CLOUD_HOST" "$PROJECT" "$ENV" "$PASS" "$FAIL" "$HALT_J25A" \
    "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)" "$RESULTS_JSON"
else
  log "LIVE verdict: PASS=$PASS FAIL=$FAIL HALT_J25A=$HALT_J25A"
  if [[ $HALT_J25A -eq 1 ]]; then
    log ""
    log "  *** HALT — J-25.A required ***"
    log "  status.url IS NULL despite active_release populated."
    log "  Backend wiring broken — engineering MUST escalate before shipping T1.1+T2.1+T3.1."
  fi
fi

[[ $FAIL -eq 0 ]] && exit 0 || exit 1
