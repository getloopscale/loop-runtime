#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY / T6.1 (Phase 6 — degrau 2).
#
# Droplet install validation: prove that `loopzero platform install` works end-to-
# end on a brand-new DigitalOcean droplet (single-VM k3d-in-VM). Validates
# the public-network path: CF tunnel egress, ACME, GitHub webhook, DOCR
# image pulls. Closes the gap between k3d-localhost (T5.1) and DOKS (T7.1).
#
# This is degrau 2 of the staircase ($0 → ~$0.50 → ~$5 → ~$10).
# Cost target: <$1 (15min of s-2vcpu-4gb-amd droplet @ ~$0.03/hr).
#
# Pre-conditions:
#   - doctl authenticated (DIGITALOCEAN_ACCESS_TOKEN exported)
#   - SSH key registered in DO account; fingerprint in $DROPLET_SSH_KEY_FP
#   - GH App credentials in $GITHUB_APP_SECRETS_FILE (optional — auto-deploy
#     test stub-skipped if missing)
#
# Usage:
#   bash infra/tests/parity/droplet-install.sh [--dry-run]
#   --dry-run: validate pre-conditions, print plan, exit 0 (no doctl provision)
#
# Exit codes:
#   0  — install + dogfood subset GREEN
#   1  — pre-flight failure (auth, doctl, ssh)
#   2  — droplet provision failed
#   3  — install failed on droplet
#   4  — dogfood subset failed
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
DROPLET_NAME="${DROPLET_NAME:-loopzero-parity-$(date -u +%s)}"
DROPLET_REGION="${DROPLET_REGION:-sfo3}"
DROPLET_SIZE="${DROPLET_SIZE:-s-2vcpu-4gb-amd}"
DROPLET_IMAGE="${DROPLET_IMAGE:-ubuntu-24-04-x64}"
DROPLET_SSH_KEY_FP="${DROPLET_SSH_KEY_FP:-}"
SSH_USER="${SSH_USER:-root}"
SSH_KEY="${SSH_KEY:-${HOME}/.ssh/id_ed25519}"
DRY_RUN="${DRY_RUN:-0}"
DOGFOOD_JOURNEYS="${DOGFOOD_JOURNEYS:-journey-01-solo-deploy}"

CLOUD_INIT="${REPO_ROOT}/infra/tests/parity/droplet-cloud-init.sh"

log() { echo "[parity-droplet] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

# Parse flags.
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,30p' "$0" >&2; exit 0 ;;
    *) fail "unknown flag: $arg" 1 ;;
  esac
done

# 1) Pre-flight: required tools + creds.
log "step 1: pre-flight"
command -v doctl >/dev/null 2>&1 || fail "doctl not in PATH (install via brew/snap)" 1
command -v ssh >/dev/null 2>&1 || fail "ssh not in PATH" 1
[[ -n "${DIGITALOCEAN_ACCESS_TOKEN:-}" ]] || fail "DIGITALOCEAN_ACCESS_TOKEN not set" 1
[[ -f "$SSH_KEY" ]] || fail "ssh key not found: $SSH_KEY" 1
[[ -n "$DROPLET_SSH_KEY_FP" ]] || fail "DROPLET_SSH_KEY_FP not set (run 'doctl compute ssh-key list')" 1
[[ -f "$CLOUD_INIT" ]] || fail "cloud-init script missing: $CLOUD_INIT" 1

# Validate doctl auth works (skip API call in dry-run — we want offline validation).
if [[ "$DRY_RUN" == "1" ]]; then
  log "  doctl auth skipped (--dry-run)"
else
  if ! doctl account get >/dev/null 2>&1; then
    fail "doctl auth failed (check DIGITALOCEAN_ACCESS_TOKEN)" 1
  fi
  log "  doctl authenticated"
fi

# 2) Plan summary (dry-run exit point).
log "step 2: plan"
log "  droplet:    $DROPLET_NAME ($DROPLET_SIZE in $DROPLET_REGION)"
log "  image:      $DROPLET_IMAGE"
log "  ssh key fp: $DROPLET_SSH_KEY_FP"
log "  cloud-init: $CLOUD_INIT ($(wc -l < "$CLOUD_INIT") lines)"
log "  dogfood:    $DOGFOOD_JOURNEYS"
log "  est cost:   ~\$0.50 for ~30min"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY-RUN done — no resources provisioned"
  exit 0
fi

# 3) Provision droplet.
log "step 3: provisioning droplet"
DROPLET_ID=$(doctl compute droplet create "$DROPLET_NAME" \
  --region "$DROPLET_REGION" \
  --size "$DROPLET_SIZE" \
  --image "$DROPLET_IMAGE" \
  --ssh-keys "$DROPLET_SSH_KEY_FP" \
  --user-data-file "$CLOUD_INIT" \
  --wait \
  --format ID --no-header 2>/dev/null) || fail "droplet create failed" 2

log "  droplet id: $DROPLET_ID"

cleanup() {
  log "cleanup: destroying droplet $DROPLET_ID"
  doctl compute droplet delete "$DROPLET_ID" --force >/dev/null 2>&1 || \
    log "  warning: droplet delete returned non-zero (manual cleanup may be required)"
}
trap cleanup EXIT

DROPLET_IP=$(doctl compute droplet get "$DROPLET_ID" --format PublicIPv4 --no-header 2>/dev/null)
[[ -n "$DROPLET_IP" ]] || fail "could not resolve droplet public IP" 2
log "  public ip: $DROPLET_IP"

# 4) Wait for cloud-init to finish.
log "step 4: waiting for cloud-init (k3d + helmfile + loopzero CLI install)"
for attempt in $(seq 1 60); do
  if ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
      -i "$SSH_KEY" "$SSH_USER@$DROPLET_IP" "test -f /var/log/loopzero-parity-ready" 2>/dev/null; then
    log "  cloud-init ready (attempt $attempt)"
    break
  fi
  if [[ $attempt -eq 60 ]]; then
    ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$SSH_USER@$DROPLET_IP" \
      "tail -50 /var/log/cloud-init-output.log" >&2 || true
    fail "cloud-init didn't finish in 10min" 3
  fi
  sleep 10
done

# 5) Run install on the droplet.
log "step 5: running 'loopzero platform install' inside droplet"
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -i "$SSH_KEY" "$SSH_USER@$DROPLET_IP" \
    "cd /opt/loopzero && bash infra/tests/parity/k3d-virgin-install.sh" 2>&1 | tee /tmp/parity-droplet-install.log
status=${PIPESTATUS[0]}
[[ $status -eq 0 ]] || fail "install on droplet exit=$status (see /tmp/parity-droplet-install.log)" 3

# 6) Run dogfood subset.
log "step 6: dogfood subset ($DOGFOOD_JOURNEYS)"
ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$SSH_USER@$DROPLET_IP" \
    "cd /opt/loopzero && bash infra/tests/dogfood/full-loop.sh --only $DOGFOOD_JOURNEYS" 2>&1 | tee /tmp/parity-droplet-dogfood.log
status=${PIPESTATUS[0]}
[[ $status -eq 0 ]] || fail "dogfood subset failed (see /tmp/parity-droplet-dogfood.log)" 4

log "DONE — droplet install + dogfood GREEN (cost: ~\$0.50)"
exit 0
