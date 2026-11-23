#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Validates a commit deployed to staging, and reports the staging/validate commit status.
# infra/staging/deploy.sh runs it after every successful deploy, from the checkout of the
# deployed commit; it can also be run by hand on the staging host:
#
#   infra/staging/validate.sh <sha>
#
# One run:
#   1. Posts staging/validate pending on the commit.
#   2. Seeds the validation identity and mints its token (seed_token below). The identity
#      lives in Kratos, which a rebuild of staging empties, so it is seeded on every run;
#      its password is random and never written down, and the session and the token stay
#      under the state directory, mode 0600.
#   3. Runs the dogfood suite (infra/tests/livetest/dogfood) against the public staging
#      hostnames, with the journeys of JOURNEYS below.
#   4. Posts staging/validate success or failure. Any exit before that, an error in a step
#      included, posts failure: a run never leaves the status pending.
#
# The log of a run is $STATE_DIR/logs/<sha>.validate.log.
#
# Everything outside the host is reached through variables, so a test can stand in for
# GitHub, the token and the suite.
set -euo pipefail

REPO="${DEPLOY_REPO:-getloopscale/loopzero}"
GITHUB_API="${DEPLOY_GITHUB_API:-https://api.github.com}"
STATE_DIR="${DEPLOY_STATE_DIR:-/var/lib/loopzero-staging/deploy}"
TOKEN_FILE="${DEPLOY_STATUS_TOKEN_FILE:-/etc/loopzero-staging/github-status-token}"
STATUS_URL_BASE="${DEPLOY_STATUS_URL_BASE:-https://staging.getloopscale.com}"
CHECKOUT="${VALIDATE_CHECKOUT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
KUBECONFIG_FILE="${VALIDATE_KUBECONFIG:-/var/lib/loopzero-staging/kubeconfig}"
EMULATOR_DIR="${VALIDATE_EMULATOR_DIR:-/var/lib/aws-emulator/.aws/emulator/9597}"
# The commands of steps 2 and 3, replaced by a test. Each runs from the checkout's root.
TOKEN_CMD="${VALIDATE_TOKEN_CMD:-seed_token}"
SUITE_CMD="${VALIDATE_SUITE_CMD:-run_suite}"

# The staging hostnames the suite is pointed at (infra/tests/livetest/dogfood/helpers.go).
export LOOPZERO_DOGFOOD_CLOUD_URL="${LOOPZERO_DOGFOOD_CLOUD_URL:-https://cloud.staging.getloopscale.com}"
export LOOPZERO_DOGFOOD_APP_URL="${LOOPZERO_DOGFOOD_APP_URL:-https://app.staging.getloopscale.com}"
export LOOPZERO_DOGFOOD_AUTH_URL="${LOOPZERO_DOGFOOD_AUTH_URL:-https://auth.staging.getloopscale.com}"
# The deep health of staging is loopzero-api's: database, Argo CD, Gitea and the cells.
# loopzero-cloud has no deep health (it answers 404 on /health/deep).
export LOOPZERO_DOGFOOD_DEEP_HEALTH_URL="${LOOPZERO_DOGFOOD_DEEP_HEALTH_URL:-https://api.staging.getloopscale.com/api/v1/health/deep}"
export LOOPZERO_DOGFOOD_JWT_PATH="$STATE_DIR/validate/jwt"

# The journeys that exercise what staging runs: loopzero-api, loopzero-cloud and its
# /engine proxy, and Kratos. Every journey checks the deep health before its scenarios.
#   j01 login: a seeded session authenticates against an engine route
#   j02 sign-up: Kratos opens a registration flow
#   j04 recovery, j05 device flow: the authenticated prerequisites of both flows
#   j07 projects list, j10 environments, j11 domains, j22 secrets: engine lists
#   j14 audit log: the engine's audit entries
#   j15 profile: Kratos whoami answers
#   j18 status: the deep health, and the engine health through the proxy
# Left out, because staging does not run what they need:
#   j06 j08 j09 j17 j19 j20 j21 j23 j25: read the canonical `sample` project, which the
#     dev seed connects to a fixture GitHub repository; staging has no GitHub App, no
#     fixture repository and no such project
#   j13 billing: Stripe is not configured on staging (loopzero-cloud mounts no /v1/billing)
#   j26 storage: loopzero-cloud runs with storage disabled (no /v1/storage)
#   dp-memory dp-rag dp-lens: the data-plane products are not deployed on staging
# A scenario that is still a sketch calls Skip; Ginkgo reports it as skipped, not passed.
JOURNEYS="${VALIDATE_JOURNEYS:-j01 || j02 || j04 || j05 || j07 || j10 || j11 || j14 || j15 || j18 || j22}"

sha="${1:-}"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || { echo "usage: $0 <sha of the deployed commit>" >&2; exit 2; }
short=${sha:0:7}
LOGS="$STATE_DIR/logs"
mkdir -p "$LOGS"
log_file="$LOGS/$sha.validate.log"

log() { printf '%s staging-validate: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
fail() { log "error: $*"; exit 1; }

[[ -r "$TOKEN_FILE" ]] || fail "no status token at $TOKEN_FILE: create a fine-grained token with Commit statuses read and write on $REPO"
TOKEN="$(<"$TOKEN_FILE")"

# status <state> <description>: the staging/validate commit status of the commit.
status() {
  local body
  body=$(jq -n --arg state "$1" --arg description "${2:0:140}" --arg context "staging/validate" \
    --arg target "$STATUS_URL_BASE" '{state: $state, description: $description, context: $context, target_url: $target}')
  curl -fsS --max-time 30 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" -X POST "$GITHUB_API/repos/$REPO/statuses/$sha" -d "$body" >/dev/null \
    || { log "error: could not post staging/validate $1 on $short"; return 1; }
}

reported=""
on_exit() {
  local code=$?
  if [[ -z "$reported" ]]; then
    status failure "validation of $short stopped (exit $code); see its validate log on the staging host" || true
  fi
}
trap on_exit EXIT

# kube_env: what kubectl needs to reach the emulator's EKS clusters.
kube_env() {
  export KUBECONFIG="$KUBECONFIG_FILE" AWS_CONFIG_FILE="$EMULATOR_DIR/config" \
    AWS_SHARED_CREDENTIALS_FILE="$EMULATOR_DIR/emulator_credentials" AWS_CA_BUNDLE="$EMULATOR_DIR/ca.pem" \
    AWS_ENDPOINT_URL=https://localhost:9597 AWS_REGION=us-east-1
}

# seed_token: the validation identity in Kratos (loopzero-ops seed admin-loopzero-dev,
# the dev seed pointed at staging), then its session exchanged at loopzero-cloud for the
# bearer token the suite sends, written to $LOOPZERO_DOGFOOD_JWT_PATH.
#
# It runs in a subshell inside an `if`, where bash ignores errexit: every step that can
# fail returns on its own.
seed_token() {
  kube_env
  local dir="$STATE_DIR/validate" port password code
  (cd cmd/loopzero-ops && GOWORK=off go build -o "$dir/loopzero-ops" .) || return 1
  # The Kratos admin API is never exposed; it is reached through a port-forward.
  port=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])') || return 1
  kubectl --context core -n loopzero-auth port-forward svc/ory-kratos-admin "$port:80" >/dev/null 2>&1 &
  # shellcheck disable=SC2064 # the pid of this port-forward, in the subshell that runs the step
  trap "kill $! 2>/dev/null || true" EXIT
  for _ in $(seq 30); do
    curl -fsS -o /dev/null "http://127.0.0.1:$port/health/ready" 2>/dev/null && break
    sleep 1
  done
  password=$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 40)
  CORE_CONTEXT=core KRATOS_ADMIN_URL="http://127.0.0.1:$port" EMAIL=staging-validate@getloopscale.com \
    SEED_PASSWORD="$password" PUBLIC_KRATOS_URL="$LOOPZERO_DOGFOOD_AUTH_URL" PUBLIC_CLOUD_URL="$LOOPZERO_DOGFOOD_APP_URL" \
    SESSION_FILE="$dir/session" LOOPZERO_DEV_CREDENTIALS="$dir/no-dev-credentials.json" \
    "$dir/loopzero-ops" seed admin-loopzero-dev || return 1
  # The session cookie goes to curl through a file, never on a command line.
  printf 'Cookie: ory_kratos_session=%s\n' "$(<"$dir/session")" >"$dir/session-header" || return 1
  code=$(curl -sS --max-time 30 -o "$dir/exchange.json" -w '%{http_code}' -X POST \
    -H @"$dir/session-header" "$LOOPZERO_DOGFOOD_CLOUD_URL/v1/auth/session/token") || code=000
  rm -f "$dir/session-header"
  if [[ "$code" != 200 ]]; then
    echo "POST /v1/auth/session/token answered HTTP $code" >&2
    rm -f "$dir/exchange.json"
    return 1
  fi
  jq -r '.access_token // empty' "$dir/exchange.json" >"$LOOPZERO_DOGFOOD_JWT_PATH" || return 1
  rm -f "$dir/exchange.json"
  chmod 600 "$LOOPZERO_DOGFOOD_JWT_PATH"
  [[ -s "$LOOPZERO_DOGFOOD_JWT_PATH" ]] || { echo "the session exchange returned no access_token" >&2; return 1; }
  echo "validation identity seeded; token written to $LOOPZERO_DOGFOOD_JWT_PATH"
}

# run_suite: the journeys of JOURNEYS, with -v so a passing run logs its summary too.
# -ginkgo.fail-on-empty fails a filter that selects
# no spec, so a renamed label cannot turn the run into a green one with nothing run.
run_suite() {
  cd infra/tests || return 1
  GOWORK=off go test -v -tags=live -count=1 -timeout 30m ./livetest/dogfood/ -run TestDogfoodLive \
    -args -ginkgo.label-filter="$JOURNEYS" -ginkgo.fail-on-empty -ginkgo.v -ginkgo.no-color
}

status pending "validating $short"
mkdir -p "$STATE_DIR/validate" && chmod 700 "$STATE_DIR/validate"
log "validating $short (log: $log_file)"
: >"$log_file"
chmod 600 "$log_file"

if ! (cd "$CHECKOUT" && $TOKEN_CMD) >>"$log_file" 2>&1; then
  reported=yes
  status failure "could not seed the validation identity for $short; see its validate log on the staging host"
  fail "$short: the validation identity could not be seeded; see $log_file"
fi

if (cd "$CHECKOUT" && $SUITE_CMD) >>"$log_file" 2>&1; then
  reported=yes
  status success "staging journeys passed on $short"
  log "$short validated"
else
  code=$?
  reported=yes
  failed=$(grep -cE '^\s*\[FAIL\]' "$log_file" || true)
  what="the staging journeys exited $code"
  (( failed > 0 )) && what="$failed staging journey specs failed"
  status failure "$what on $short; see its validate log on the staging host"
  fail "$short: the staging journeys failed (exit $code); see $log_file"
fi
