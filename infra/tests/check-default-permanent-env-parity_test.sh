#!/bin/bash
# Hermetic test: assert Go DefaultPermanentEnv == TS DEFAULT_PERMANENT_ENV.
# Per ADR D3 EC-3 (decisions/2026-06-11-pkg-domain-default-permanent-env.md):
# prevents Go↔TS constant drift that would silently re-introduce J-22↔J-10 break.
#
# Wired in: task quality:gates (per rules/shell-scripts-allowlist.md)
# Allowlist entry: infra/tests/check-default-permanent-env-parity_test.sh — T1.2 NEW

set -uo pipefail

WORKTREE_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$WORKTREE_ROOT"

GO_FILE="pkg/domain/constants.go"
TS_FILE="cloud/dashboard/src/lib/domain.ts"

if [ ! -f "$GO_FILE" ]; then
    echo "❌ FAIL: $GO_FILE missing"
    exit 1
fi
if [ ! -f "$TS_FILE" ]; then
    echo "❌ FAIL: $TS_FILE missing"
    exit 1
fi

GO_VALUE=$(grep -oE 'DefaultPermanentEnv\s*=\s*"[^"]*"' "$GO_FILE" | grep -oE '"[^"]*"' | tr -d '"')
TS_VALUE=$(grep -oE "DEFAULT_PERMANENT_ENV\s*=\s*'[^']*'" "$TS_FILE" | grep -oE "'[^']*'" | tr -d "'")

if [ -z "$GO_VALUE" ]; then
    echo "❌ FAIL: could not extract Go DefaultPermanentEnv value from $GO_FILE"
    exit 1
fi
if [ -z "$TS_VALUE" ]; then
    echo "❌ FAIL: could not extract TS DEFAULT_PERMANENT_ENV value from $TS_FILE"
    exit 1
fi

if [ "$GO_VALUE" = "$TS_VALUE" ]; then
    echo "✅ default-permanent-env-parity: Go=\"$GO_VALUE\" == TS=\"$TS_VALUE\""
    exit 0
else
    echo "❌ FAIL: Go DefaultPermanentEnv=\"$GO_VALUE\" != TS DEFAULT_PERMANENT_ENV=\"$TS_VALUE\""
    echo "   Per ADR D3 (decisions/2026-06-11-pkg-domain-default-permanent-env.md): values MUST match"
    echo "   Fix: align $TS_FILE to the Go canonical value (Go is source of truth)"
    exit 1
fi
