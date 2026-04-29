// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP — T3.1 + T3.2.
// `loopzero platform install` is the SOLE installation entry point. The
// optional `--provider` flag controls whether L1 (cloud infra) is
// provisioned via Pulumi BEFORE L2 (Helmfile sync). When unset, the CLI
// runs in standalone mode (single existing K8s cluster).

import * as fs from 'fs';
import { spawnSync } from 'child_process';
import * as yaml from 'js-yaml';
import {
  PlatformConfig,
  validateCluster,
  getPlatformStatus,
} from '../installer';
import {
  helmfileSync,
  helmfileDestroy,
  ensureHelmfileAvailable,
  resolveHelmfilePath,
} from '../installer/helmfile-runner';
import {
  pulumiUp,
  pulumiOutputs,
  pulumiDestroy,
  ensurePulumiAvailable,
  resolvePulumiProjectDir,
  resolveProjectRoot,
} from '../installer/pulumi-runner';

export interface PlatformDeps {
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface PlatformInstallOptions {
  valuesFile: string;
  profile?: string;
  provider?: 'digitalocean' | 'aws' | 'k3d';
  dryRun?: boolean;
  skipInfra?: boolean;
}

/**
 * Maps L1 Pulumi outputs to env vars consumed by Helmfile L2 + scripts.
 */
function pulumiOutputsToEnv(outputs: Record<string, unknown>): Record<string, string> {
  const env: Record<string, string> = {};
  const map: Record<string, string> = {
    dbHost: 'LOOPZERO_DB_HOST',
    dbPort: 'LOOPZERO_DB_PORT',
    dbName: 'LOOPZERO_DB_NAME',
    dbUser: 'LOOPZERO_DB_USER',
    dbPassword: 'LOOPZERO_DB_PASSWORD',
    dbUri: 'LOOPZERO_DB_URI',
    bucketName: 'LOOPZERO_BUCKET_NAME',
    bucketEndpoint: 'LOOPZERO_BUCKET_ENDPOINT',
    registryEndpoint: 'LOOPZERO_REGISTRY_ENDPOINT',
    registryName: 'LOOPZERO_REGISTRY_NAME',
    cfTunnelToken: 'LOOPZERO_CF_TUNNEL_TOKEN',
    cfKvNamespaceId: 'LOOPZERO_CF_KV_NAMESPACE',
    harborRedisHost: 'LOOPZERO_HARBOR_REDIS_HOST',
    harborRedisPort: 'LOOPZERO_HARBOR_REDIS_PORT',
    harborRedisPassword: 'LOOPZERO_HARBOR_REDIS_PASSWORD',
    mgmtKubeconfig: 'LOOPZERO_MGMT_KUBECONFIG_RAW',
    buildKubeconfig: 'LOOPZERO_BUILD_KUBECONFIG_RAW',
    runtimeKubeconfig: 'LOOPZERO_RUNTIME_KUBECONFIG_RAW',
    runtime2Kubeconfig: 'LOOPZERO_RUNTIME2_KUBECONFIG_RAW',
  };
  for (const [k, envName] of Object.entries(map)) {
    const v = outputs[k];
    if (v == null) continue;
    env[envName] = typeof v === 'string' ? v : JSON.stringify(v);
  }
  return env;
}

/**
 * `loopzero platform install -f values.yaml [--profile <name>] [--provider <name>] [--dry-run]`
 *
 * 6 steps (post-CHG-2026-04-28-LAYERED-BOOTSTRAP):
 *   1. Validate (config, cluster reachable OR --provider set)
 *   2. Preflight (helmfile installed, tokens valid if --provider)
 *   3. Infrastructure (CONDITIONAL — only with --provider)
 *      → pulumi up → outputs (kubeconfigs, DB, registry, ...)
 *   4. Bootstrap (helmfile sync per cluster role)
 *      → multi-cluster: build → mgmt → runtime × N
 *      → standalone: mgmt only with environment="standalone"
 *   5. Wire (cross-cluster: ArgoCD register, RBAC, wiring ConfigMap)
 *   6. Validate (smoke test, regression checks)
 */
export async function platformInstall(
  deps: PlatformDeps,
  optsOrFile: PlatformInstallOptions | string,
  legacyProfile?: string,
): Promise<number> {
  const { writeTo, writeError } = deps;
  // Backwards-compat: callers may pass (deps, valuesFile, profile) (old signature).
  const opts: PlatformInstallOptions =
    typeof optsOrFile === 'string'
      ? { valuesFile: optsOrFile, profile: legacyProfile }
      : optsOrFile;

  // Load customer values
  let config: PlatformConfig;
  try {
    const raw = fs.readFileSync(opts.valuesFile, 'utf-8');
    config = yaml.load(raw) as PlatformConfig;
    if (opts.profile) config.profile = opts.profile as PlatformConfig['profile'];
    if (!config.profile) config.profile = 'minimal';
    if (opts.provider) config.provider = opts.provider;
  } catch {
    writeError(`Failed to read values file: ${opts.valuesFile}`);
    return 1;
  }

  if (!config.domain) {
    writeError('Missing required field: domain');
    return 1;
  }

  const useInfra = !!config.provider && !opts.skipInfra;
  const projectRoot = resolveProjectRoot();
  // Pick environment:
  //   --provider set + profile=production → prod
  //   --provider set + profile=minimal    → dev
  //   no --provider                       → standalone (single cluster)
  const environment = !useInfra
    ? 'standalone'
    : config.profile === 'production'
      ? 'prod'
      : 'dev';

  writeTo('');
  writeTo('  LoopZero Platform Installer');
  writeTo('  =======================');
  writeTo(`  Domain:      ${config.domain}`);
  writeTo(`  Profile:     ${config.profile}`);
  writeTo(`  Provider:    ${config.provider ?? 'standalone (existing cluster)'}`);
  writeTo(`  Environment: ${environment}`);
  if (opts.dryRun) writeTo('  Mode:        DRY RUN (helmfile diff)');
  writeTo('');

  // ── Step 1: Validate ──────────────────────────────────────────────
  writeTo('Step 1/6: Validating configuration...');
  if (!useInfra) {
    const validation = validateCluster();
    for (const check of validation.checks) {
      writeTo(`  ${check.ok ? '✓' : '✗'} ${check.name}: ${check.detail}`);
    }
    if (!validation.valid) {
      writeError('Cluster validation failed. Either fix the cluster or pass --provider <name>.');
      return 1;
    }
  } else {
    writeTo(`  ✓ provider=${config.provider} — cluster will be provisioned`);
  }

  // ── Step 2: Preflight ─────────────────────────────────────────────
  writeTo('Step 2/6: Preflight checks...');
  const helmfileCheck = ensureHelmfileAvailable();
  if (!helmfileCheck.ok) {
    writeError(`  ✗ ${helmfileCheck.error}`);
    return 1;
  }
  writeTo(`  ✓ helmfile ${helmfileCheck.version}`);

  if (useInfra) {
    const pulumiCheck = ensurePulumiAvailable();
    if (!pulumiCheck.ok) {
      writeError(`  ✗ ${pulumiCheck.error}`);
      return 1;
    }
    writeTo(`  ✓ pulumi ${pulumiCheck.version}`);
  }

  // ── Step 3: Infrastructure (CONDITIONAL — only with --provider) ────
  let extraEnv: Record<string, string> = {};
  if (useInfra) {
    writeTo('Step 3/6: Provisioning cloud infrastructure (Pulumi L1)...');
    const pulumiCtx = {
      projectDir: resolvePulumiProjectDir(projectRoot),
      stack: environment,
      streamOutput: true,
      extraEnv: {
        ...(config.providerConfig?.digitaloceanToken
          ? { DIGITALOCEAN_TOKEN: config.providerConfig.digitaloceanToken }
          : {}),
        ...(config.providerConfig?.cfApiToken
          ? { CLOUDFLARE_API_TOKEN: config.providerConfig.cfApiToken }
          : {}),
      },
    };
    if (!opts.dryRun) {
      const result = pulumiUp(pulumiCtx);
      if (result.status !== 'success') {
        writeError(`  ✗ pulumi up failed (exit ${result.exitCode})`);
        return 3;
      }
      writeTo(`  ✓ pulumi up complete (${(result.durationMs / 1000).toFixed(1)}s)`);
    } else {
      writeTo('  (dry-run: skipping pulumi up)');
    }

    // Read outputs
    const outputs = pulumiOutputs(pulumiCtx, true);
    if (!outputs) {
      writeError('  ✗ failed to read pulumi outputs');
      return 3;
    }
    extraEnv = pulumiOutputsToEnv(outputs);
    writeTo(`  ✓ stack outputs captured: ${Object.keys(extraEnv).length} env vars`);
  } else {
    writeTo('Step 3/6: Skipping infrastructure (no --provider) — using existing cluster');
  }

  // ── Step 4: Bootstrap (Helmfile L2) ────────────────────────────────
  writeTo('Step 4/6: Installing platform via Helmfile...');
  const clusterRoles: Array<'mgmt' | 'build' | 'runtime'> = useInfra
    ? ['build', 'mgmt', 'runtime']
    : ['mgmt']; // standalone

  for (const role of clusterRoles) {
    const filePath = resolveHelmfilePath(projectRoot, role);
    const kubeconfig = useInfra ? `${process.env.HOME}/.kube/loopzero-${role}.yaml` : undefined;

    // Pre-sync secret generation (replaces helmfile prepare hooks; idempotent).
    if (!opts.dryRun) {
      
      const presyncEnv = { ...process.env, ...extraEnv, ...(kubeconfig ? { KUBECONFIG: kubeconfig } : {}) };
      const presyncScripts: Array<{ script: string; args: string[] }> = role === 'build'
        ? [
            { script: 'install-local-path-provisioner.sh', args: [environment] },
            { script: 'generate-build-secrets.sh', args: [environment] },
          ]
        : role === 'runtime'
          ? [{ script: 'generate-runtime-secrets.sh', args: [environment, 'runtime'] }]
          : [{ script: 'generate-secrets.sh', args: [environment, 'mgmt'] }];

      for (const { script, args } of presyncScripts) {
        const scriptPath = `${projectRoot}/infra/helmfile/scripts/${script}`;
        writeTo(`  → presync ${script}`);
        const psr = spawnSync('bash', [scriptPath, ...args], { env: presyncEnv, stdio: 'inherit' });
        if (psr.status !== 0) {
          writeError(`  ✗ ${script} failed`);
          return 3;
        }
      }
    }

    writeTo(`  → helmfile ${role} (${environment})`);
    const result = helmfileSync({
      filePath,
      environment,
      streamOutput: true,
      dryRun: opts.dryRun,
      extraEnv,
      kubeconfig,
    });
    if (result.status !== 'success') {
      writeError(`  ✗ helmfile ${role} failed (exit ${result.exitCode})`);
      return 3;
    }
    writeTo(`  ✓ helmfile ${role} complete (${(result.durationMs / 1000).toFixed(1)}s)`);
  }

  // ── Step 5: Wire ───────────────────────────────────────────────────
  writeTo('Step 5/6: Wiring cross-cluster connectivity...');
  if (useInfra && !opts.dryRun) {
    
    const wireScript = `${projectRoot}/infra/helmfile/scripts/wire-clusters.sh`;
    const wireResult = spawnSync('bash', [wireScript, environment], {
      env: { ...process.env, ...extraEnv },
      stdio: 'inherit',
    });
    if (wireResult.status !== 0) {
      writeError('  ✗ wire-clusters.sh failed');
      return 3;
    }
    writeTo('  ✓ cross-cluster wiring complete');
  } else {
    writeTo('  (skipping cross-cluster wiring — standalone mode)');
  }

  // ── Step 6: Validate ───────────────────────────────────────────────
  writeTo('Step 6/6: Validating installation...');
  if (!opts.dryRun) {
    const status = getPlatformStatus();
    writeTo(`  Platform phase: ${status.phase}`);
    for (const c of status.components) {
      writeTo(`    ${c.status === 'Running' ? '✓' : '✗'} ${c.name}: ${c.status}`);
    }
    if (status.phase !== 'Ready' && status.phase !== 'Installing') {
      writeError('Installation completed but platform is not Ready.');
      return 5;
    }
  }

  writeTo('');
  writeTo('  ✓ LoopZero platform installed.');
  writeTo(`    Domain:    https://${config.domain}`);
  writeTo(`    API:       https://api.${config.domain}`);
  writeTo(`    Dashboard: https://app.${config.domain}`);
  return 0;
}

/**
 * `loopzero platform secrets backup [--output <file>]`
 *
 * CHG-2026-04-28-LAYERED-BOOTSTRAP iter 14 — manual on-demand backup of all
 * LoopZero-managed Secrets. Useful before destructive operations (pulumi destroy,
 * cluster recreation, password rotation). The `generate-secrets.sh` scripts
 * already auto-backup on every run, so this command is for ad-hoc safety.
 */
export async function platformSecretsBackup(
  deps: PlatformDeps,
  output?: string,
): Promise<number> {
  const { writeTo, writeError } = deps;

  const backupDir = `${process.env.HOME}/.loopzero/backups`;
  if (!fs.existsSync(backupDir)) {
    fs.mkdirSync(backupDir, { recursive: true, mode: 0o700 });
  }
  const ts = new Date().toISOString().replace(/[:.]/g, '').slice(0, 15);
  const outFile = output ?? `${backupDir}/secrets-manual-${ts}.yaml`;

  writeTo(`Backing up LoopZero-managed Secrets to ${outFile}...`);

  const namespaces = ['loopzero-system', 'argo', 'external-secrets'];
  const merged: string[] = [];
  for (const ns of namespaces) {
    const r = spawnSync('kubectl', ['get', 'secret', '-n', ns, '-l', 'loopzero.io/managed-by!=,', '-o', 'yaml'], {
      encoding: 'utf-8',
    });
    if (r.status === 0 && r.stdout) {
      merged.push(`# Namespace: ${ns}\n---\n${r.stdout}`);
    }
  }

  if (merged.length === 0) {
    writeError('No LoopZero-managed Secrets found to back up.');
    return 1;
  }

  fs.writeFileSync(outFile, merged.join('\n'), { mode: 0o600 });
  writeTo(`  ✓ Backup saved (${merged.length} namespaces). Restore: loopzero platform secrets restore -f ${outFile}`);
  return 0;
}

/**
 * `loopzero platform secrets restore -f <backup-file>`
 *
 * CHG-2026-04-28-LAYERED-BOOTSTRAP iter 14 — restores LoopZero Secrets from a
 * previously-saved backup. Used after `pulumi destroy`, cluster recreation,
 * or accidental Secret deletion.
 */
export async function platformSecretsRestore(
  deps: PlatformDeps,
  backupFile: string,
): Promise<number> {
  const { writeTo, writeError } = deps;

  if (!fs.existsSync(backupFile)) {
    writeError(`Backup file not found: ${backupFile}`);
    return 1;
  }

  writeTo(`Restoring LoopZero Secrets from ${backupFile}...`);
  const r = spawnSync('kubectl', ['apply', '-f', backupFile], { stdio: 'inherit' });
  if (r.status !== 0) {
    writeError('  ✗ kubectl apply failed');
    return 3;
  }
  writeTo('  ✓ Secrets restored');
  return 0;
}

/**
 * `loopzero platform secrets list`
 *
 * Lists available Secret backups in ~/.loopzero/backups/.
 */
export async function platformSecretsList(deps: PlatformDeps): Promise<number> {
  const { writeTo } = deps;
  const backupDir = `${process.env.HOME}/.loopzero/backups`;
  if (!fs.existsSync(backupDir)) {
    writeTo('No backups directory found.');
    return 0;
  }
  const files = fs
    .readdirSync(backupDir)
    .filter((f) => /secrets.*\.ya?ml$/.test(f))
    .map((f) => ({ name: f, mtime: fs.statSync(`${backupDir}/${f}`).mtime }))
    .sort((a, b) => b.mtime.getTime() - a.mtime.getTime());

  if (files.length === 0) {
    writeTo('No Secret backups found in ~/.loopzero/backups/.');
    return 0;
  }
  writeTo(`Found ${files.length} backup(s) in ${backupDir}:`);
  for (const f of files) {
    writeTo(`  ${f.mtime.toISOString()}  ${f.name}`);
  }
  return 0;
}

/**
 * `loopzero platform status`
 */
export async function platformStatus(deps: PlatformDeps): Promise<number> {
  const { writeTo } = deps;
  const status = getPlatformStatus();
  writeTo('');
  writeTo(`  Phase: ${status.phase}`);
  if (status.version) writeTo(`  Version: ${status.version}`);
  writeTo(`  CRDs installed: ${status.crdsInstalled}`);
  writeTo('');
  writeTo('  Components:');
  for (const c of status.components) {
    const replicas = c.replicas ? ` (${c.replicas})` : '';
    writeTo(`    ${c.status === 'Running' ? '✓' : '✗'} ${c.name}${replicas}`);
  }
  if (status.endpoints.api) {
    writeTo('');
    writeTo('  Endpoints:');
    writeTo(`    API:       ${status.endpoints.api}`);
    if (status.endpoints.dashboard) writeTo(`    Dashboard: ${status.endpoints.dashboard}`);
  }
  return status.phase === 'Ready' ? 0 : 1;
}

/**
 * `loopzero platform upgrade [-f values.yaml]`
 *
 * CHG-2026-04-28-LAYERED-BOOTSTRAP / T3.2 — upgrade is `helmfile sync` with
 * the latest chart versions from the helmfile. Idempotent — sync makes only
 * the changes that differ from the current cluster state.
 */
export async function platformUpgrade(
  deps: PlatformDeps,
  _version: string,
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
  const profile = config?.profile ?? 'minimal';
  const provider = config?.provider;
  const environment = !provider
    ? 'standalone'
    : profile === 'production'
      ? 'prod'
      : 'dev';
  const projectRoot = resolveProjectRoot();
  const clusterRoles: Array<'mgmt' | 'build' | 'runtime'> = provider
    ? ['build', 'mgmt', 'runtime']
    : ['mgmt'];

  writeTo(`Upgrading platform (env=${environment})...`);
  for (const role of clusterRoles) {
    writeTo(`  → helmfile ${role} sync`);
    const result = helmfileSync({
      filePath: resolveHelmfilePath(projectRoot, role),
      environment,
      streamOutput: true,
    });
    if (result.status !== 'success') {
      writeError(`  ✗ helmfile ${role} failed`);
      return 3;
    }
  }
  writeTo('  ✓ Upgrade complete');
  return 0;
}

/**
 * `loopzero platform uninstall`
 *
 * CHG-2026-04-28-LAYERED-BOOTSTRAP / T3.2 — uninstall is `helmfile destroy`
 * (reverse `needs:` order) for L2, plus optional `pulumi destroy` for L1
 * when --provider was used.
 */
export async function platformUninstall(deps: PlatformDeps, opts?: { provider?: string; deleteInfra?: boolean }): Promise<number> {
  const { writeTo, writeError } = deps;
  const projectRoot = resolveProjectRoot();
  const provider = opts?.provider;
  const environment = !provider ? 'standalone' : 'dev';
  const clusterRoles: Array<'mgmt' | 'build' | 'runtime'> = provider
    ? ['runtime', 'mgmt', 'build']
    : ['mgmt'];

  writeTo('Uninstalling LoopZero platform...');
  for (const role of clusterRoles) {
    writeTo(`  → helmfile ${role} destroy`);
    const result = helmfileDestroy({
      filePath: resolveHelmfilePath(projectRoot, role),
      environment,
      streamOutput: true,
    });
    if (result.status !== 'success') {
      writeError(`  ✗ helmfile ${role} destroy failed`);
      return 3;
    }
  }

  if (opts?.deleteInfra && provider) {
    writeTo('  → pulumi destroy (cloud infrastructure)');
    const pulumiCtx = {
      projectDir: resolvePulumiProjectDir(projectRoot),
      stack: environment,
      streamOutput: true,
    };
    const result = pulumiDestroy(pulumiCtx);
    if (result.status !== 'success') {
      writeError('  ✗ pulumi destroy failed');
      return 3;
    }
  }

  writeTo('  ✓ Uninstalled');
  return 0;
}
