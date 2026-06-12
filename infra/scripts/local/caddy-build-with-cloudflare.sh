#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# caddy-build-with-cloudflare.sh — produce a Caddy binary with the
# caddy-dns/cloudflare module baked in, so the wildcard preview-dev
# block in infra/local/Caddyfile can issue TLS via Let's Encrypt
# DNS-01 challenge instead of HTTP-01.
#
# J-23 #68 of 4-task batch 2026-06-07 (sponsor verbatim "TLS para
# *.preview-dev.loopzero.example"). HTTP-01 against a wildcard hostname hits
# LE's "50 certs/week per Registered Domain" rate limit within a single
# dogfood session — DNS-01 is the only path that scales to per-PR
# previews. The official Caddy build ships without DNS providers; the
# canonical solution is `xcaddy build` with the cloudflare module pinned.
#
# Idempotent. Skips the build when:
#   - `caddy` is already on PATH AND
#   - `caddy list-modules` reports `dns.providers.cloudflare` AND
#   - $CADDY_FORCE_REBUILD is empty
#
# Operator pre-requisites:
#   - Go 1.21+ on PATH (`go version`)
#   - Network access to proxy.golang.org + github.com
#   - `CF_API_TOKEN` env var with Zone:DNS:Edit scope on loopzero.example
#     (NOT used during build — Caddy reads it at runtime). Verify with
#     `curl -fsS https://api.cloudflare.com/client/v4/user/tokens/verify \
#         -H "Authorization: Bearer $CF_API_TOKEN"`.
#
# Usage:
#   bash infra/scripts/local/caddy-build-with-cloudflare.sh           # build only if missing module
#   CADDY_FORCE_REBUILD=1 bash …                                       # always rebuild
#   CADDY_OUTPUT=/usr/local/bin/caddy bash …                           # install path (default)

set -euo pipefail

# Pinned versions — bump intentionally; never floating.
CADDY_VERSION="${CADDY_VERSION:-v2.11.3}"
CADDY_CLOUDFLARE_PKG="github.com/caddy-dns/cloudflare"
CADDY_OUTPUT="${CADDY_OUTPUT:-/usr/local/bin/caddy}"
XCADDY_VERSION="${XCADDY_VERSION:-v0.4.6}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: required binary '$1' not found on PATH" >&2
    exit 2
  }
}

# Idempotency gate: if the binary at $CADDY_OUTPUT exists AND already
# bundles dns.providers.cloudflare, skip rebuild. Check the output path
# DIRECTLY (not via PATH lookup) so non-canonical install paths still
# short-circuit correctly. Override with CADDY_FORCE_REBUILD=1.
if [[ -z "${CADDY_FORCE_REBUILD:-}" ]] && [[ -x "$CADDY_OUTPUT" ]]; then
  if "$CADDY_OUTPUT" list-modules 2>/dev/null | grep -q '^dns\.providers\.cloudflare$'; then
    echo "OK: $CADDY_OUTPUT already has dns.providers.cloudflare module."
    "$CADDY_OUTPUT" version
    exit 0
  fi
  echo "Caddy installed at $CADDY_OUTPUT but missing cloudflare module — rebuilding."
fi

require go
echo "→ Installing xcaddy $XCADDY_VERSION"
GOBIN="${GOBIN:-$HOME/go/bin}"
mkdir -p "$GOBIN"
GO111MODULE=on go install "github.com/caddyserver/xcaddy/cmd/xcaddy@${XCADDY_VERSION}" 2>&1 | sed 's/^/    /'

if ! command -v xcaddy >/dev/null 2>&1; then
  if [[ -x "$GOBIN/xcaddy" ]]; then
    export PATH="$GOBIN:$PATH"
  else
    echo "ERROR: xcaddy install succeeded but binary not on PATH" >&2
    exit 2
  fi
fi

BUILD_TMP="$(mktemp -d -t caddy-build-XXXXXX)"
trap 'rm -rf "$BUILD_TMP"' EXIT INT TERM

echo "→ Building Caddy $CADDY_VERSION with $CADDY_CLOUDFLARE_PKG"
echo "  output: $BUILD_TMP/caddy (will be installed to $CADDY_OUTPUT)"
xcaddy build "$CADDY_VERSION" \
  --with "$CADDY_CLOUDFLARE_PKG" \
  --output "$BUILD_TMP/caddy" 2>&1 | tail -15

# Verify the module is wired before installing.
if ! "$BUILD_TMP/caddy" list-modules 2>/dev/null | grep -q '^dns\.providers\.cloudflare$'; then
  echo "ERROR: build succeeded but dns.providers.cloudflare not present — aborting install" >&2
  "$BUILD_TMP/caddy" list-modules | grep -i dns >&2 || true
  exit 3
fi

echo "→ Installing $BUILD_TMP/caddy → $CADDY_OUTPUT"
if [[ -w "$(dirname "$CADDY_OUTPUT")" ]]; then
  install -m 0755 "$BUILD_TMP/caddy" "$CADDY_OUTPUT"
else
  sudo install -m 0755 "$BUILD_TMP/caddy" "$CADDY_OUTPUT"
fi

echo "OK: $CADDY_OUTPUT is now Caddy $CADDY_VERSION + caddy-dns/cloudflare."
"$CADDY_OUTPUT" version
"$CADDY_OUTPUT" list-modules | grep dns.providers
echo
echo "Next: export CF_API_TOKEN=<token-with-Zone-DNS-Edit-on-loopzero.example>"
echo "      then run \`task cloud:caddy:up\` to reload the wildcard block."
