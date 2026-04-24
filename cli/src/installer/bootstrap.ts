// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import { PlatformConfig } from './types';
import * as crypto from 'crypto';

// ─── Tokens generated during bootstrap ─────────────────────────────────────
// These are passed to buildPlatformValues() so the umbrella chart
// receives them via --set and creates the Kubernetes Secrets.

export interface BootstrapTokens {
  giteaToken: string;
  argocdPassword: string;
  minioAccessKey: string;
  minioSecretKey: string;
  argoCallbackSecret: string;
  hydraClientId: string;
}

// ─── Database migrations ────────────────────────────────────────────────────

/**
 * Run database migrations using the API pod's built-in migrate binary.
 * Falls back to psql if the API pod is not yet running.
 */
export function runMigrations(databaseUrl: string, log: (msg: string) => void): void {
  log('  Running database migrations...');
  try {
    // Try via API pod first (has migrate binary baked in)
    const apiPod = execSync(
      'kubectl get pods -n loopzero-system -l app.kubernetes.io/name=loopzero-api -o jsonpath="{.items[0].metadata.name}" 2>/dev/null',
      { encoding: 'utf-8', timeout: 10_000 },
    ).trim();

    if (apiPod) {
      execSync(
        `kubectl exec -n loopzero-system ${apiPod} -- /usr/local/bin/migrate -database "${databaseUrl}" -path /migrations up`,
        { encoding: 'utf-8', timeout: 120_000 },
      );
      log('  ✓ Migrations applied via API pod');
      return;
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (msg.includes('no change') || msg.includes('already')) {
      log('  ✓ Migrations already up to date');
      return;
    }
  }

  // Fallback: run via psql directly (migrations embedded in seed SQL)
  try {
    execSync(`psql "${databaseUrl}" -c "SELECT 1" 2>/dev/null`, { stdio: 'pipe', timeout: 5_000 });
    log('  ⚠ API pod not ready — migrations will apply on first boot');
  } catch {
    log('  ⚠ Cannot reach database — migrations deferred to API startup');
  }
}

// ─── Gitea bootstrap ────────────────────────────────────────────────────────

/**
 * Bootstrap Gitea: create "loopzero" org and generate API token with correct scopes.
 *
 * Gitea 1.25+ requires explicit scopes — `["all"]` is NOT valid.
 * We use basic auth (admin user:pass) to create the token via HTTP API.
 *
 * NETWORKING: The chart service `gitea-http.gitea.svc.cluster.local:3000`
 * is only resolvable from INSIDE the cluster. We open a short-lived
 * `kubectl port-forward` to a local loopback port and target that, so
 * the install works from any host (dev laptop, CI runner, cloud shell).
 */
export function bootstrapGitea(
  _giteaUrl: string,
  adminUser: string,
  adminPass: string,
  log: (msg: string) => void,
): string {
  log('  Bootstrapping Gitea...');

  // Ensure the admin can actually log in via API. The Gitea chart creates
  // the admin with `must_change_password=true` by default — first login via
  // basic auth would then 401 with "You must change your password". We reset
  // the flag via `gitea admin user change-password` (idempotent, works even
  // if password is already correct).
  try {
    execSync(
      `kubectl exec -n gitea deploy/gitea -c gitea -- ` +
      `gitea admin user change-password ` +
      `--username ${adminUser} --password ${adminPass} ` +
      `--must-change-password=false`,
      { stdio: 'pipe', timeout: 15_000 },
    );
    log('  ✓ Gitea admin password confirmed');
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(`  ⚠ Could not reset Gitea admin password: ${msg.slice(0, 80)}`);
    log('    Continuing — basic auth may fail if password was changed.');
  }

  return withPortForward(
    'gitea', 'svc/gitea-http', 3000,
    (localUrl) => {
      // Wait for Gitea to be ready (it may still be migrating)
      const ready = waitForHttp(`${localUrl}/api/v1/version`, 60, log);
      if (!ready) {
        log('  ⚠ Gitea did not become ready — skipping bootstrap');
        return '';
      }

      // Create "loopzero" org (idempotent — 409 if exists)
      try {
        execSync(
          `curl -sf -X POST -u '${adminUser}:${adminPass}' ` +
          `'${localUrl}/api/v1/orgs' ` +
          `-H 'Content-Type: application/json' ` +
          `-d '{"username":"loopzero","visibility":"private"}'`,
          { stdio: 'pipe', timeout: 10_000 },
        );
        log('  ✓ Gitea org "loopzero" created');
      } catch {
        log('  ✓ Gitea org "loopzero" exists');
      }

      // Delete stale tokens with same prefix (idempotent re-run)
      try {
        const tokensJson = execSync(
          `curl -sf -u '${adminUser}:${adminPass}' '${localUrl}/api/v1/users/${adminUser}/tokens'`,
          { encoding: 'utf-8', timeout: 10_000 },
        );
        const tokens = JSON.parse(tokensJson) as { id: number; name: string }[];
        for (const t of tokens) {
          if (t.name.startsWith('loopzero-platform-')) {
            execSync(
              `curl -sf -X DELETE -u '${adminUser}:${adminPass}' '${localUrl}/api/v1/users/${adminUser}/tokens/${t.id}'`,
              { stdio: 'pipe', timeout: 5_000 },
            );
          }
        }
      } catch {
        // Non-fatal — stale tokens just waste space
      }

      // Generate admin token with explicit scopes
      const tokenName = `loopzero-platform-${Date.now()}`;
      try {
        const resp = execSync(
          `curl -sf -X POST -u '${adminUser}:${adminPass}' ` +
          `'${localUrl}/api/v1/users/${adminUser}/tokens' ` +
          `-H 'Content-Type: application/json' ` +
          `-d '${JSON.stringify({
            name: tokenName,
            scopes: [
              'write:organization',
              'write:repository',
              'write:user',
              'read:organization',
              'read:repository',
              'read:user',
              'write:admin',
              'read:admin',
            ],
          })}'`,
          { encoding: 'utf-8', timeout: 10_000 },
        );
        const parsed = JSON.parse(resp);
        const token = parsed.sha1 || parsed.token || '';
        if (!token) {
          log(`  ⚠ Gitea token response missing sha1: ${JSON.stringify(parsed).slice(0, 100)}`);
          return '';
        }
        log('  ✓ Gitea API token generated');
        return token;
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        log(`  ✗ Gitea token generation failed: ${msg.slice(0, 80)}`);
        return '';
      }
    },
  );
}

// ─── ArgoCD bootstrap ───────────────────────────────────────────────────────

/**
 * Bootstrap ArgoCD: extract initial admin password from K8s secret.
 */
export function bootstrapArgoCD(namespace: string, log: (msg: string) => void): string {
  log('  Extracting ArgoCD admin credentials...');
  try {
    const pass = execSync(
      `kubectl get secret -n ${namespace} argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d`,
      { encoding: 'utf-8', timeout: 10_000 },
    ).trim();
    if (pass) {
      log('  ✓ ArgoCD admin password extracted');
    } else {
      log('  ⚠ ArgoCD admin password is empty');
    }
    return pass;
  } catch {
    log('  ⚠ ArgoCD admin secret not found — ArgoCD may not be installed');
    return '';
  }
}

// ─── PostgreSQL (CloudNativePG) credentials ────────────────────────────────

/**
 * Extract PostgreSQL connection URL from the CloudNativePG-generated secret.
 *
 * ADR-0012: After `cnpg/cluster` chart installs, CNPG operator generates
 * a secret named `<release>-cluster-app` containing username, password,
 * and a ready-to-use `uri` field. Waits for the Cluster CR to reach
 * healthy state before extracting credentials.
 *
 * The release name is `postgresql` (prerequisites.ts), so the Cluster CR
 * is named `postgresql-cluster` and the secret is `postgresql-cluster-app`.
 *
 * Returns `null` if the cluster did not become healthy or the secret is
 * missing — caller should treat as fatal and surface a clear error.
 */
export function extractPostgresCredentials(
  namespace: string,
  log: (msg: string) => void,
): { databaseUrl: string; username: string; password: string } | null {
  const clusterName = 'postgresql-cluster';
  const secretName = `${clusterName}-app`;

  log('  Waiting for PostgreSQL cluster to become healthy...');
  try {
    // CNPG exposes a `Ready` condition on the Cluster CR once primary is up.
    execSync(
      `kubectl wait --for=condition=Ready cluster/${clusterName} -n ${namespace} --timeout=300s`,
      { encoding: 'utf-8', timeout: 320_000, stdio: 'pipe' },
    );
  } catch {
    // Fallback: poll status.phase until "Cluster in healthy state"
    const deadline = Date.now() + 300_000;
    let healthy = false;
    while (Date.now() < deadline) {
      try {
        const phase = execSync(
          `kubectl get cluster ${clusterName} -n ${namespace} -o jsonpath='{.status.phase}' 2>/dev/null`,
          { encoding: 'utf-8', timeout: 10_000 },
        ).trim();
        if (phase === 'Cluster in healthy state') {
          healthy = true;
          break;
        }
      } catch {
        // keep polling
      }
      execSync('sleep 5');
    }
    if (!healthy) {
      log('  ✗ PostgreSQL cluster did not become healthy within 5 min');
      return null;
    }
  }

  try {
    const b64 = (field: string): string =>
      execSync(
        `kubectl get secret -n ${namespace} ${secretName} -o jsonpath='{.data.${field}}' 2>/dev/null | base64 -d`,
        { encoding: 'utf-8', timeout: 10_000 },
      ).trim();

    const username = b64('username');
    const password = b64('password');
    const rawUri = b64('uri');

    if (!username || !password || !rawUri) {
      log(`  ✗ PostgreSQL secret ${secretName} missing fields`);
      return null;
    }

    // CNPG uri is `postgresql://USER:PASS@HOST:5432/DB`. Add sslmode=disable
    // for in-cluster traffic (CNPG enables TLS on wire but Go pgx defaults
    // to require which fails on self-signed — disable keeps minimal install
    // working; production installs can override via config.database.url).
    const databaseUrl = rawUri.includes('?')
      ? `${rawUri}&sslmode=disable`
      : `${rawUri}?sslmode=disable`;

    log('  ✓ PostgreSQL credentials extracted from CNPG secret');
    return { databaseUrl, username, password };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(`  ✗ Failed to read PostgreSQL secret: ${msg.slice(0, 80)}`);
    return null;
  }
}

// ─── MinIO credentials ──────────────────────────────────────────────────────

/**
 * Extract MinIO root credentials from Helm release or K8s secret.
 */
export function extractMinioCredentials(
  log: (msg: string) => void,
): { accessKey: string; secretKey: string } {
  // MinIO Helm chart creates a secret named "minio" in its namespace
  try {
    const accessKey = execSync(
      `kubectl get secret -n loopzero-system minio -o jsonpath='{.data.rootUser}' 2>/dev/null | base64 -d`,
      { encoding: 'utf-8', timeout: 5_000 },
    ).trim();
    const secretKey = execSync(
      `kubectl get secret -n loopzero-system minio -o jsonpath='{.data.rootPassword}' 2>/dev/null | base64 -d`,
      { encoding: 'utf-8', timeout: 5_000 },
    ).trim();
    if (accessKey && secretKey) {
      log('  ✓ MinIO credentials extracted');
      return { accessKey, secretKey };
    }
  } catch {
    // Try alternative secret format
  }

  // Fallback: try the default MinIO credentials
  log('  ⚠ MinIO secret not found — using defaults (minioadmin/minioadmin)');
  return { accessKey: 'minioadmin', secretKey: 'minioadmin' };
}

// ─── Hydra OAuth2 client ────────────────────────────────────────────────────

/**
 * Create Hydra OAuth2 client for loopzero-cli.
 * Uses kubectl exec to call Hydra admin API (no port-forward needed).
 */
export function createHydraClient(log: (msg: string) => void): string {
  log('  Creating OAuth2 client...');

  // Call Hydra admin API via kubectl exec (pod-to-pod, no port-forward)
  const hydraAdminUrl = 'http://ory-hydra-admin.ory.svc.cluster.local:4445';

  try {
    // Check if client already exists
    const existingResp = execSync(
      `kubectl exec -n ory deploy/ory-hydra -- wget -q -O - '${hydraAdminUrl}/admin/clients' 2>/dev/null || echo '[]'`,
      { encoding: 'utf-8', timeout: 15_000 },
    );
    const clients = JSON.parse(existingResp.trim() || '[]') as { client_id: string; client_name: string }[];
    const existing = clients.find((c) => c.client_name === 'loopzero-cli');
    if (existing) {
      log(`  ✓ OAuth2 client exists: ${existing.client_id}`);
      return existing.client_id;
    }
  } catch {
    // If listing fails, proceed to create
  }

  try {
    const clientConfig = JSON.stringify({
      client_name: 'loopzero-cli',
      grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'],
      scope: 'openid offline_access',
      redirect_uris: ['http://localhost:11811/callback'],
      audience: ['loopzero-engine'],
      token_endpoint_auth_method: 'none',
    });

    // Write config to a temp file in the pod, then POST it
    const resp = execSync(
      `kubectl exec -n ory deploy/ory-hydra -- sh -c ` +
      `'echo '"'"'${clientConfig}'"'"' | wget -q -O - --post-data=@- ` +
      `--header="Content-Type: application/json" ` +
      `${hydraAdminUrl}/admin/clients'`,
      { encoding: 'utf-8', timeout: 15_000 },
    );
    const parsed = JSON.parse(resp.trim());
    const clientId = parsed.client_id || '';
    if (clientId) {
      log(`  ✓ OAuth2 client created: ${clientId}`);
    } else {
      log('  ⚠ OAuth2 client created but client_id missing');
    }
    return clientId;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(`  ⚠ OAuth2 client creation failed: ${msg.slice(0, 80)}`);
    log('    This can be created manually after install');
    return '';
  }
}

// ─── Runtime secrets ────────────────────────────────────────────────────────

/**
 * Create the MinIO credentials secret required by build CWTs.
 * The CWT templates reference `loopzero-minio-credentials` with fields:
 *   MINIO_ENDPOINT, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
 */
export function createMinioCredentialsSecret(
  namespace: string,
  minioEndpoint: string,
  accessKey: string,
  secretKey: string,
  log: (msg: string) => void,
): void {
  try {
    const cmd = [
      `kubectl create secret generic loopzero-minio-credentials`,
      `--namespace ${namespace}`,
      `--from-literal=MINIO_ENDPOINT=${minioEndpoint}`,
      `--from-literal=AWS_ACCESS_KEY_ID=${accessKey}`,
      `--from-literal=AWS_SECRET_ACCESS_KEY=${secretKey}`,
      `--dry-run=client -o yaml | kubectl apply -f -`,
    ].join(' ');
    execSync(cmd, { stdio: 'pipe', timeout: 10_000 });
    log(`  ✓ MinIO credentials secret created in ${namespace}`);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(`  ⚠ MinIO credentials secret failed: ${msg.slice(0, 80)}`);
  }
}

// ─── Kubeconfig secrets ─────────────────────────────────────────────────────

/**
 * Create kubeconfig secrets for build/workload cluster access.
 * Single-cluster mode: uses current cluster's internal API endpoint.
 * Multi-cluster mode: customer provides kubeconfig paths.
 */
export function createKubeconfigs(
  config: PlatformConfig,
  log: (msg: string) => void,
): void {
  log('  Configuring cluster access...');

  const clusterMode = config.clusters?.mode || 'single';

  if (clusterMode === 'single') {
    // Single-cluster: create configmaps with in-cluster API server reference.
    // Pods use their ServiceAccount token for in-cluster auth, so we don't
    // need real credentials — just a valid kubeconfig that the loopzero-api
    // code can read.
    //
    // IMPORTANT: loopzero-api / operators look up contexts BY NAME. Defaults:
    //   - MGMT_KUBE_CONTEXT     = k3d-loopzero-mgmt
    //   - BUILD_KUBE_CONTEXT    = k3d-loopzero-build
    //   - RUNTIME_KUBE_CONTEXT  = k3d-loopzero-workload
    // In single-cluster mode we point all three names at the same cluster.
    // A production install should override via env (BUILD_KUBE_CONTEXT,
    // RUNTIME_KUBE_CONTEXT) in the chart values to match customer-named
    // contexts.
    try {
      const apiServer = execSync(
        `kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'`,
        { encoding: 'utf-8', timeout: 5_000 },
      ).trim();

      const kubeconfig = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Config',
        clusters: [
          { name: 'local', cluster: { server: apiServer, 'insecure-skip-tls-verify': true } },
        ],
        contexts: [
          { name: 'k3d-loopzero-mgmt', context: { cluster: 'local' } },
          { name: 'k3d-loopzero-build', context: { cluster: 'local' } },
          { name: 'k3d-loopzero-workload', context: { cluster: 'local' } },
          { name: 'local', context: { cluster: 'local' } },
        ],
        'current-context': 'local',
      });

      for (const name of ['loopzero-build-kubeconfig', 'loopzero-runtime-kubeconfig']) {
        execSync(
          `kubectl create configmap ${name} -n loopzero-system ` +
          `--from-literal=config='${kubeconfig}' ` +
          `--dry-run=client -o yaml | kubectl apply -f -`,
          { stdio: 'pipe', timeout: 10_000 },
        );
      }
      log('  ✓ Kubeconfig configmaps created (single-cluster mode, 3 contexts)');
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log(`  ⚠ Kubeconfig creation failed: ${msg.slice(0, 80)}`);
    }
  } else {
    // Multi-cluster: customer provides kubeconfig files
    const buildKc = config.clusters?.build?.kubeconfigPath;
    const workloadKc = config.clusters?.workload?.kubeconfigPath;

    if (buildKc) {
      try {
        execSync(
          `kubectl create configmap loopzero-build-kubeconfig -n loopzero-system ` +
          `--from-file=config=${buildKc} ` +
          `--dry-run=client -o yaml | kubectl apply -f -`,
          { stdio: 'pipe', timeout: 10_000 },
        );
        log('  ✓ Build cluster kubeconfig created');
      } catch {
        log('  ⚠ Build cluster kubeconfig creation failed');
      }
    }

    if (workloadKc) {
      try {
        execSync(
          `kubectl create configmap loopzero-runtime-kubeconfig -n loopzero-system ` +
          `--from-file=config=${workloadKc} ` +
          `--dry-run=client -o yaml | kubectl apply -f -`,
          { stdio: 'pipe', timeout: 10_000 },
        );
        log('  ✓ Workload cluster kubeconfig created');
      } catch {
        log('  ⚠ Workload cluster kubeconfig creation failed');
      }
    }

    if (!buildKc || !workloadKc) {
      log('  ⚠ Multi-cluster mode requires clusters.build.kubeconfigPath and clusters.workload.kubeconfigPath');
    }
  }
}

// ─── Auth databases ─────────────────────────────────────────────────────────

/**
 * Create Hydra and Kratos databases and grant them to the loopzero user.
 * Idempotent — checks existence before CREATE.
 *
 * Uses `kubectl exec` into the CNPG primary pod rather than connecting
 * from the host, because the in-cluster service DNS
 * (postgresql-cluster-rw.loopzero-system.svc.cluster.local) does NOT resolve
 * from outside the cluster. The previous `psql "<url>"` call silently
 * failed for this reason — auth pods then crash-looped with
 * `database "kratos" does not exist`.
 *
 * For external DBs (config.database.external=true + database.url), the
 * customer is expected to have created kratos/hydra databases already.
 * We detect external mode via the URL hostname and skip in-cluster creation.
 */
export function createAuthDatabases(databaseUrl: string, log: (msg: string) => void): void {
  log('  Creating auth databases...');

  // If URL hostname is NOT the internal CNPG service, the DB is external —
  // customer owns DB creation and grants.
  const isInternal = databaseUrl.includes('postgresql-cluster-rw')
    || databaseUrl.includes('postgresql.loopzero-system');
  if (!isInternal) {
    log('  ⚠ External database detected — skipping auth DB creation.');
    log('    Customer must create databases `kratos` and `hydra` and grant');
    log('    access to the loopzero user before install.');
    return;
  }

  // Find the CNPG primary pod. CNPG labels pods with cnpg.io/cluster=<name>
  // and cnpg.io/instanceRole=primary.
  let primaryPod = '';
  try {
    primaryPod = execSync(
      `kubectl get pods -n loopzero-system ` +
      `-l cnpg.io/cluster=postgresql-cluster,cnpg.io/instanceRole=primary ` +
      `-o jsonpath='{.items[0].metadata.name}' 2>/dev/null`,
      { encoding: 'utf-8', timeout: 10_000 },
    ).trim();
  } catch {
    // fall through to error handling
  }

  if (!primaryPod) {
    log('  ✗ CNPG primary pod not found — cannot create auth databases');
    log('    Inspect: kubectl get pods -n loopzero-system -l cnpg.io/cluster=postgresql-cluster');
    return;
  }

  for (const db of ['kratos', 'hydra']) {
    try {
      // Check existence first (idempotent); CREATE + GRANT if missing.
      const exists = execSync(
        `kubectl exec -n loopzero-system ${primaryPod} -c postgres -- ` +
        `psql -U postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${db}'" 2>/dev/null`,
        { encoding: 'utf-8', timeout: 15_000 },
      ).trim();

      if (exists !== '1') {
        execSync(
          `kubectl exec -n loopzero-system ${primaryPod} -c postgres -- ` +
          `psql -U postgres -c "CREATE DATABASE ${db} OWNER loopzero"`,
          { encoding: 'utf-8', timeout: 15_000, stdio: 'pipe' },
        );
        log(`  ✓ Created database ${db} (owner: loopzero)`);
      } else {
        // Ensure ownership (in case DB pre-existed with different owner)
        execSync(
          `kubectl exec -n loopzero-system ${primaryPod} -c postgres -- ` +
          `psql -U postgres -c "ALTER DATABASE ${db} OWNER TO loopzero"`,
          { encoding: 'utf-8', timeout: 10_000, stdio: 'pipe' },
        );
        log(`  ✓ Database ${db} exists (owner ensured: loopzero)`);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log(`  ✗ Failed to prepare database ${db}: ${msg.slice(0, 120)}`);
    }
  }
}

// ─── Admin user seed ────────────────────────────────────────────────────────

/**
 * Seed initial admin user in the loopzero database.
 * Idempotent (ON CONFLICT DO NOTHING).
 */
export function seedAdminUser(
  databaseUrl: string,
  adminEmail: string,
  log: (msg: string) => void,
): void {
  log('  Creating admin user...');
  try {
    // Use the API pod's psql or direct psql
    const sql = `INSERT INTO users (id, email, display_name, username) VALUES (gen_random_uuid()::text, '${adminEmail}', 'Admin', 'admin') ON CONFLICT (email) DO NOTHING;`;
    execSync(`psql "${databaseUrl}" -c "${sql}" 2>/dev/null`, {
      stdio: 'pipe',
      timeout: 10_000,
    });
    log(`  ✓ Admin user seeded: ${adminEmail}`);
  } catch {
    log('  ⚠ Admin user seeding skipped — will be created on first login');
  }
}

// ─── Utilities ──────────────────────────────────────────────────────────────

/**
 * Generate a cryptographically secure random secret.
 */
export function generateSecret(length: number): string {
  return crypto.randomBytes(length).toString('base64url').slice(0, length);
}

/**
 * Wait for a local HTTP endpoint to respond 2xx/3xx. Used with
 * `withPortForward` to probe a service that we just opened a tunnel to.
 * Returns true on success, false on timeout.
 */
function waitForHttp(url: string, timeoutSec: number, log: (msg: string) => void): boolean {
  const deadline = Date.now() + timeoutSec * 1000;
  while (Date.now() < deadline) {
    try {
      const code = execSync(
        `curl -sf -o /dev/null -w "%{http_code}" --max-time 3 "${url}" 2>/dev/null || echo 000`,
        { encoding: 'utf-8', timeout: 5_000 },
      ).trim();
      if (code.startsWith('2') || code.startsWith('3')) {
        return true;
      }
    } catch {
      // continue retrying
    }
    execSync('sleep 2');
  }
  log(`  ⚠ Endpoint ${url} did not respond within ${timeoutSec}s`);
  return false;
}

/**
 * Run a callback with a short-lived `kubectl port-forward` tunnel open.
 * Allocates a random local port in 20000–29999, starts the forward in
 * background, waits briefly for it to be ready, invokes `fn(localUrl)`,
 * then tears down the forward in a finally block.
 *
 * This is the canonical pattern for installer operations that need to
 * call an in-cluster service from outside the cluster (Gitea bootstrap,
 * Hydra client registration, etc.).
 */
export function withPortForward<T>(
  namespace: string,
  resource: string, // e.g. "svc/gitea-http" or "deploy/ory-hydra"
  remotePort: number,
  fn: (localUrl: string) => T,
): T {
  const localPort = 20000 + Math.floor(Math.random() * 10000);
  const logFile = `/tmp/pf-${namespace}-${resource.replace('/', '-')}-${localPort}.log`;

  // Launch port-forward in background; detach from parent so exit doesn't reap it
  const child = execSync(
    `nohup kubectl port-forward -n ${namespace} ${resource} ${localPort}:${remotePort} ` +
    `> ${logFile} 2>&1 & echo $!`,
    { encoding: 'utf-8', timeout: 5_000 },
  ).trim();
  const pid = parseInt(child, 10);

  try {
    // Wait for the port to be listening
    const ready = waitForHttp(`http://127.0.0.1:${localPort}`, 15, () => { /* silent */ });
    if (!ready) {
      // Endpoint didn't respond 2xx/3xx, but for some services (gitea /)
      // the root returns 200 only after setup. Try a short delay anyway —
      // the caller's own probe (waitForHttp on the API version endpoint)
      // will provide the authoritative readiness check.
      execSync('sleep 2');
    }
    return fn(`http://127.0.0.1:${localPort}`);
  } finally {
    try {
      execSync(`kill ${pid} 2>/dev/null || true`, { stdio: 'pipe', timeout: 3_000 });
    } catch {
      // best-effort teardown
    }
  }
}
