#!/usr/bin/env bash
# setup-host-inotify-limits.sh — persist kernel inotify limits required for k3d multi-cluster topology.
#
# Why this exists (Finding 2026-05-13 — k3d loop-runtime-cell-0 agent timeout):
#
# LoopRuntime's local dev topology runs 4 k3d clusters (mgmt + build + workload + runtime-2)
# simultaneously. Each k3s server + agent runs an embedded containerd which uses
# fsnotify watchers for the CRI plugin's CNI conf monitor, image registry watcher,
# and snapshotter sync. Each cluster consumes ~5-10 inotify instances per user.
#
# Linux default `fs.inotify.max_user_instances = 128` (kernel default since the 3.x era)
# is insufficient for 4 k3d clusters. Containerd fails to load the CRI plugin with:
#
#   "failed to create cni conf monitor for default: failed to create fsnotify
#    watcher: too many open files"
#
# Symptom: `k3d cluster create` times out at the agent registration step
# (`Successfully registered node` never logged) — typically the 3rd cluster
# (loop-runtime-cell-0) hits the wall after mgmt+build already consumed instances.
#
# CTO mandate 2026-05-13 Rule 0 (local fidedigno ao prod) + Rule 6 (zero workaround):
# fix the kernel limit systematically, persist across reboots, and pre-flight gate
# `task clusters:create` so the failure mode never recurs silently.
#
# Idempotency: writing the same /etc/sysctl.d/99-loop-runtime-cleanroom.conf is a no-op if
# content matches; sysctl --system re-applies all .conf files (cheap).
#
# Usage:
#   sudo bash infra/scripts/setup-host-inotify-limits.sh
#
# Exit codes:
#   0 — limits are persisted AND active (>= required threshold)
#   1 — script must be run as root (re-invoke with sudo)
#   2 — sysctl --system failed (kernel rejected the values)

set -euo pipefail

# Required minimums (rationale in finding doc):
#   max_user_instances: 4 clusters × ~10 instances each = 40 baseline. With other
#     dev processes (tilt, port-forwards, VS Code remote, browser, etc.) easily
#     1.5× margin. Set to 1024 — modern container distro default.
#   max_user_watches: 4 clusters × 50k watchers (containerd CNI conf monitor +
#     image registry watch + snapshotter sync) = 200k. Default 124464 is below
#     this. Set to 524288 (modern container distro default).
REQUIRED_INSTANCES=1024
REQUIRED_WATCHES=524288

CONF_FILE="/etc/sysctl.d/99-loop-runtime-cleanroom.conf"

if [[ "$EUID" -ne 0 ]]; then
  echo "ERROR: this script must run as root. Re-invoke with: sudo bash $0" >&2
  exit 1
fi

# Backup any pre-existing file with a non-conflicting marker so we don't
# silently overwrite a different policy.
if [[ -f "$CONF_FILE" ]] && ! grep -q "loop-runtime-cleanroom" "$CONF_FILE" 2>/dev/null; then
  cp -p "$CONF_FILE" "${CONF_FILE}.bak.$(date +%Y%m%d-%H%M%S)"
  echo "Backed up pre-existing $CONF_FILE"
fi

cat > "$CONF_FILE" <<EOF
# loop-runtime-cleanroom: kernel inotify limits for 4 k3d clusters (mgmt + build + workload + runtime-2)
# Managed by infra/scripts/setup-host-inotify-limits.sh — do not edit by hand.
# Rationale: see .claude/knowledge-base/concepts/operations/finding-2026-05-13-k3d-agent-timeout.md
fs.inotify.max_user_instances = $REQUIRED_INSTANCES
fs.inotify.max_user_watches   = $REQUIRED_WATCHES
EOF
echo "Wrote $CONF_FILE"

# Apply now (rather than only at next reboot)
if ! sysctl --system >/dev/null; then
  echo "ERROR: sysctl --system failed" >&2
  exit 2
fi

# Verify the kernel accepted the values
ACTUAL_INSTANCES=$(sysctl -n fs.inotify.max_user_instances)
ACTUAL_WATCHES=$(sysctl -n fs.inotify.max_user_watches)

if [[ "$ACTUAL_INSTANCES" -lt "$REQUIRED_INSTANCES" ]]; then
  echo "ERROR: fs.inotify.max_user_instances=$ACTUAL_INSTANCES is still below required $REQUIRED_INSTANCES" >&2
  exit 2
fi
if [[ "$ACTUAL_WATCHES" -lt "$REQUIRED_WATCHES" ]]; then
  echo "ERROR: fs.inotify.max_user_watches=$ACTUAL_WATCHES is still below required $REQUIRED_WATCHES" >&2
  exit 2
fi

echo ""
echo "=== Active kernel limits ==="
echo "fs.inotify.max_user_instances = $ACTUAL_INSTANCES"
echo "fs.inotify.max_user_watches   = $ACTUAL_WATCHES"
echo ""
echo "Persisted in $CONF_FILE — survives reboots."
echo "Run \`task setup\` or \`task dogfood:cleanroom\` to validate."
