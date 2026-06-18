#!/usr/bin/env bash
# Hermetic test — install-config-contract Slice 2 T1.1 + T2.1.
# Reloader annotation present iff enabled + chart README documents the install surface.
# NO kubectl, NO network.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART="$ROOT/charts/loopzero-cloud"; README="$CHART/README.md"
fail=0; pass=0; ok(){ pass=$((pass+1)); }; no(){ fail=$((fail+1)); echo "  FAIL: $1"; }

# T1.1 — annotation gated by reloader.enabled
[ "$(helm template "$CHART" --set reloader.enabled=true 2>/dev/null | grep -c 'reloader.stakater.com/auto')" -ge 1 ] && ok || no "annotation absent when reloader.enabled=true"
[ "$(helm template "$CHART" 2>/dev/null | grep -c 'reloader.stakater.com')" -eq 0 ] && ok || no "reloader annotation leaked into default render"
helm template "$CHART" --set reloader.enabled=true >/dev/null 2>&1 && ok || no "enabled render does not pass schema"

# T2.1 — README documents the install surface
[ -f "$README" ] && ok || no "README.md missing"
for tok in 'existingSecret' 'values.example.yaml' 'SECRETS.md' 'examples/' 'reloader.enabled'; do
  grep -q "$tok" "$README" && ok || no "README missing reference: $tok"
done
# README links resolve
for f in SECRETS.md values.example.yaml examples/eso-openbao.yaml; do
  [ -f "$CHART/$f" ] && ok || no "README links a missing file: $f"
done

echo "loopzero-cloud-reloader: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
