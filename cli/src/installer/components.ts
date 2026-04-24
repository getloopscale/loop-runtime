// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import { PlatformConfig, PlatformStatus, ComponentStatus } from './types';
import { BootstrapTokens } from './bootstrap';
import { installOrUpgrade, uninstall, test as helmTest, releaseExists } from './helm';

/**
 * OCI registry for LoopZero Helm charts.
 */
const CHART_REGISTRY = 'oci://registry.digitalocean.com/loopzero-system/helm';
const CHART_VERSION = '0.2.0';
const IMAGE_PULL_SECRET_NAME = 'loopzero-system-pull';

/**
 * Ensure DOCR imagePullSecret exists in the target namespace.
 * Uses doctl to generate a docker config, or falls back to existing docker config.
 */
export function ensureImagePullSecret(namespace: string, log: (msg: string) => void): void {
  try {
    // Check if secret already exists
    execSync(`kubectl get secret ${IMAGE_PULL_SECRET_NAME} -n ${namespace}`, {
      stdio: 'pipe', timeout: 5_000,
    });
    log(`  ✓ Image pull secret exists`);
    return;
  } catch {
    // Secret doesn't exist — create it
  }

  // Try doctl registry login first (refreshes docker config)
  try {
    execSync('doctl registry login --expiry-seconds 3600', { stdio: 'pipe', timeout: 30_000 });
  } catch {
    // doctl may not be available — try with existing docker config
  }

  // Find docker config
  const home = process.env.HOME || '/root';
  const dockerConfigPaths = [
    `${home}/.docker/config.json`,
    '/root/.docker/config.json',
  ];

  let dockerConfig = '';
  for (const p of dockerConfigPaths) {
    try {
      execSync(`test -f ${p}`, { stdio: 'pipe' });
      dockerConfig = p;
      break;
    } catch {
      continue;
    }
  }

  if (dockerConfig) {
    // Create from docker config file
    execSync(
      `kubectl create secret generic ${IMAGE_PULL_SECRET_NAME} ` +
      `--namespace ${namespace} ` +
      `--from-file=.dockerconfigjson=${dockerConfig} ` +
      `--type=kubernetes.io/dockerconfigjson ` +
      `--dry-run=client -o yaml | kubectl apply -f -`,
      { stdio: 'pipe', timeout: 10_000 },
    );
    log(`  ✓ Image pull secret created from docker config`);
  } else {
    log(`  ⚠ No docker config found — pods may fail to pull images`);
    log(`    Run: doctl registry login && loopzero platform install ...`);
  }
}

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
 * Compute env-var overrides for the build-time image tags that the
 * loopzero-api Go binary defaults to. See Fix 8 comment in buildPlatformValues.
 *
 * Customer may set `config.images.packsRunner` and/or
 * `config.images.cosignSigner` (full image ref including tag). Otherwise
 * we fall back to tags that are known to exist in the DOCR as of v0.2.0.
 * When the publish pipeline is fixed to emit `v0.1.0` (or current version)
 * for both images, this function becomes a no-op.
 */
function imageTagOverrides(config: PlatformConfig): Record<string, string> {
  const out: Record<string, string> = {};
  const imgCfg = (config as { images?: { packsRunner?: string; cosignSigner?: string } }).images;

  const packs = imgCfg?.packsRunner
    || 'registry.digitalocean.com/loopzero-system/loopzero-packs-runner:20260424';
  const cosign = imgCfg?.cosignSigner
    || 'registry.digitalocean.com/loopzero-system/loopzero-cosign-signer:dev';

  // These env vars are read by api/internal/argo/languages.go loopzeroPacks() and
  // api/internal/argo/cosign.go loopzeroCosign() to override imageLoopZeroPacks /
  // imageCosignSigner constants.
  out['loopzero-api.env.LOOPZERO_PACKS_IMAGE'] = packs;
  out['loopzero-api.env.LOOPZERO_COSIGN_IMAGE'] = cosign;
  out['loopzero-controller.env.LOOPZERO_PACKS_IMAGE'] = packs;
  out['loopzero-controller.env.LOOPZERO_COSIGN_IMAGE'] = cosign;
  return out;
}

/**
 * Build Helm values for the platform chart based on customer config + bootstrap tokens.
 *
 * This is the central function that wires all runtime configuration.
 * Every token, credential, and URL needed by every component is set here.
 */
function buildPlatformValues(
  config: PlatformConfig,
  tokens: BootstrapTokens,
): Record<string, unknown> {
  const values: Record<string, unknown> = {
    // ── Global ────────────────────────────────────────────────────────────
    'global.registry': 'registry.digitalocean.com/loopzero-system',
    'global.imagePullSecrets[0].name': IMAGE_PULL_SECRET_NAME,

    // CRDs installed separately — disable in umbrella
    'loopzero-crds.enabled': 'false',

    // Disable components that need external CRDs not yet available
    'loopzero-monitoring.enabled': 'false',
    'loopzero-admission-policies.enabled': 'false',

    // ── loopzero-api ──────────────────────────────────────────────────────────
    'loopzero-api.config.baseDomain': config.domain,
    'loopzero-api.config.giteaUrl': 'http://gitea-http.gitea.svc.cluster.local:3000',
    'loopzero-api.config.argocdUrl': 'http://argocd-server.argocd.svc.cluster.local:80',
    'loopzero-api.config.minioEndpoint': 'http://minio.loopzero-system.svc.cluster.local:9000',
    'loopzero-api.config.registryUrl': config.registry.endpoint,
    'loopzero-api.config.jwksUrl': 'http://ory-hydra-public.ory.svc.cluster.local:4444/.well-known/jwks.json',
    'loopzero-api.config.jwtIssuer': 'http://ory-hydra-public.ory.svc.cluster.local:4444/',
    'loopzero-api.config.argoNamespace': 'argo',
    'loopzero-api.config.argocdNamespace': 'argocd',

    // API secrets (Helm creates the K8s Secret with these values)
    'loopzero-api.secrets.databaseUrl': config.database.url || 'postgres://loopzero:loopzero@postgresql.loopzero-system.svc:5432/loopzero?sslmode=disable',
    'loopzero-api.secrets.giteaToken': tokens.giteaToken,
    'loopzero-api.secrets.argocdToken': tokens.argocdPassword,
    'loopzero-api.secrets.minioAccessKey': tokens.minioAccessKey,
    'loopzero-api.secrets.minioSecretKey': tokens.minioSecretKey,
    'loopzero-api.secrets.argoCallbackSecret': tokens.argoCallbackSecret,
    // CHG-2026-04-25-E2E (Gap 2): bootstrap + service-auth secrets that the
    // chart now templates. Without bootstrapSecret, POST /internal/bootstrap
    // returns 401 and the install can't seed the first admin user.
    'loopzero-api.secrets.bootstrapSecret': tokens.bootstrapSecret,
    'loopzero-api.secrets.serviceAuthSecret': tokens.serviceAuthSecret,

    // ── loopzero-controller ───────────────────────────────────────────────────
    'loopzero-controller.config.baseDomain': config.domain,
    'loopzero-controller.config.giteaUrl': 'http://gitea-http.gitea.svc.cluster.local:3000',
    'loopzero-controller.config.argocdUrl': 'http://argocd-server.argocd.svc.cluster.local:80',
    'loopzero-controller.config.minioEndpoint': 'http://minio.loopzero-system.svc.cluster.local:9000',
    'loopzero-controller.config.registryUrl': config.registry.endpoint,
    'loopzero-controller.config.argoNamespace': 'argo',
    'loopzero-controller.config.argocdNamespace': 'argocd',
    // LOOPZERO_CLUSTER_ISSUER: cert-manager ClusterIssuer for TLS. Customer can
    // override via config.clusterIssuer; default production "letsencrypt-prod"
    // assumes cert-manager is wired to Let's Encrypt. For self-signed or
    // internal-only installs, the installer provisions "loopzero-ca-issuer"
    // automatically (Step 5, profile=minimal). Chart v0.2.0 reads this as
    // an env var, not config — set both paths so either chart layout works.
    'loopzero-controller.config.clusterIssuer': config.clusterIssuer
      || (config.profile === 'production' ? 'letsencrypt-prod' : 'loopzero-ca-issuer'),
    'loopzero-controller.env.LOOPZERO_CLUSTER_ISSUER': config.clusterIssuer
      || (config.profile === 'production' ? 'letsencrypt-prod' : 'loopzero-ca-issuer'),
    'loopzero-controller.secrets.databaseUrl': config.database.url || 'postgres://loopzero:loopzero@postgresql.loopzero-system.svc:5432/loopzero?sslmode=disable',
    'loopzero-controller.secrets.giteaToken': tokens.giteaToken,
    'loopzero-controller.secrets.argocdToken': tokens.argocdPassword,
    'loopzero-controller.secrets.minioAccessKey': tokens.minioAccessKey,
    'loopzero-controller.secrets.minioSecretKey': tokens.minioSecretKey,

    // ── loopzero-tenant-controller ────────────────────────────────────────────
    'loopzero-tenant-controller.config.giteaUrl': 'http://gitea-http.gitea.svc.cluster.local:3000',
    'loopzero-tenant-controller.config.baseDomain': config.domain,
    'loopzero-tenant-controller.config.argocdNamespace': 'argocd',
    'loopzero-tenant-controller.secrets.giteaToken': tokens.giteaToken,

    // ── loopzero-build-cwts ───────────────────────────────────────────────────
    // CWT templates reference loopzero-minio-credentials secret directly
    // (created separately by createMinioCredentialsSecret)
    'loopzero-build-cwts.minio.insecure': 'true',

    // Fix 8 (CHG-2026-04-24-INSTALLER-E2E): loopzero-packs-runner and
    // loopzero-cosign-signer image tags. The source-of-truth for the tag used
    // at runtime is the `loopzero-api` container (which renders the build
    // workflow) via env vars LOOPZERO_PACKS_IMAGE / LOOPZERO_COSIGN_IMAGE. Chart
    // v0.2.0 hardcodes tag v0.1.0 in the Go binary (api/internal/argo/
    // constants.go); the image publish pipeline tags differently
    // (loopzero-packs-runner currently only has date-stamped tags like
    // 20260424; loopzero-cosign-signer has "dev"). Until the publish pipeline
    // emits v0.1.0/v0.2.0 for both, let the installer inject the actual
    // tag via loopzero-api env. Customer overrides via values.yaml
    // images.packsRunner / images.cosignSigner.
    ...imageTagOverrides(config),

    // ── Network policies ──────────────────────────────────────────────────
    // Disable for initial install — cross-namespace egress requires known endpoints.
    // Can be enabled after all services are up via `loopzero platform upgrade`.
    'loopzero-api.networkPolicy.enabled': 'false',
    'loopzero-controller.networkPolicy.enabled': 'false',
    'loopzero-tenant-controller.networkPolicy.enabled': 'false',
  };

  // ── Kubeconfig (single-cluster mode) ──────────────────────────────────
  if (!config.clusters || config.clusters.mode === 'single') {
    values['loopzero-api.kubeconfig.build.existingConfigMap'] = 'loopzero-build-kubeconfig';
    values['loopzero-api.kubeconfig.runtime.existingConfigMap'] = 'loopzero-runtime-kubeconfig';
    values['loopzero-controller.kubeconfig.build.existingConfigMap'] = 'loopzero-build-kubeconfig';
    values['loopzero-tenant-controller.kubeconfig.build.existingConfigMap'] = 'loopzero-build-kubeconfig';
    values['loopzero-tenant-controller.kubeconfig.runtime.existingConfigMap'] = 'loopzero-runtime-kubeconfig';
  }

  // ── Profile-based sizing ──────────────────────────────────────────────
  if (config.profile === 'production') {
    values['loopzero-api.replicaCount'] = '2';
    values['loopzero-cloud.replicaCount'] = '2';
    values['loopzero-dashboard.replicaCount'] = '2';
    values['loopzero-api.networkPolicy.enabled'] = 'true';
    values['loopzero-controller.networkPolicy.enabled'] = 'true';
    values['loopzero-tenant-controller.networkPolicy.enabled'] = 'true';
  }

  return values;
}

/**
 * Install LoopZero platform components via umbrella chart.
 */
export function installPlatform(
  config: PlatformConfig,
  tokens: BootstrapTokens,
  log: (msg: string) => void,
): void {
  const values = buildPlatformValues(config, tokens);

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
export function upgradePlatform(
  version: string,
  config: PlatformConfig,
  tokens: BootstrapTokens,
  log: (msg: string) => void,
): void {
  const values = buildPlatformValues(config, tokens);

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
export function waitForPods(log: (msg: string) => void, timeoutSec = 180): boolean {
  log('  Waiting for pods...');
  const start = Date.now();
  const deadline = start + timeoutSec * 1000;

  while (Date.now() < deadline) {
    try {
      const output = execSync(
        'kubectl get pods -n loopzero-system -o jsonpath="{.items[*].status.conditions[?(@.type==\'Ready\')].status}"',
        { encoding: 'utf-8', timeout: 10_000 },
      ).trim();
      const statuses = output.split(' ').filter(Boolean);
      const allReady = statuses.length > 0 && statuses.every((s) => s === 'True');
      if (allReady) {
        const elapsed = Math.round((Date.now() - start) / 1000);
        log(`  ✓ All ${statuses.length} pods Ready (${elapsed}s)`);
        return true;
      }
    } catch {
      // kubectl may fail transiently
    }
    execSync('sleep 5');
  }

  // Show which pods aren't ready for debugging
  try {
    const notReady = execSync(
      `kubectl get pods -n loopzero-system --field-selector=status.phase!=Running --no-headers 2>/dev/null || true`,
      { encoding: 'utf-8', timeout: 5_000 },
    ).trim();
    if (notReady) {
      log(`  Pods not ready:\n${notReady}`);
    }
  } catch {
    // ignore
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
