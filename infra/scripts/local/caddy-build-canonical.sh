#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# caddy-build-canonical.sh — T1.1 of plan
# kratos-self-service-rate-limit-systemic v1.1 (EC-1 + EC-6 absorbed).
# Canonical xcaddy builder producing a SINGLE Caddy binary that bundles
# BOTH:
#
#   1. github.com/caddy-dns/cloudflare        — DNS-01 challenge for
#                                                 *.preview-dev.getloopscale.app
#                                                 wildcard cert (J-23 #68);
#   2. github.com/mholt/caddy-ratelimit@<sha> — edge rate-limit for
#                                                 Red Team #68 (login) +
#                                                 #89 (recovery email-bomb)
#                                                 + #43 (register defense).
#
# EC-1 absorption: a separate single-module builder would OVERWRITE the
# existing canonical binary and DROP cloudflare DNS support → wildcard
# cert renewal would break silently. Canonical fix: one builder, both
# modules. The legacy `caddy-build-with-cloudflare.sh` script is now a
# thin shim that execs this canonical builder.
#
# EC-6 absorption: caddy-ratelimit is pinned to commit SHA
# 5625512f24f6f59d6f64fb3aafe5eecff0b286db (HEAD verified on-disk at
# .claude/knowledge-base/references/caddy-ratelimit/, dated 2026-06-12)
# — NOT HEAD (which is mutable). Upstream has not yet shipped a semver
# tag; commit SHA is the canonical pin until v0.x lands.
#
# Idempotent. Skips the build when:
#   - $CADDY_OUTPUT binary exists AND
#   - `caddy list-modules` reports BOTH `dns.providers.cloudflare`
#     AND `http.handlers.rate_limit` AND
#   - $CADDY_FORCE_REBUILD is empty
#
# Operator pre-requisites:
#   - Go 1.21+ on PATH (`go version`)
#   - Network access to proxy.golang.org + github.com
#   - $CF_API_TOKEN env var with Zone:DNS:Edit scope on getloopscale.com
#     (NOT used during build — Caddy reads it at runtime).
#
# Usage:
#   bash infra/scripts/local/caddy-build-canonical.sh           # build only if missing module(s)
#   CADDY_FORCE_REBUILD=1 bash …                                # always rebuild
#   CADDY_OUTPUT=/usr/local/bin/caddy bash …                    # install path (default)

set -euo pipefail

# Pinned versions — bump intentionally; never floating.
CADDY_VERSION="${CADDY_VERSION:-v2.11.3}"
XCADDY_VERSION="${XCADDY_VERSION:-v0.4.6}"
CADDY_CLOUDFLARE_PKG="github.com/caddy-dns/cloudflare"
# EC-6: explicit commit SHA pin — caddy-ratelimit has no semver tag yet.
# Verify on-disk: cd .claude/knowledge-base/references/caddy-ratelimit && git log -1
# Canonical literal: mholt/caddy-ratelimit@5625512f24f6f59d6f64fb3aafe5eecff0b286db
CADDY_RATELIMIT_SHA="5625512f24f6f59d6f64fb3aafe5eecff0b286db"
CADDY_RATELIMIT_PKG="github.com/mholt/caddy-ratelimit@${CADDY_RATELIMIT_SHA}"
CADDY_OUTPUT="${CADDY_OUTPUT:-/usr/local/bin/caddy}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: required binary '$1' not found on PATH" >&2
    exit 2
  }
}

# Idempotency gate: if the binary at $CADDY_OUTPUT exists AND already
# bundles BOTH required modules, skip rebuild. Check the output path
# DIRECTLY (not via PATH lookup) so non-canonical install paths still
# short-circuit correctly. Override with CADDY_FORCE_REBUILD=1.
if [[ -z "${CADDY_FORCE_REBUILD:-}" ]] && [[ -x "$CADDY_OUTPUT" ]]; then
  HAVE_CF=0
  HAVE_RL=0
  if "$CADDY_OUTPUT" list-modules 2>/dev/null | grep -q '^dns\.providers\.cloudflare$'; then
    HAVE_CF=1
  fi
  if "$CADDY_OUTPUT" list-modules 2>/dev/null | grep -q '^http\.handlers\.rate_limit$'; then
    HAVE_RL=1
  fi
  if [[ "$HAVE_CF" -eq 1 && "$HAVE_RL" -eq 1 ]]; then
    echo "OK: $CADDY_OUTPUT already bundles both modules — skipping rebuild."
    "$CADDY_OUTPUT" version
    exit 0
  fi
  if [[ "$HAVE_CF" -eq 1 && "$HAVE_RL" -eq 0 ]]; then
    echo "Caddy installed at $CADDY_OUTPUT but missing rate_limit module — rebuilding."
  elif [[ "$HAVE_CF" -eq 0 && "$HAVE_RL" -eq 1 ]]; then
    echo "Caddy installed at $CADDY_OUTPUT but missing cloudflare module — rebuilding."
  else
    echo "Caddy installed at $CADDY_OUTPUT but missing BOTH modules — rebuilding."
  fi
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

echo "→ Building Caddy $CADDY_VERSION with:"
echo "    $CADDY_CLOUDFLARE_PKG"
echo "    $CADDY_RATELIMIT_PKG"
echo "  output: $BUILD_TMP/caddy (will be installed to $CADDY_OUTPUT)"
xcaddy build "$CADDY_VERSION" \
  --with "$CADDY_CLOUDFLARE_PKG" \
  --with "$CADDY_RATELIMIT_PKG" \
  --output "$BUILD_TMP/caddy" 2>&1 | tail -20

# Verify BOTH modules are wired before installing.
MISSING=""
if ! "$BUILD_TMP/caddy" list-modules 2>/dev/null | grep -q '^dns\.providers\.cloudflare$'; then
  MISSING="$MISSING dns.providers.cloudflare"
fi
if ! "$BUILD_TMP/caddy" list-modules 2>/dev/null | grep -q '^http\.handlers\.rate_limit$'; then
  MISSING="$MISSING http.handlers.rate_limit"
fi
if [[ -n "$MISSING" ]]; then
  echo "ERROR: build succeeded but missing module(s):$MISSING — aborting install" >&2
  "$BUILD_TMP/caddy" list-modules | grep -iE 'dns|rate' >&2 || true
  exit 3
fi

echo "→ Installing $BUILD_TMP/caddy → $CADDY_OUTPUT"
if [[ -w "$(dirname "$CADDY_OUTPUT")" ]]; then
  install -m 0755 "$BUILD_TMP/caddy" "$CADDY_OUTPUT"
else
  sudo install -m 0755 "$BUILD_TMP/caddy" "$CADDY_OUTPUT"
fi

echo "OK: $CADDY_OUTPUT is now Caddy $CADDY_VERSION + caddy-dns/cloudflare + mholt/caddy-ratelimit@${CADDY_RATELIMIT_SHA:0:7}."
"$CADDY_OUTPUT" version
echo
"$CADDY_OUTPUT" list-modules | grep -E 'dns\.providers\.cloudflare|http\.handlers\.rate_limit'
echo
echo "Next:"
echo "  1. export CF_API_TOKEN=<token-with-Zone-DNS-Edit-on-getloopscale.com>"
echo "  2. \`task cloud:caddy:up\` to reload the auth-dev + preview-dev blocks"
echo "  3. T1.4/T1.5/T3.5 LIVE bash tests verify the rate_limit zones engage."
