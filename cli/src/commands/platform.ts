// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as yaml from 'js-yaml';
import {
  PlatformConfig,
  validateCluster,
  installPrerequisites,
  installCRDs,
  installPlatform,
  upgradePlatform,
  uninstallPlatform,
  validateInstallation,
  waitForPods,
  getPlatformStatus,
} from '../installer';

export interface PlatformDeps {
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

/**
 * `loopzero platform install -f values.yaml [--profile minimal|production]`
 *
 * Installs the complete LoopZero platform on the current Kubernetes cluster.
 */
export async function platformInstall(
  deps: PlatformDeps,
  valuesFile: string,
  profile?: string,
): Promise<number> {
  const { writeTo, writeError } = deps;

  // Load customer values
  let config: PlatformConfig;
  try {
    const raw = fs.readFileSync(valuesFile, 'utf-8');
    config = yaml.load(raw) as PlatformConfig;
    if (profile) config.profile = profile as PlatformConfig['profile'];
    if (!config.profile) config.profile = 'minimal';
  } catch {
    writeError(`Failed to read values file: ${valuesFile}`);
    return 1;
  }

  if (!config.domain) {
    writeError('Missing required field: domain');
    return 1;
  }

  writeTo('');
  writeTo('  LoopZero Platform Installer');
  writeTo('  =======================');
  writeTo('');

  // Step 1: Validate cluster
  writeTo('Step 1/7: Validating cluster...');
  const validation = validateCluster();
  for (const check of validation.checks) {
    writeTo(`  ${check.ok ? '✓' : '✗'} ${check.name}: ${check.detail}`);
  }
  if (!validation.valid) {
    writeError('Cluster validation failed. Fix the issues above and retry.');
    return 1;
  }
  writeTo('');

  // Step 2: Prerequisites
  writeTo('Step 2/7: Installing prerequisites...');
  const prereqResults = installPrerequisites(config, writeTo);
  const prereqFailed = prereqResults.some((r) => r.status === 'failed');
  if (prereqFailed) {
    writeError('Prerequisite installation failed. Check the errors above.');
    return 1;
  }
  writeTo('');

  // Step 3: CRDs
  writeTo('Step 3/7: Installing CRDs...');
  try {
    installCRDs(writeTo);
  } catch (err) {
    writeError(`CRD installation failed: ${err}`);
    return 1;
  }
  writeTo('');

  // Step 4: Platform
  writeTo('Step 4/7: Installing LoopZero platform...');
  try {
    installPlatform(config, writeTo);
  } catch (err) {
    writeError(`Platform installation failed: ${err}`);
    return 1;
  }
  writeTo('');

  // Step 5: Wait for pods
  writeTo('Step 5/7: Waiting for pods...');
  const podsReady = waitForPods(writeTo);
  if (!podsReady) {
    writeError('Pods did not become ready. Check: kubectl get pods -n loopzero-system');
    return 1;
  }
  writeTo('');

  // Step 6: Health checks
  writeTo('Step 6/7: Running health checks...');
  validateInstallation(writeTo);
  writeTo('');

  // Step 7: Done
  writeTo('Step 7/7: Done!');
  writeTo('');
  writeTo(`  Platform URL:    https://${config.domain}`);
  writeTo(`  Dashboard:       https://cloud.${config.domain}`);
  writeTo(`  API:             https://api.${config.domain}`);
  writeTo('');
  writeTo('  Next steps:');
  writeTo(`  1. Configure DNS: *.${config.domain} → <ingress-ip>`);
  writeTo('  2. Create first user: loopzero admin create-user --email admin@example.com');
  writeTo(`  3. Login: loopzero login --api-url https://api.${config.domain}`);
  writeTo('');

  return 0;
}

/**
 * `loopzero platform status`
 */
export async function platformStatus(deps: PlatformDeps): Promise<number> {
  const { writeTo } = deps;
  const status = getPlatformStatus();

  writeTo('');
  writeTo(`  Phase:   ${status.phase}`);
  writeTo(`  Version: ${status.version || 'unknown'}`);
  writeTo(`  CRDs:    ${status.crdsInstalled}/10`);
  writeTo('');
  writeTo('  Components:');
  for (const c of status.components) {
    const icon = c.status === 'Running' ? '✓' : c.status === 'NotInstalled' ? '–' : '✗';
    writeTo(`    ${icon} ${c.name}: ${c.status}${c.replicas ? ` (${c.replicas})` : ''}`);
  }

  if (status.endpoints.api) {
    writeTo('');
    writeTo('  Endpoints:');
    writeTo(`    API:       ${status.endpoints.api}`);
    writeTo(`    Dashboard: ${status.endpoints.dashboard}`);
  }
  writeTo('');

  return status.phase === 'Ready' ? 0 : 1;
}

/**
 * `loopzero platform upgrade --version <version>`
 */
export async function platformUpgrade(
  deps: PlatformDeps,
  version: string,
  valuesFile?: string,
): Promise<number> {
  const { writeTo, writeError } = deps;

  let config: PlatformConfig | undefined;
  if (valuesFile) {
    try {
      config = yaml.load(fs.readFileSync(valuesFile, 'utf-8')) as PlatformConfig;
    } catch {
      writeError(`Failed to read values file: ${valuesFile}`);
      return 1;
    }
  }

  writeTo(`Upgrading to v${version}...`);
  try {
    upgradePlatform(version, config || { domain: '', profile: 'minimal', database: { external: true }, registry: { type: 'docr', endpoint: '' }, auth: { type: 'bundled' }, storage: { endpoint: '', bucket: '' } }, writeTo);
    const podsReady = waitForPods(writeTo);
    if (!podsReady) {
      writeError('Upgrade may have failed — pods not ready');
      return 1;
    }
    writeTo(`✓ Upgraded to v${version}`);
    return 0;
  } catch (err) {
    writeError(`Upgrade failed: ${err}`);
    return 1;
  }
}

/**
 * `loopzero platform uninstall`
 */
export async function platformUninstall(deps: PlatformDeps): Promise<number> {
  const { writeTo } = deps;
  writeTo('Uninstalling LoopZero platform...');
  uninstallPlatform(writeTo);
  writeTo('✓ Uninstalled');
  return 0;
}
