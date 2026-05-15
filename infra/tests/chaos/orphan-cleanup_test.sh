#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan T3.3 — hermetic tests for cleanup-orphan-chaos.sh.
# Uses a stub kubectl that returns canned output to validate the script's
# decision logic without needing a real K8s cluster.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../scripts" && pwd)/cleanup-orphan-chaos.sh"

PASS=0
FAIL=0
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Stub kubectl: returns content of a fixture file based on the resource kind
# in the args. Records every invocation for assertion.
make_stub_kubectl() {
  local fixture_dir="$1"
  local stub="$TMPDIR/kubectl-stub.sh"
  cat > "$stub" <<EOF
#!/usr/bin/env bash
# Stub kubectl — emits canned data based on resource kind argument.
fixture_dir='$fixture_dir'
case "\$1" in
  get)
    kind=\$2
    if [ -f "\$fixture_dir/\$kind.tsv" ]; then
      cat "\$fixture_dir/\$kind.tsv"
    fi
    ;;
  patch)
    # Record patch attempts to a file for assertion
    echo "\$@" >> "$TMPDIR/patches.log"
    ;;
  *)
    ;;
esac
EOF
  chmod +x "$stub"
  echo "$stub"
}

run_test() {
  local name="$1"
  local fn="$2"
  if "$fn"; then
    echo "PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name"
    FAIL=$((FAIL + 1))
  fi
}

# Test 1: stuck finalizer (deletionTimestamp older than threshold) → patches
test_orphan_cleanup_patches_finalizer() {
  local fix="$TMPDIR/test1"
  mkdir -p "$fix"

  # NetworkChaos with deletionTimestamp 10min ago + 1 finalizer
  local old_ts
  old_ts=$(date -u -d "10 minutes ago" +%Y-%m-%dT%H:%M:%SZ)
  printf "%s\t%s\t%s\t%s\n" "loopzero-system" "stuck-chaos" "$old_ts" "[chaos-mesh/records]" \
    > "$fix/networkchaos.tsv"
  echo "" > "$fix/podchaos.tsv"
  echo "" > "$fix/iochaos.tsv"
  echo "" > "$fix/stresschaos.tsv"

  local kubectl_stub
  kubectl_stub=$(make_stub_kubectl "$fix")
  rm -f "$TMPDIR/patches.log"

  KUBECTL="$kubectl_stub" THRESHOLD_MIN=5 bash "$SCRIPT" > "$TMPDIR/out1.log" 2>&1 || return 1

  if ! grep -q "patched=1" "$TMPDIR/out1.log"; then
    echo "  expected patched=1, got:" >&2
    cat "$TMPDIR/out1.log" >&2
    return 1
  fi
  if ! grep -q "stuck-chaos" "$TMPDIR/patches.log"; then
    echo "  expected stuck-chaos to be patched, but no patches recorded" >&2
    return 1
  fi
  return 0
}

# Test 2: chaos resource WITHOUT deletionTimestamp → no patch
test_orphan_cleanup_no_op_on_active_resource() {
  local fix="$TMPDIR/test2"
  mkdir -p "$fix"

  # NetworkChaos with NO deletionTimestamp (actively running)
  printf "%s\t%s\t\t\n" "loopzero-system" "active-chaos" \
    > "$fix/networkchaos.tsv"
  echo "" > "$fix/podchaos.tsv"
  echo "" > "$fix/iochaos.tsv"
  echo "" > "$fix/stresschaos.tsv"

  local kubectl_stub
  kubectl_stub=$(make_stub_kubectl "$fix")
  rm -f "$TMPDIR/patches.log"

  KUBECTL="$kubectl_stub" THRESHOLD_MIN=5 bash "$SCRIPT" > "$TMPDIR/out2.log" 2>&1 || return 1

  if ! grep -q "patched=0" "$TMPDIR/out2.log"; then
    echo "  expected patched=0, got:" >&2
    cat "$TMPDIR/out2.log" >&2
    return 1
  fi
  if [ -f "$TMPDIR/patches.log" ]; then
    echo "  active chaos should NOT have been patched" >&2
    return 1
  fi
  return 0
}

# Test 3: chaos resource with deletionTimestamp YOUNGER than threshold → skipped
test_orphan_cleanup_within_threshold_skip() {
  local fix="$TMPDIR/test3"
  mkdir -p "$fix"

  # NetworkChaos with deletionTimestamp 2min ago (under 5min threshold)
  local young_ts
  young_ts=$(date -u -d "2 minutes ago" +%Y-%m-%dT%H:%M:%SZ)
  printf "%s\t%s\t%s\t%s\n" "loopzero-system" "young-chaos" "$young_ts" "[finalizer]" \
    > "$fix/networkchaos.tsv"
  echo "" > "$fix/podchaos.tsv"
  echo "" > "$fix/iochaos.tsv"
  echo "" > "$fix/stresschaos.tsv"

  local kubectl_stub
  kubectl_stub=$(make_stub_kubectl "$fix")
  rm -f "$TMPDIR/patches.log"

  KUBECTL="$kubectl_stub" THRESHOLD_MIN=5 bash "$SCRIPT" > "$TMPDIR/out3.log" 2>&1 || return 1

  if ! grep -q "patched=0" "$TMPDIR/out3.log"; then
    echo "  expected patched=0 (within threshold), got:" >&2
    cat "$TMPDIR/out3.log" >&2
    return 1
  fi
  return 0
}

run_test "patches stuck finalizer (10min old)" test_orphan_cleanup_patches_finalizer
run_test "no-op on active chaos (no deletionTimestamp)" test_orphan_cleanup_no_op_on_active_resource
run_test "skips chaos within threshold (2min < 5min)" test_orphan_cleanup_within_threshold_skip

echo ""
echo "RESULTS: $PASS pass, $FAIL fail"
[ "$FAIL" -eq 0 ]
