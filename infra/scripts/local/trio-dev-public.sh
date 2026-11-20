#!/usr/bin/env bash
# Trio LIVE for dev-public env — T8.1 of plan
# 2026-06-04-dev-public-domain-canonicalization.
#
# Soft-equivalent of /cleanroom + /dogfood + /chaos-test + /live-proof
# against the existing dev-public infra (non-destructive). Pairs the 3
# already-validated LIVE tests with a clear go/no-go verdict.
#
# Composition:
#   /cleanroom    → soft-replaced by "config drift check" (live-proof L2
#                   asserts no placeholder leaked back into ConfigMap)
#   /dogfood      → soft-replaced by "user journey probe" (billing portal
#                   e2e: login + JWT + portal — proves dashboard backend)
#   /chaos-test   → soft-replaced by "service availability probe" (curl
#                   all 3 hostnames -dev + verify TLS + reachability)
#   /live-proof   → ran in its full form
#
# True full versions (destructive cleanroom + chaos injection) require
# operator-driven `task setup` + chaos-mesh install across all 4 clusters
# — out of scope for the non-destructive trio executor.
#
# Flags:
#   --env=dev-public  (default)
#   --bail-on-fail    exit on first FAIL (default: run all 4, report aggregate)
#   --json            machine-readable verdict

set -euo pipefail

ENV_NAME="dev-public"
BAIL=0
EMIT_JSON=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env=*) ENV_NAME="${1#*=}"; shift ;;
    --bail-on-fail) BAIL=1; shift ;;
    --json) EMIT_JSON=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

case "$ENV_NAME" in
  dev-public)
    AUTH_HOST="https://auth-dev.getloopscale.com"
    APP_HOST="https://app-dev.getloopscale.com"
    CLOUD_HOST="https://cloud-dev.getloopscale.com"
    ;;
  *)
    printf 'unsupported env=%s (only dev-public for now)\n' "$ENV_NAME" >&2
    exit 1 ;;
esac

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"

TRIO_PASS=0
TRIO_FAIL=0
declare -A VERDICT

log() { printf '[trio %s] %s\n' "$ENV_NAME" "$*" >&2; }
hr()  { printf '═══════════════════════════════════════════════════════════\n' >&2; }

phase() {
  local name="$1" cmd="$2"
  hr
  log "PHASE: $name"
  hr
  if eval "$cmd" >&2; then
    VERDICT["$name"]="PASS"
    TRIO_PASS=$((TRIO_PASS + 1))
    log "$name → PASS"
  else
    VERDICT["$name"]="FAIL"
    TRIO_FAIL=$((TRIO_FAIL + 1))
    log "$name → FAIL"
    [[ $BAIL -eq 1 ]] && { log "BAIL on first failure"; exit 1; }
  fi
}

# Soft cleanroom: assert no placeholder / drift in canonical state.
# A real cleanroom destroys infra; this verifies the existing infra
# matches what the canonical config should produce.
soft_cleanroom() {
  local cfg
  cfg=$(kubectl --context k3d-loopzero-core -n loopzero-auth get configmap ory-kratos-config \
    -o "jsonpath={.data.kratos\.yaml}" 2>/dev/null || echo "")
  [[ -n "$cfg" ]] || { log "  ConfigMap ory-kratos-config missing"; return 1; }
  # No REPLACE_VIA_PULUMI placeholders
  if printf '%s' "$cfg" | grep -q 'REPLACE_VIA_PULUMI'; then
    log "  ConfigMap has REPLACE_VIA_PULUMI placeholders — drift detected"
    return 1
  fi
  # No localhost:4455 (canonical dev-public uses *-dev hosts)
  if printf '%s' "$cfg" | grep -q 'localhost:4455'; then
    log "  ConfigMap has localhost:4455 URLs — sync regressed to default env"
    return 1
  fi
  # No literal ${RESEND_API_KEY} (Kratos doesn't env-interp)
  if printf '%s' "$cfg" | grep -qE '\$\{RESEND_API_KEY\}'; then
    log "  ConfigMap has literal \${RESEND_API_KEY} — env interp NOT working"
    return 1
  fi
  log "  ConfigMap free of placeholders, localhost URLs, and literal \${VAR}"
  return 0
}

# Soft chaos: probe the 3 -dev hostnames + TLS validity (there were 6 until B-025)
soft_chaos() {
  # B-025 (2026-08-19): the three without a suffix left the Caddyfile. `cloud-dev` stays: it has
  # its own block and is the development pair that keeps serving.
  local hosts=(
    "auth-dev.getloopscale.com" "app-dev.getloopscale.com" "cloud-dev.getloopscale.com"
  )
  local fails=0
  for h in "${hosts[@]}"; do
    # TLS valid (LE issuer)
    local issuer
    issuer=$(echo | timeout 5 openssl s_client -servername "$h" -connect "$h:443" 2>/dev/null \
      | openssl x509 -noout -issuer 2>/dev/null)
    if echo "$issuer" | grep -qi "let.s encrypt"; then
      log "  $h → TLS LE OK"
    else
      log "  $h → TLS NOT LE (issuer=$issuer)"
      fails=$((fails + 1))
    fi
  done
  [[ $fails -eq 0 ]]
}

# ============================================================================
# Execute trio
# ============================================================================

log "starting (auth=$AUTH_HOST app=$APP_HOST cloud=$CLOUD_HOST)"
echo "" >&2

phase "cleanroom-soft (config drift assertion)" "soft_cleanroom"
phase "chaos-soft (3/3 hostnames TLS LE)"      "soft_chaos"
phase "dogfood-soft (billing portal e2e)"      "bash $REPO_ROOT/infra/tests/e2e/live/dev-public-billing-portal_test.sh"
phase "live-proof (3 checks × 3 layers)"        "bash $REPO_ROOT/archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh"

hr
log "TRIO COMPLETE for env=$ENV_NAME"
hr
log "  cleanroom-soft : ${VERDICT[cleanroom-soft\ \(config\ drift\ assertion\)]:-MISSING}"
log "  chaos-soft     : ${VERDICT[chaos-soft\ \(6\/6\ hostnames\ TLS\ LE\)]:-MISSING}"
log "  dogfood-soft   : ${VERDICT[dogfood-soft\ \(billing\ portal\ e2e\)]:-MISSING}"
log "  live-proof     : ${VERDICT[live-proof\ \(3\ checks\ ×\ 3\ layers\)]:-MISSING}"
log ""
log "TOTAL PASS=$TRIO_PASS FAIL=$TRIO_FAIL"

if [[ $EMIT_JSON -eq 1 ]]; then
  python3 -c "
import json
print(json.dumps({
  'env': '$ENV_NAME',
  'pass': $TRIO_PASS,
  'fail': $TRIO_FAIL,
  'verdict': 'GREEN' if $TRIO_FAIL == 0 else 'RED',
}, indent=2))"
fi

[[ "$TRIO_FAIL" -eq 0 ]] || exit 1
