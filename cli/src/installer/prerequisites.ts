// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import * as crypto from 'crypto';
import { HelmRelease, PlatformConfig } from './types';
import { installOrUpgrade, releaseExists } from './helm';

/**
 * Pinned versions for all third-party prerequisites.
 * Same versions used in production — deterministic provisioning.
 */
const VERSIONS = {
  certManager: '1.14.5',
  traefik: '28.3.0',
  cnpgOperator: '0.28.0',
  cnpgCluster: '0.3.1',
  gitea: '10.6.0',
  argocd: '7.3.11',
  argoWorkflows: '1.0.5',
  oryKratos: '0.61.0',
  oryHydra: '0.61.0',
  minio: '5.4.0',
};

/**
 * Helm repos needed for prerequisites.
 *
 * ADR-0012 (2026-04-24): CloudNativePG replaces bitnami/postgresql. CNPG is
 * CNCF graduated — zero vendor lock-in. Bitnami repo removed entirely.
 */
const REPOS: Record<string, string> = {
  jetstack: 'https://charts.jetstack.io',
  traefik: 'https://traefik.github.io/charts',
  cnpg: 'https://cloudnative-pg.github.io/charts',
  'gitea-charts': 'https://dl.gitea.com/charts/',
  argo: 'https://argoproj.github.io/argo-helm',
  ory: 'https://k8s.ory.sh/helm/charts',
  minio: 'https://charts.min.io/',
};

/**
 * Build the ordered list of CORE prerequisite Helm releases.
 *
 * "Core" means: infrastructure that does not depend on a PostgreSQL DSN
 * being known beforehand. Installed first so `extractPostgresCredentials`
 * can run before auth prerequisites (kratos/hydra) which NEED the DSN.
 *
 * Returns: cert-manager, traefik, cnpg-operator, postgresql (Cluster),
 *          gitea (SQLite), argocd, argo-workflows, minio.
 */
export function buildCorePrerequisites(config: PlatformConfig): HelmRelease[] {
  const releases: HelmRelease[] = [];

  // 1. cert-manager (always)
  releases.push({
    name: 'cert-manager',
    chart: 'jetstack/cert-manager',
    version: VERSIONS.certManager,
    namespace: 'cert-manager',
    createNamespace: true,
    values: {
      'installCRDs': 'true',
      'global.leaderElection.namespace': 'cert-manager',
    },
    waitReady: true,
    timeout: '5m',
    order: 10,
  });

  // 2. Traefik ingress
  releases.push({
    name: 'traefik',
    chart: 'traefik/traefik',
    version: VERSIONS.traefik,
    namespace: 'traefik',
    createNamespace: true,
    values: {
      'replicas': '2',
      'ingressRoute.dashboard.enabled': 'false',
    },
    waitReady: true,
    timeout: '3m',
    order: 20,
  });

  // 3. PostgreSQL — CloudNativePG (ADR-0012, replaces bitnami/postgresql)
  //
  // Bitnami removed public images from docker.io in Sep/2025. CNPG is CNCF
  // graduated, production-grade, HA via streaming replication, zero vendor
  // lock-in. Two releases: operator (controller + CRDs) then Cluster CR.
  if (!config.database.external) {
    releases.push({
      name: 'cnpg-operator',
      chart: 'cnpg/cloudnative-pg',
      version: VERSIONS.cnpgOperator,
      namespace: 'cnpg-system',
      createNamespace: true,
      values: {},
      waitReady: true,
      timeout: '3m',
      order: 25,
    });

    const pgInstances = config.profile === 'production' ? '3' : '1';
    releases.push({
      name: 'postgresql',
      chart: 'cnpg/cluster',
      version: VERSIONS.cnpgCluster,
      namespace: 'loopzero-system',
      createNamespace: true,
      values: {
        'cluster.instances': pgInstances,
        'cluster.storage.size': '10Gi',
        'cluster.initdb.database': 'loopzero',
        'cluster.initdb.owner': 'loopzero',
      },
      waitReady: true,
      timeout: '5m',
      order: 30,
    });
  }

  // 4. Gitea — SQLite mode (ADR-0012)
  //
  // Gitea HA subcharts (postgresql-ha, redis-cluster, valkey) depended on
  // bitnami images that no longer exist. Gitea here is a GitOps mirror
  // (internal desired-state store) — SQLite is sufficient and removes all
  // Bitnami subchart dependencies.
  //
  // Admin password is explicit (not random) because bootstrap.ts calls
  // the Gitea admin API to create an org + API token. Chart default
  // generates a random password inaccessible to the installer. For
  // minimal profile this is acceptable; production profile should use
  // an externalized Gitea or customer-managed admin via existingSecret.
  releases.push({
    name: 'gitea',
    chart: 'gitea-charts/gitea',
    version: VERSIONS.gitea,
    namespace: 'gitea',
    createNamespace: true,
    values: {
      'gitea.admin.username': 'loopzero-admin',
      'gitea.admin.password': 'loopzero-gitea-pass',
      'gitea.admin.email': 'admin@loopzero.local',
      'gitea.config.server.ROOT_URL': `https://git.${config.domain}`,
      'persistence.size': '10Gi',
      'postgresql-ha.enabled': 'false',
      'postgresql.enabled': 'false',
      'redis-cluster.enabled': 'false',
      'valkey-cluster.enabled': 'false',
      'valkey.enabled': 'false',
      'gitea.config.database.DB_TYPE': 'sqlite3',
      'gitea.config.session.PROVIDER': 'memory',
      'gitea.config.cache.ADAPTER': 'memory',
      'gitea.config.queue.TYPE': 'level',
    },
    waitReady: true,
    timeout: '5m',
    order: 40,
  });

  // 5. ArgoCD
  releases.push({
    name: 'argocd',
    chart: 'argo/argo-cd',
    version: VERSIONS.argocd,
    namespace: 'argocd',
    createNamespace: true,
    values: {
      'server.insecure': 'true',
      'configs.params.server\\.insecure': 'true',
    },
    waitReady: true,
    timeout: '5m',
    order: 50,
  });

  // 5b. Argo Workflows (build pipeline)
  releases.push({
    name: 'argo-workflows',
    chart: 'argo/argo-workflows',
    version: VERSIONS.argoWorkflows,
    namespace: 'argo',
    createNamespace: true,
    values: {
      'server.serviceType': 'ClusterIP',
      'controller.workflowWorkers': '32',
      'controller.podWorkers': '32',
    },
    waitReady: true,
    timeout: '3m',
    order: 55,
  });

  // MinIO (artifact storage) — DSN-independent
  //
  // Chart default requests 16Gi RAM which is absurd for any shared cluster.
  // 512Mi is sufficient for LoopZero's build artifact traffic in the minimal
  // profile. Production profile overrides to 2Gi via profile branch below.
  const minioMemRequest = config.profile === 'production' ? '2Gi' : '512Mi';
  releases.push({
    name: 'minio',
    chart: 'minio/minio',
    version: VERSIONS.minio,
    namespace: 'loopzero-system',
    values: {
      'mode': 'standalone',
      'persistence.size': '20Gi',
      'buckets[0].name': 'loopzero-artifacts',
      'buckets[0].policy': 'none',
      'resources.requests.memory': minioMemRequest,
    },
    waitReady: true,
    timeout: '3m',
    order: 70,
  });

  return releases.sort((a, b) => a.order - b.order);
}

/**
 * Build the ordered list of AUTH prerequisite Helm releases.
 *
 * Split from `buildCorePrerequisites` because kratos/hydra need the
 * PostgreSQL DSN (extracted from the CNPG secret) at chart install time —
 * their `automigration` hooks fail pre-install if the DSN points to a
 * service that does not exist. The caller must ensure `config.database.url`
 * is populated before calling this.
 *
 * Returns: ory-kratos, ory-hydra (only if `config.auth.type === 'bundled'`).
 */
export function buildAuthPrerequisites(config: PlatformConfig): HelmRelease[] {
  const releases: HelmRelease[] = [];

  if (config.auth.type !== 'bundled') {
    return releases;
  }

  // At this point `config.database.url` MUST be populated — either by
  // the customer (external=true) or by extractPostgresCredentials() after
  // CNPG cluster is healthy. If missing, the derived DSN would point to
  // a nonexistent service and the helm --wait install would timeout.
  const dbBase = config.database.url
    ? config.database.url.replace(/\/[^/?]*(\?.*)?$/, '')
    : '';
  const sslSuffix = config.database.url?.includes('sslmode=') ? '' : '?sslmode=disable';

  // Generate or use provided system secret (must be 32+ chars)
  const configAuth = config.auth as { type: string; systemSecret?: string };
  const systemSecret = configAuth.systemSecret
    || crypto.randomBytes(24).toString('base64url');

  releases.push({
    name: 'ory-kratos',
    chart: 'ory/kratos',
    version: VERSIONS.oryKratos,
    namespace: 'ory',
    createNamespace: true,
    values: {
      'kratos.config.dsn': `${dbBase}/kratos${sslSuffix}`,
      'kratos.config.selfservice.default_browser_return_url': `https://cloud.${config.domain}`,
      'kratos.config.identity.schemas[0].id': 'default',
      'kratos.config.identity.schemas[0].url': 'base64://ewogICIkaWQiOiAiZGVmYXVsdCIsCiAgIiRzY2hlbWEiOiAiaHR0cDovL2pzb24tc2NoZW1hLm9yZy9kcmFmdC0wNy9zY2hlbWEjIiwKICAidGl0bGUiOiAiVXNlciIsCiAgInR5cGUiOiAib2JqZWN0IiwKICAicHJvcGVydGllcyI6IHsKICAgICJ0cmFpdHMiOiB7CiAgICAgICJ0eXBlIjogIm9iamVjdCIsCiAgICAgICJwcm9wZXJ0aWVzIjogewogICAgICAgICJlbWFpbCI6IHsKICAgICAgICAgICJ0eXBlIjogInN0cmluZyIsCiAgICAgICAgICAiZm9ybWF0IjogImVtYWlsIiwKICAgICAgICAgICJvcnkuc2gva3JhdG9zIjogewogICAgICAgICAgICAiY3JlZGVudGlhbHMiOiB7CiAgICAgICAgICAgICAgInBhc3N3b3JkIjogewogICAgICAgICAgICAgICAgImlkZW50aWZpZXIiOiB0cnVlCiAgICAgICAgICAgICAgfQogICAgICAgICAgICB9CiAgICAgICAgICB9CiAgICAgICAgfQogICAgICB9LAogICAgICAicmVxdWlyZWQiOiBbImVtYWlsIl0KICAgIH0KICB9Cn0=',
      'kratos.automigration.enabled': 'true',
    },
    waitReady: true,
    timeout: '5m',
    order: 60,
  });
  releases.push({
    name: 'ory-hydra',
    chart: 'ory/hydra',
    version: VERSIONS.oryHydra,
    namespace: 'ory',
    values: {
      'hydra.config.dsn': `${dbBase}/hydra${sslSuffix}`,
      'hydra.config.urls.self.issuer': 'http://ory-hydra-public.ory.svc.cluster.local:4444/',
      'hydra.config.strategies.access_token': 'jwt',
      'hydra.config.secrets.system[0]': systemSecret,
      'hydra.dev': 'true',
      'hydra.automigration.enabled': 'true',
    },
    waitReady: true,
    timeout: '5m',
    order: 61,
  });

  return releases.sort((a, b) => a.order - b.order);
}

/**
 * Ensure all Helm repos are added.
 */
export function ensureHelmRepos(): void {
  for (const [name, url] of Object.entries(REPOS)) {
    try {
      execSync(`helm repo add ${name} ${url} 2>/dev/null || true`, { stdio: 'pipe' });
    } catch {
      // ignore — repo may already exist
    }
  }
  try {
    execSync('helm repo update', { stdio: 'pipe', timeout: 30_000 });
  } catch {
    // ignore
  }
}

type InstallResult = { name: string; status: 'installed' | 'exists' | 'failed'; error?: string };

/**
 * Install a list of Helm releases sequentially, logging progress.
 * Shared by installCorePrerequisites and installAuthPrerequisites.
 */
function installReleaseList(
  releases: HelmRelease[],
  log: (msg: string) => void,
): InstallResult[] {
  const results: InstallResult[] = [];
  for (const release of releases) {
    try {
      if (releaseExists(release.name, release.namespace)) {
        log(`  ✓ ${release.name} (already installed)`);
        results.push({ name: release.name, status: 'exists' });
        continue;
      }

      log(`  Installing ${release.name} v${release.version}...`);
      installOrUpgrade({
        name: release.name,
        chart: release.chart,
        version: release.version,
        namespace: release.namespace,
        createNamespace: release.createNamespace,
        values: release.values,
        wait: release.waitReady,
        timeout: release.timeout,
      });
      log(`  ✓ ${release.name} v${release.version}`);
      results.push({ name: release.name, status: 'installed' });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log(`  ✗ ${release.name} — ${msg}`);
      results.push({ name: release.name, status: 'failed', error: msg });
    }
  }
  return results;
}

/**
 * Install all CORE prerequisites (infra that does not depend on PG DSN).
 */
export function installCorePrerequisites(
  config: PlatformConfig,
  log: (msg: string) => void,
): InstallResult[] {
  ensureHelmRepos();
  return installReleaseList(buildCorePrerequisites(config), log);
}

/**
 * Install AUTH prerequisites (ory-kratos, ory-hydra — need PG DSN).
 * Caller MUST ensure `config.database.url` is populated first.
 */
export function installAuthPrerequisites(
  config: PlatformConfig,
  log: (msg: string) => void,
): InstallResult[] {
  return installReleaseList(buildAuthPrerequisites(config), log);
}

/**
 * Legacy single-phase install. Kept for backwards compatibility but the
 * two-phase flow (installCorePrerequisites → extract creds →
 * installAuthPrerequisites) in `platform.ts` is preferred.
 */
export function installPrerequisites(
  config: PlatformConfig,
  log: (msg: string) => void,
): InstallResult[] {
  const core = installCorePrerequisites(config, log);
  const auth = installAuthPrerequisites(config, log);
  return [...core, ...auth];
}
