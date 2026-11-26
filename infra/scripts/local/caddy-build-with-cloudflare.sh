#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# DEPRECATION SHIM — superseded by `caddy-build-canonical.sh`
# per T1.1 EC-1 of plan kratos-self-service-rate-limit-systemic v1.1.
# The canonical builder bundles BOTH caddy-dns/cloudflare AND
# mholt/caddy-ratelimit in a single binary; calling either module
# alone would overwrite the other.
#
# This shim preserves the existing operator path
# `bash infra/scripts/local/caddy-build-with-cloudflare.sh`
# (referenced by allowlist + memory + runbooks) by transparently
# exec'ing the canonical builder.

set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$HERE/caddy-build-canonical.sh" "$@"
