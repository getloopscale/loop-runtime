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
  postgresql: '16.6.1',
  gitea: '10.6.0',
  argocd: '7.3.11',
  argoWorkflows: '1.0.5',
  oryKratos: '0.61.0',
  oryHydra: '0.61.0',
  minio: '5.4.0',
};

/**
 * Helm repos needed for prerequisites.
 */
const REPOS: Record<string, string> = {
  jetstack: 'https://charts.jetstack.io',
  traefik: 'https://traefik.github.io/charts',
  bitnami: 'https://charts.bitnami.com/bitnami',
  'gitea-charts': 'https://dl.gitea.com/charts/',
  argo: 'https://argoproj.github.io/argo-helm',
  ory: 'https://k8s.ory.sh/helm/charts',
  minio: 'https://charts.min.io/',
};

/**
 * Build the ordered list of prerequisite Helm releases based on config.
 */
export function buildPrerequisites(config: PlatformConfig): HelmRelease[] {
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

  // 3. PostgreSQL (only if not external)
  if (!config.database.external) {
    releases.push({
      name: 'postgresql',
      chart: 'bitnami/postgresql',
      version: VERSIONS.postgresql,
      namespace: 'loopzero-system',
      createNamespace: true,
      values: {
        'auth.database': 'loopzero',
        'auth.username': 'loopzero',
        'primary.persistence.size': '10Gi',
      },
      waitReady: true,
      timeout: '5m',
      order: 30,
    });
  }

  // 4. Gitea
  releases.push({
    name: 'gitea',
    chart: 'gitea-charts/gitea',
    version: VERSIONS.gitea,
    namespace: 'gitea',
    createNamespace: true,
    values: {
      'gitea.admin.username': 'loopzero-admin',
      'gitea.config.server.ROOT_URL': `https://git.${config.domain}`,
      'persistence.size': '10Gi',
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

  // 6. Ory (bundled auth)
  if (config.auth.type === 'bundled') {
    const dbBase = config.database.url
      ? config.database.url.replace(/\/[^/]*(\?.*)?$/, '')
      : 'postgres://loopzero:loopzero@postgresql.loopzero-system.svc:5432';
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
  }

  // 7. MinIO (artifact storage)
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
    },
    waitReady: true,
    timeout: '3m',
    order: 70,
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

/**
 * Install all prerequisites in order.
 * Returns results for each step.
 */
export function installPrerequisites(
  config: PlatformConfig,
  log: (msg: string) => void,
): { name: string; status: 'installed' | 'exists' | 'failed'; error?: string }[] {
  ensureHelmRepos();

  const releases = buildPrerequisites(config);
  const results: ReturnType<typeof installPrerequisites> = [];

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
