// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as yaml from 'js-yaml';
import {
  PlatformConfig,
  validateCluster,
  installPrerequisites,
  ensureImagePullSecret,
  installCRDs,
  installPlatform,
  upgradePlatform,
  uninstallPlatform,
  validateInstallation,
  waitForPods,
  getPlatformStatus,
  runMigrations,
  bootstrapGitea,
  bootstrapArgoCD,
  createKubeconfigs,
  createHydraClient,
  createAuthDatabases,
} from '../installer';

export interface PlatformDeps {
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

/**
 * `loopzero platform install -f values.yaml [--profile minimal|production]`
 *
 * Installs the complete LoopZero platform on the current Kubernetes cluster.
 * Zero manual intervention — all prerequisites, bootstrap, and config automated.
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

  // ── Step 1: Validate cluster ──────────────────────────────────────
  writeTo('Step 1/10: Validating cluster...');
  const validation = validateCluster();
  for (const check of validation.checks) {
    writeTo(`  ${check.ok ? '✓' : '✗'} ${check.name}: ${check.detail}`);
  }
  if (!validation.valid) {
    writeError('Cluster validation failed. Fix the issues above and retry.');
    return 1;
  }
  writeTo('');

  // ── Step 2: Install prerequisites ─────────────────────────────────
  writeTo('Step 2/10: Installing prerequisites...');
  const prereqResults = installPrerequisites(config, writeTo);
  const prereqFailed = prereqResults.some((r) => r.status === 'failed');
  if (prereqFailed) {
    writeError('Prerequisite installation failed. Check the errors above.');
    return 1;
  }
  writeTo('');

  // ── Step 3: Registry credentials ──────────────────────────────────
  writeTo('Step 3/10: Configuring registry credentials...');
  try {
    ensureImagePullSecret('loopzero-system', writeTo);
  } catch (err) {
    writeError(`Failed to create image pull secret: ${err}`);
    return 1;
  }
  writeTo('');

  // ── Step 4: Database setup ────────────────────────────────────────
  writeTo('Step 4/10: Setting up databases...');
  const dbUrl = config.database.url || 'postgres://loopzero:loopzero@postgresql.loopzero-system.svc:5432/loopzero?sslmode=disable';
  createAuthDatabases(dbUrl, writeTo);
  writeTo('');

  // ── Step 5: Install CRDs ──────────────────────────────────────────
  writeTo('Step 5/10: Installing CRDs...');
  try {
    installCRDs(writeTo);
  } catch (err) {
    writeError(`CRD installation failed: ${err}`);
    return 1;
  }
  writeTo('');

  // ── Step 6: Bootstrap services ────────────────────────────────────
  writeTo('Step 6/10: Bootstrapping services...');

  // Gitea
  const giteaToken = bootstrapGitea(
    'http://gitea-http.gitea.svc.cluster.local:3000',
    'loopzero-admin', 'loopzero-gitea-pass',
    writeTo,
  );

  // ArgoCD
  bootstrapArgoCD('argocd', writeTo);

  // Hydra OAuth2 client
  createHydraClient(writeTo);

  // Kubeconfigs
  createKubeconfigs(config, writeTo);
  writeTo('');

  // ── Step 7: Install LoopZero platform ─────────────────────────────────
  writeTo('Step 7/10: Installing LoopZero platform...');
  // Inject bootstrap tokens into config for the Helm install
  if (giteaToken) config.storage = { ...config.storage, giteaToken } as typeof config.storage & { giteaToken: string };
  try {
    installPlatform(config, writeTo);
  } catch (err) {
    writeError(`Platform installation failed: ${err}`);
    return 1;
  }
  writeTo('');

  // ── Step 8: Run migrations ────────────────────────────────────────
  writeTo('Step 8/10: Running database migrations...');
  runMigrations(dbUrl, writeTo);
  writeTo('');

  // ── Step 9: Wait for pods ─────────────────────────────────────────
  writeTo('Step 9/10: Waiting for pods...');
  const podsReady = waitForPods(writeTo);
  if (!podsReady) {
    writeError('Pods did not become ready. Check: kubectl get pods -n loopzero-system');
    return 1;
  }
  writeTo('');

  // ── Step 10: Validate ─────────────────────────────────────────────
  writeTo('Step 10/10: Running health checks...');
  validateInstallation(writeTo);
  writeTo('');

  // ── Done ──────────────────────────────────────────────────────────
  writeTo('Installation complete!');
  writeTo('');
  writeTo(`  Platform URL:    https://${config.domain}`);
  writeTo(`  Dashboard:       https://cloud.${config.domain}`);
  writeTo(`  API:             https://api.${config.domain}`);
  writeTo('');
  writeTo('  Next steps:');
  writeTo(`  1. Configure DNS: *.${config.domain} → <ingress-ip>`);
  writeTo(`  2. Login: loopzero login --api-url https://api.${config.domain}`);
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
    upgradePlatform(version, config || {
      domain: '', profile: 'minimal',
      database: { external: true },
      registry: { type: 'docr', endpoint: '' },
      auth: { type: 'bundled' },
      storage: { endpoint: '', bucket: '' },
    }, writeTo);
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
