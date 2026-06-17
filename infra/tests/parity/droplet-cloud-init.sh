#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY / T6.1 — droplet cloud-init.
#
# Idempotent bootstrap that runs on first boot of the parity droplet:
#   1) install docker, kubectl, helm, helmfile, helm-diff plugin, k3d, doctl, jq
#   2) install loopzero CLI from npm (latest tag)
#   3) clone the loopzero repo into /opt/loopzero
#   4) emit /var/log/loopzero-parity-ready when complete (handshake with caller)
#
# Runs as root. Logs to /var/log/cloud-init-output.log. Caller polls for the
# ready file via SSH.
#
# Versions pinned via env (defaults: stable). Override only when validating
# upstream upgrades.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -euxo pipefail

LOOPZERO_REF="${LOOPZERO_REF:-develop}"
HELM_VERSION="${HELM_VERSION:-v3.16.4}"
HELMFILE_VERSION="${HELMFILE_VERSION:-v1.0.0-rc.10}"
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.32.13}"
K3D_VERSION="${K3D_VERSION:-v5.7.5}"

export DEBIAN_FRONTEND=noninteractive

apt-get update -y
apt-get install -y curl ca-certificates gnupg jq git make build-essential

# 1) Docker (engine + cli + buildx).
if ! command -v docker >/dev/null 2>&1; then
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
  apt-get update -y
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin
fi

# 2) kubectl.
curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o /usr/local/bin/kubectl
chmod +x /usr/local/bin/kubectl

# 3) helm + helm-diff plugin.
curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" | tar -xz -C /tmp
install -m 0755 /tmp/linux-amd64/helm /usr/local/bin/helm
helm plugin install https://github.com/databus23/helm-diff || true

# 4) helmfile.
curl -fsSL "https://github.com/helmfile/helmfile/releases/download/${HELMFILE_VERSION}/helmfile_${HELMFILE_VERSION#v}_linux_amd64.tar.gz" | tar -xz -C /tmp
install -m 0755 /tmp/helmfile /usr/local/bin/helmfile

# 5) k3d.
curl -fsSL "https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh" | TAG="$K3D_VERSION" bash

# 6) doctl (for parity scripts that use it).
DOCTL_VERSION=$(curl -fsSL https://api.github.com/repos/digitalocean/doctl/releases/latest | jq -r .tag_name | sed 's/^v//')
curl -fsSL "https://github.com/digitalocean/doctl/releases/download/v${DOCTL_VERSION}/doctl-${DOCTL_VERSION}-linux-amd64.tar.gz" | tar -xz -C /tmp
install -m 0755 /tmp/doctl /usr/local/bin/doctl

# 7) Node.js (for loopzero CLI npm install).
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt-get install -y nodejs

# 8) LoopZero repo + CLI.
git clone --branch "$LOOPZERO_REF" --depth 1 https://github.com/getloopscale/loopzero.git /opt/loopzero
cd /opt/loopzero/cli
npm ci
npm run build
npm link

# 9) Sanity probe.
docker --version
kubectl version --client
helm version --short
helmfile version
k3d version
node --version
loopzero --version

# 10) Handshake — caller polls this file.
date -u +%FT%TZ > /var/log/loopzero-parity-ready
