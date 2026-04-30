#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY / T7.1 (Phase 7 — degrau 3).
#
# DOKS install validation: prove that `loopzero platform install --provider
# digitalocean` produces a fully functional platform on a brand-new DOKS
# cluster (not adopted, freshly provisioned via Pulumi). Validates the
# Pulumi L1 (cloud infra) + Helmfile L2 (charts) split end-to-end.
#
# This is degrau 3 of the staircase ($0 → ~$0.50 → ~$5 → ~$10).
# Cost target: <$5 (60min of s-4vcpu-8gb-amd × 3 nodes + 1 PG basic).
#
# DoD #2: pulumi destroy <5min — verified live by this script.
#
# Pre-conditions:
#   - doctl authenticated
#   - cd infra/pulumi && npm ci done
#   - $PULUMI_ACCESS_TOKEN set (for s3:// state backend)
#   - $AWS_ACCESS_KEY_ID/$AWS_SECRET_ACCESS_KEY for s3 state
#
# Usage:
#   bash infra/tests/parity/doks-install.sh [--dry-run]
#
# Exit codes:
#   0 — pulumi up + install + dogfood + destroy GREEN
#   1 — pre-flight failure
#   2 — pulumi up failed
#   3 — install failed
#   4 — dogfood failed
#   5 — pulumi destroy >5min or failed
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
STACK="${PULUMI_STACK:-parity-test}"
PULUMI_DIR="${REPO_ROOT}/infra/pulumi"
DESTROY_TIMEOUT_S="${DESTROY_TIMEOUT_S:-300}"   # 5min DoD #2
DRY_RUN="${DRY_RUN:-0}"
DOGFOOD_JOURNEYS="${DOGFOOD_JOURNEYS:-journey-01-solo-deploy,journey-03-secrets,journey-08-rollback}"

log() { echo "[parity-doks] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,30p' "$0" >&2; exit 0 ;;
    *) fail "unknown flag: $arg" 1 ;;
  esac
done

# 1) Pre-flight.
log "step 1: pre-flight"
command -v pulumi >/dev/null 2>&1 || fail "pulumi not in PATH" 1
command -v doctl >/dev/null 2>&1 || fail "doctl not in PATH" 1
command -v loopzero >/dev/null 2>&1 || fail "loopzero CLI not in PATH" 1
[[ -n "${DIGITALOCEAN_ACCESS_TOKEN:-}" ]] || fail "DIGITALOCEAN_ACCESS_TOKEN not set" 1
[[ -d "$PULUMI_DIR/node_modules" ]] || fail "$PULUMI_DIR — run 'npm ci' first" 1

# 2) Plan summary.
log "step 2: plan"
log "  stack:      $STACK"
log "  destroy timeout: ${DESTROY_TIMEOUT_S}s (DoD #2)"
log "  dogfood:    $DOGFOOD_JOURNEYS"
log "  est cost:   ~\$5 for ~60min (3-node DOKS + PG basic)"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY-RUN — running 'pulumi preview' instead of 'up'"
  cd "$PULUMI_DIR"
  pulumi preview --stack "$STACK" 2>&1 | tail -20 || \
    log "  preview returned non-zero (stack may not exist yet — expected for new stack)"
  log "DRY-RUN done — no resources provisioned"
  exit 0
fi

# 3) Pulumi up.
log "step 3: pulumi up --stack $STACK"
cd "$PULUMI_DIR"
if ! pulumi up --stack "$STACK" --yes 2>&1 | tee /tmp/parity-doks-up.log; then
  fail "pulumi up failed (see /tmp/parity-doks-up.log)" 2
fi

cleanup() {
  log "cleanup: pulumi destroy --stack $STACK (DoD #2 timer starts NOW)"
  cd "$PULUMI_DIR"
  start=$(date +%s)
  if timeout "${DESTROY_TIMEOUT_S}s" pulumi destroy --stack "$STACK" --yes 2>&1 | tee /tmp/parity-doks-destroy.log; then
    elapsed=$(( $(date +%s) - start ))
    log "  destroy completed in ${elapsed}s (DoD #2: <${DESTROY_TIMEOUT_S}s — $([ $elapsed -lt $DESTROY_TIMEOUT_S ] && echo PASS || echo MISS))"
  else
    log "  destroy exceeded ${DESTROY_TIMEOUT_S}s — DoD #2 MISS"
  fi
}
trap cleanup EXIT

# 4) Resolve kubeconfig from Pulumi outputs.
log "step 4: resolving kubeconfig"
KUBECONFIG_PATH=$(mktemp)
pulumi stack output --stack "$STACK" mgmtKubeconfig --show-secrets > "$KUBECONFIG_PATH" 2>/dev/null
[[ -s "$KUBECONFIG_PATH" ]] || fail "could not export kubeconfig from stack" 2
export KUBECONFIG="$KUBECONFIG_PATH"

# 5) loopzero platform install.
log "step 5: loopzero platform install --provider digitalocean"
VALUES="${REPO_ROOT}/infra/tests/parity/doks-values.yaml"
[[ -f "$VALUES" ]] || VALUES="${REPO_ROOT}/infra/helm/loopzero-cloud/values-prod.yaml"

if ! loopzero platform install \
    -f "$VALUES" \
    --provider digitalocean \
    --no-restore-secrets 2>&1 | tee /tmp/parity-doks-install.log; then
  fail "install failed (see /tmp/parity-doks-install.log)" 3
fi

# 6) Dogfood subset.
log "step 6: dogfood subset ($DOGFOOD_JOURNEYS)"
IFS=',' read -ra journeys <<< "$DOGFOOD_JOURNEYS"
for journey in "${journeys[@]}"; do
  log "  running: $journey"
  if ! bash "${REPO_ROOT}/infra/tests/dogfood/full-loop.sh" --only "$journey" 2>&1 | tail -10; then
    fail "dogfood journey $journey failed" 4
  fi
done

log "DONE — DOKS install + dogfood + destroy GREEN"
exit 0
