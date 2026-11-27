#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# sandbox.sh up|down|address
#
# The sandbox service of the staging environment, on the host's Docker. `up` starts it,
# or replaces it when its image or the console's key changed; `down` removes it with
# every sandbox it ran; `address` prints the URL loop-console reaches it at.
#
# The API listens on the gateway of the core VPC's Docker network, which the emulator
# creates. `up` and `address` therefore need the VPC to exist (LOOP_RUNTIME_VPC_ID).
#
# up reads:
#   LOOP_RUNTIME_VPC_ID      the core VPC, an output of infra-live
#   SANDBOX_GATEWAY_PUBKEY   base64 Ed25519 public key loop-console signs with
#   SANDBOX_IMAGE            optional; default: the pin of the staging environment
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
COMPOSE="$ROOT/infra/staging/sandbox/compose.yaml"
STATE="${SANDBOX_STATE_DIR:-/var/lib/loop-runtime-staging/sandbox}"
PORT="${SANDBOX_PORT:-8090}"
# What sandboxes are created from, pulled ahead so a first create does not wait for it.
WARM_IMAGES="${SANDBOX_WARM_IMAGES:-python:3.12-slim}"

log() { printf '[staging-sandbox] %s\n' "$*" >&2; }
fail() { log "error: $*"; exit 1; }

gateway() {
  local vpc="${LOOP_RUNTIME_VPC_ID:?LOOP_RUNTIME_VPC_ID is not set}" network
  network="$(docker network ls -q --filter "label=aws-emulator.vpc-id=$vpc" | head -n1)"
  [ -n "$network" ] || fail "no Docker network for $vpc: the emulator has not created the VPC"
  docker network inspect "$network" -f '{{(index .IPAM.Config 0).Gateway}}'
}

compose() { docker compose --project-directory "$STATE" -f "$COMPOSE" --env-file "$STATE/env" "$@"; }

case "${1:-}" in
  address)
    address="$(gateway)"
    printf 'http://%s:%s\n' "$address" "$PORT"
    ;;

  up)
    : "${SANDBOX_GATEWAY_PUBKEY:?SANDBOX_GATEWAY_PUBKEY is not set}"
    image="${SANDBOX_IMAGE:-}"
    if [ -z "$image" ]; then
      env_file="$ROOT/infra/helmfile/environments/staging.yaml"
      tag="$(yq '.platformImages.tags."loop-sandbox" // ""' "$env_file")"
      [ -n "$tag" ] || fail "platformImages.tags.loop-sandbox is not set in $env_file"
      image="$(yq '.platformImages.registry' "$env_file" | sed 's:/*$::')/loop-sandbox:$tag"
    fi
    address="$(gateway)"
    bind="$address:$PORT"

    # The database password and the key that encrypts tenant secrets are made once and
    # live as long as the database's volume: `down` removes the three together.
    install -d -m 0700 "$STATE"
    if [ ! -f "$STATE/secrets" ]; then
      (umask 077; printf 'SANDBOX_DB_PASSWORD=%s\nSANDBOX_SECRETS_KEY=%s\n' \
        "$(openssl rand -hex 24)" "$(openssl rand -hex 32)" >"$STATE/secrets")
    fi
    (umask 077; { cat "$STATE/secrets"
      printf 'SANDBOX_IMAGE=%s\nSANDBOX_GATEWAY_PUBKEY=%s\nSANDBOX_BIND=%s\n' "$image" "$SANDBOX_GATEWAY_PUBKEY" "$bind"
    } >"$STATE/env")

    compose up -d --wait --quiet-pull
    for _ in $(seq 1 60); do
      if curl -fsS --max-time 3 -o /dev/null "http://$bind/v1/readyz"; then
        for warm in $WARM_IMAGES; do docker pull --quiet "$warm" >/dev/null || log "warning: could not pull $warm"; done
        log "sandbox API $image ready on $bind"
        exit 0
      fi
      sleep 2
    done
    compose logs --tail 40 api >&2 || true
    fail "the sandbox API did not become ready on $bind"
    ;;

  down)
    # Sandboxes and their guards first: they are containers of the driver, not of the
    # compose project, each with the volume that holds its agent.
    mapfile -t containers < <(docker ps -aq --filter label=loopsandbox.managed=true)
    if [ "${#containers[@]}" -gt 0 ]; then docker rm -f -v "${containers[@]}" >/dev/null; fi
    if [ -f "$STATE/env" ]; then compose down --volumes --remove-orphans >/dev/null 2>&1 || true; fi
    # Without its env file compose cannot read the project; remove what is left by name.
    mapfile -t leftover < <(docker ps -aq --filter label=com.docker.compose.project=loop-sandbox)
    if [ "${#leftover[@]}" -gt 0 ]; then docker rm -f -v "${leftover[@]}" >/dev/null; fi
    docker volume rm -f loop-sandbox_postgres-data >/dev/null 2>&1 || true
    rm -rf "$STATE"
    log "sandbox service removed"
    ;;

  *)
    echo "usage: $0 up|down|address" >&2
    exit 2
    ;;
esac
