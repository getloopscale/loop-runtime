// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';

export interface ValidationResult {
  valid: boolean;
  checks: { name: string; ok: boolean; detail: string }[];
}

/**
 * Validate that the target cluster is ready for LoopZero installation.
 */
export function validateCluster(): ValidationResult {
  const checks: ValidationResult['checks'] = [];

  // kubectl connectivity
  try {
    const version = execSync('kubectl version --client -o json', { encoding: 'utf-8' });
    const v = JSON.parse(version);
    checks.push({ name: 'kubectl', ok: true, detail: `v${v.clientVersion.major}.${v.clientVersion.minor}` });
  } catch {
    checks.push({ name: 'kubectl', ok: false, detail: 'kubectl not found or not configured' });
  }

  // Cluster reachable
  try {
    execSync('kubectl cluster-info', { encoding: 'utf-8', timeout: 10_000, stdio: 'pipe' });
    checks.push({ name: 'cluster', ok: true, detail: 'reachable' });
  } catch {
    checks.push({ name: 'cluster', ok: false, detail: 'cluster not reachable — check kubeconfig' });
  }

  // Helm installed
  try {
    const helmVersion = execSync('helm version --short', { encoding: 'utf-8' }).trim();
    checks.push({ name: 'helm', ok: true, detail: helmVersion });
  } catch {
    checks.push({ name: 'helm', ok: false, detail: 'helm not found — install from https://helm.sh' });
  }

  // Node count and resources
  try {
    const nodesJson = execSync('kubectl get nodes -o json', { encoding: 'utf-8', timeout: 10_000 });
    const nodes = JSON.parse(nodesJson);
    const count = nodes.items.length;
    checks.push({ name: 'nodes', ok: count >= 1, detail: `${count} node(s)` });
  } catch {
    checks.push({ name: 'nodes', ok: false, detail: 'cannot list nodes' });
  }

  // Default StorageClass
  try {
    const sc = execSync(
      'kubectl get storageclass -o jsonpath="{.items[?(@.metadata.annotations.storageclass\\.kubernetes\\.io/is-default-class==\\"true\\")].metadata.name}"',
      { encoding: 'utf-8', timeout: 10_000 }
    ).trim();
    checks.push({ name: 'storageclass', ok: sc.length > 0, detail: sc || 'no default StorageClass' });
  } catch {
    checks.push({ name: 'storageclass', ok: false, detail: 'cannot check StorageClass' });
  }

  return {
    valid: checks.every((c) => c.ok),
    checks,
  };
}
