// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import { PlatformConfig, PlatformStatus, ComponentStatus } from './types';
import { installOrUpgrade, uninstall, test as helmTest, releaseExists } from './helm';

/**
 * OCI registry for LoopZero Helm charts.
 */
const CHART_REGISTRY = 'oci://registry.digitalocean.com/loopzero-system/helm';
const CHART_VERSION = '0.1.3';

/**
 * Install LoopZero CRDs (lifecycle independent, installed first).
 */
export function installCRDs(log: (msg: string) => void): void {
  log('  Installing CRDs...');
  installOrUpgrade({
    name: 'loopzero-crds',
    chart: `${CHART_REGISTRY}/loopzero-crds`,
    version: CHART_VERSION,
    namespace: 'loopzero-system',
    createNamespace: true,
    wait: true,
    timeout: '2m',
  });

  // Verify CRD count
  try {
    const crds = execSync('kubectl get crd -o name 2>/dev/null | grep loopzero.io | wc -l', {
      encoding: 'utf-8',
    }).trim();
    log(`  ✓ ${crds} LoopZero CRDs installed`);
  } catch {
    log('  ✓ CRDs installed');
  }
}

/**
 * Build Helm values for the platform chart based on customer config.
 */
function buildPlatformValues(config: PlatformConfig): Record<string, unknown> {
  const values: Record<string, unknown> = {
    'global.registry': 'registry.digitalocean.com/loopzero-system',
  };

  // API config
  values['loopzero-api.config.baseDomain'] = config.domain;
  values['loopzero-api.config.giteaUrl'] = `http://gitea-http.gitea.svc.cluster.local:3000`;
  values['loopzero-api.config.argocdUrl'] = `http://argocd-server.argocd.svc.cluster.local:80`;
  values['loopzero-api.config.minioEndpoint'] = `minio.loopzero-system.svc.cluster.local:9000`;
  values['loopzero-api.config.registryUrl'] = config.registry.endpoint;

  // Database
  if (config.database.external && config.database.url) {
    values['loopzero-api.secrets.databaseUrl'] = config.database.url;
    values['loopzero-controller.secrets.databaseUrl'] = config.database.url;
  }

  // Profile-based sizing
  if (config.profile === 'production') {
    values['loopzero-api.replicaCount'] = '2';
    values['loopzero-cloud.replicaCount'] = '2';
    values['loopzero-dashboard.replicaCount'] = '2';
    values['loopzero-api.networkPolicy.enabled'] = 'true';
    values['loopzero-cloud.networkPolicy.enabled'] = 'true';
    values['loopzero-dashboard.networkPolicy.enabled'] = 'true';
  }

  return values;
}

/**
 * Install LoopZero platform components via umbrella chart.
 */
export function installPlatform(config: PlatformConfig, log: (msg: string) => void): void {
  const values = buildPlatformValues(config);

  log('  Installing LoopZero platform...');
  installOrUpgrade({
    name: 'loopzero',
    chart: `${CHART_REGISTRY}/loopzero-platform`,
    version: CHART_VERSION,
    namespace: 'loopzero-system',
    createNamespace: true,
    values,
    wait: true,
    timeout: '5m',
  });
  log('  ✓ LoopZero platform installed');
}

/**
 * Upgrade LoopZero platform to a new version.
 */
export function upgradePlatform(version: string, config: PlatformConfig, log: (msg: string) => void): void {
  const values = buildPlatformValues(config);

  log(`  Upgrading to v${version}...`);

  // CRDs first
  installOrUpgrade({
    name: 'loopzero-crds',
    chart: `${CHART_REGISTRY}/loopzero-crds`,
    version: version,
    namespace: 'loopzero-system',
    wait: true,
    timeout: '2m',
  });

  // Platform
  installOrUpgrade({
    name: 'loopzero',
    chart: `${CHART_REGISTRY}/loopzero-platform`,
    version: version,
    namespace: 'loopzero-system',
    values,
    wait: true,
    timeout: '5m',
  });
  log(`  ✓ Upgraded to v${version}`);
}

/**
 * Uninstall LoopZero platform (preserves CRDs and data).
 */
export function uninstallPlatform(log: (msg: string) => void): void {
  log('  Removing LoopZero platform...');
  uninstall('loopzero', 'loopzero-system');
  log('  ✓ Platform removed (CRDs and data preserved)');
  log('  To remove CRDs: helm uninstall loopzero-crds -n loopzero-system');
}

/**
 * Run helm test hooks to validate installation.
 */
export function validateInstallation(log: (msg: string) => void): boolean {
  log('  Running health checks...');
  const ok = helmTest('loopzero', 'loopzero-system');
  if (ok) {
    log('  ✓ All health checks passed');
  } else {
    log('  ✗ Some health checks failed — run: helm test loopzero -n loopzero-system');
  }
  return ok;
}

/**
 * Wait for all pods in loopzero-system to be Ready.
 */
export function waitForPods(log: (msg: string) => void, timeoutSec = 120): boolean {
  log('  Waiting for pods...');
  const deadline = Date.now() + timeoutSec * 1000;

  while (Date.now() < deadline) {
    try {
      const output = execSync(
        'kubectl get pods -n loopzero-system -o jsonpath="{.items[*].status.conditions[?(@.type==\'Ready\')].status}"',
        { encoding: 'utf-8', timeout: 10_000 },
      ).trim();
      const statuses = output.split(' ').filter(Boolean);
      const allReady = statuses.length > 0 && statuses.every((s) => s === 'True');
      if (allReady) {
        log(`  ✓ All ${statuses.length} pods Ready (${Math.round((Date.now() - (deadline - timeoutSec * 1000)) / 1000)}s)`);
        return true;
      }
    } catch {
      // kubectl may fail transiently
    }
    execSync('sleep 3');
  }

  log('  ✗ Timeout waiting for pods');
  return false;
}

/**
 * Get current platform status.
 */
export function getPlatformStatus(): PlatformStatus {
  const components: ComponentStatus[] = [];

  // Check each LoopZero component
  const loopzeroComponents = ['loopzero-api', 'loopzero-tenant-controller', 'loopzero-controller', 'loopzero-cloud', 'loopzero-dashboard'];
  for (const name of loopzeroComponents) {
    try {
      const output = execSync(
        `kubectl get deploy -n loopzero-system -l app.kubernetes.io/name=${name} -o jsonpath="{.items[0].status.readyReplicas}/{.items[0].spec.replicas}"`,
        { encoding: 'utf-8', timeout: 5_000 },
      ).trim();
      components.push({
        name,
        status: output.startsWith('0/') || !output ? 'Pending' : 'Running',
        replicas: output || '0/0',
      });
    } catch {
      components.push({ name, status: 'NotInstalled' });
    }
  }

  // CRD count
  let crdsInstalled = 0;
  try {
    crdsInstalled = parseInt(
      execSync('kubectl get crd -o name 2>/dev/null | grep loopzero.io | wc -l', { encoding: 'utf-8' }).trim(),
      10,
    );
  } catch {
    // ignore
  }

  // Determine phase
  const installed = releaseExists('loopzero', 'loopzero-system');
  let phase: PlatformStatus['phase'] = 'NotInstalled';
  if (installed) {
    const allRunning = components.every((c) => c.status === 'Running');
    const anyFailed = components.some((c) => c.status === 'Failed');
    if (allRunning) phase = 'Ready';
    else if (anyFailed) phase = 'Failed';
    else phase = 'Degraded';
  }

  return {
    phase,
    version: CHART_VERSION,
    components,
    crdsInstalled,
    endpoints: {
      api: installed ? `https://api.${process.env.LOOPZERO_DOMAIN || 'localhost'}` : undefined,
      dashboard: installed ? `https://cloud.${process.env.LOOPZERO_DOMAIN || 'localhost'}` : undefined,
    },
  };
}
