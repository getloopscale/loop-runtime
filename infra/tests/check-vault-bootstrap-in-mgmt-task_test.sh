#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-vault-bootstrap-in-mgmt-task_test.sh — iter 24 hermetic
# structural test for the `task k3d:vault:bootstrap-in-mgmt` shortcut
# (Option (c) of cleanroom iter 23 cross-cluster reachability Open
# Question, per docs/operations/openbao-transit-cutover-runbook.md).
#
# Locks the contract that the shortcut canonical-mirrors the upstream
# `task k3d:vault:bootstrap` task with EXACTLY two well-known differences:
#   1. context k3d-loopzero-vault → k3d-loopzero-mgmt (everywhere)
#   2. pre-flight error message references `task setup` instead of `task k3d:vault:create`
#
# Defense against drift: if upstream `task k3d:vault:bootstrap` adds a
# new helmfile selector or kubectl wait step, this test fails so we
# remember to mirror the change.
#
# Hermetic — no kubectl, no helmfile install. Pure grep on taskfiles/k3d.yml.

set -euo pipefail

TASKFILE="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/taskfiles/k3d.yml"
TMPDIR=$(mktemp -d -t vault-bootstrap-mgmt-XXXXXX)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

PASS=0
FAIL=0
assert() {
  local desc="$1" cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "FAIL: $desc"
    FAIL=$((FAIL+1))
  fi
}

# Extract the canonical upstream task body + the new shortcut body.
awk '/^  k3d:vault:bootstrap:$/,/^  k3d:vault:bootstrap-in-mgmt:$/' "$TASKFILE" > "$TMPDIR/upstream.txt"
awk '/^  k3d:vault:bootstrap-in-mgmt:$/,/^  k3d:vault:status-in-mgmt:$/' "$TASKFILE" > "$TMPDIR/shortcut.txt"

UPSTREAM_HELMFILES=$(grep -c "helmfile -f " "$TMPDIR/upstream.txt" || true)
SHORTCUT_HELMFILES=$(grep -c "helmfile -f " "$TMPDIR/shortcut.txt" || true)
UPSTREAM_WAITS=$(grep -c "kubectl --context .* wait" "$TMPDIR/upstream.txt" || true)
SHORTCUT_WAITS=$(grep -c "kubectl --context .* wait" "$TMPDIR/shortcut.txt" || true)

# A — task body is present + non-empty
assert "A1 task k3d:vault:bootstrap-in-mgmt registered" \
  "grep -q '^  k3d:vault:bootstrap-in-mgmt:$' '$TASKFILE'"

assert "A2 task k3d:vault:status-in-mgmt sibling registered" \
  "grep -q '^  k3d:vault:status-in-mgmt:$' '$TASKFILE'"

# B — helmfile selector chain mirrors upstream (no drift between sibling tasks)
assert "B1 shortcut has same number of helmfile syncs as upstream ($UPSTREAM_HELMFILES = $SHORTCUT_HELMFILES)" \
  "[ '$UPSTREAM_HELMFILES' = '$SHORTCUT_HELMFILES' ]"

assert "B2 shortcut has same number of kubectl waits as upstream ($UPSTREAM_WAITS = $SHORTCUT_WAITS)" \
  "[ '$UPSTREAM_WAITS' = '$SHORTCUT_WAITS' ]"

for selector in cert-manager cluster-issuer openbao-cert openbao; do
  assert "B-${selector}: shortcut syncs --selector name=${selector}" \
    "grep -q 'selector name=${selector} sync' '$TMPDIR/shortcut.txt'"
done

# C — context swap is total (no stray loopzero-vault references in shortcut body)
assert "C1 shortcut body uses k3d-loopzero-mgmt context everywhere" \
  "grep -q 'kube-context k3d-loopzero-mgmt' '$TMPDIR/shortcut.txt'"

assert "C2 shortcut body does NOT reference k3d-loopzero-vault (no copy-paste drift)" \
  "! grep -q 'k3d-loopzero-vault' '$TMPDIR/shortcut.txt'"

# D — pre-flight error message references task setup (not task k3d:vault:create)
assert "D1 shortcut pre-flight references 'task setup'" \
  "grep -q 'task setup' '$TMPDIR/shortcut.txt'"

assert "D2 shortcut pre-flight does NOT reference 'task k3d:vault:create' (mgmt comes from task setup)" \
  "! grep -q 'task k3d:vault:create' '$TMPDIR/shortcut.txt'"

# E — desc block cites the runbook Open Question + the iter 23 audit
assert "E1 desc cites docs/operations/openbao-transit-cutover-runbook.md" \
  "grep -q 'openbao-transit-cutover-runbook.md' '$TMPDIR/shortcut.txt'"

assert "E2 desc cites option (c) iter 23" \
  "grep -q 'Option (c)' '$TMPDIR/shortcut.txt'"

assert "E3 desc names the trade-off (loses control-plane/secret-storage isolation)" \
  "grep -q 'isolation' '$TMPDIR/shortcut.txt'"

assert "E4 desc warns prod must use canonical k3d:vault:bootstrap" \
  "grep -q 'prod cutover MUST' '$TMPDIR/shortcut.txt'"

# F — anti-patterns
assert "F1 no Co-Authored-By trailer references" \
  "! grep -qi 'Co-Authored-By' '$TMPDIR/shortcut.txt'"

assert "F2 no --no-verify flag in cmds" \
  "! grep -q -- '--no-verify' '$TMPDIR/shortcut.txt'"

# Summary
echo ""
echo "vault-bootstrap-in-mgmt task contract: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot dir (preserved): $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
