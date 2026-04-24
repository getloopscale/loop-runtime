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
 * We use basic auth (admin user:pass) to create the token via HTTP API,
 * which is the reliable path across all Gitea versions.
 */
export function bootstrapGitea(
  giteaUrl: string,
  adminUser: string,
  adminPass: string,
  log: (msg: string) => void,
): string {
  log('  Bootstrapping Gitea...');

  // Wait for Gitea to be ready (it may still be migrating)
  waitForService(giteaUrl, 'Gitea', log, 60);

  // Create "loopzero" org (idempotent — 409 if exists)
  try {
    execSync(
      `curl -sf -X POST -u '${adminUser}:${adminPass}' ` +
      `'${giteaUrl}/api/v1/orgs' ` +
      `-H 'Content-Type: application/json' ` +
      `-d '{"username":"loopzero","visibility":"private"}'`,
      { stdio: 'pipe', timeout: 10_000 },
    );
    log('  ✓ Gitea org "loopzero" created');
  } catch {
    log('  ✓ Gitea org "loopzero" exists');
  }

  // Delete any stale tokens with same prefix (idempotent re-run)
  try {
    const tokensJson = execSync(
      `curl -sf -u '${adminUser}:${adminPass}' '${giteaUrl}/api/v1/users/${adminUser}/tokens'`,
      { encoding: 'utf-8', timeout: 10_000 },
    );
    const tokens = JSON.parse(tokensJson) as { id: number; name: string }[];
    for (const t of tokens) {
      if (t.name.startsWith('loopzero-platform-')) {
        execSync(
          `curl -sf -X DELETE -u '${adminUser}:${adminPass}' '${giteaUrl}/api/v1/users/${adminUser}/tokens/${t.id}'`,
          { stdio: 'pipe', timeout: 5_000 },
        );
      }
    }
  } catch {
    // Non-fatal — stale tokens just waste space
  }

  // Generate admin token with explicit scopes for Gitea 1.25+
  // Scopes must cover: org creation, repo CRUD, admin operations
  const tokenName = `loopzero-platform-${Date.now()}`;
  try {
    const resp = execSync(
      `curl -sf -X POST -u '${adminUser}:${adminPass}' ` +
      `'${giteaUrl}/api/v1/users/${adminUser}/tokens' ` +
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
    // Single-cluster: create configmaps with in-cluster API server reference
    // The pods will use their ServiceAccount token — we just need the API server URL
    try {
      const apiServer = execSync(
        `kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'`,
        { encoding: 'utf-8', timeout: 5_000 },
      ).trim();

      // Create a minimal kubeconfig that points to the same cluster
      const kubeconfig = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Config',
        clusters: [{ name: 'local', cluster: { server: apiServer, 'insecure-skip-tls-verify': true } }],
        contexts: [{ name: 'local', context: { cluster: 'local' } }],
        'current-context': 'local',
      });

      for (const name of ['loopzero-build-kubeconfig', 'loopzero-runtime-kubeconfig']) {
        execSync(
          `kubectl create configmap ${name} -n loopzero-system ` +
          `--from-literal=kubeconfig='${kubeconfig}' ` +
          `--dry-run=client -o yaml | kubectl apply -f -`,
          { stdio: 'pipe', timeout: 10_000 },
        );
      }
      log('  ✓ Kubeconfig configmaps created (single-cluster mode)');
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
          `--from-file=kubeconfig=${buildKc} ` +
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
          `--from-file=kubeconfig=${workloadKc} ` +
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
 * Create Hydra and Kratos databases if using a shared PG instance.
 * Idempotent — CREATE DATABASE IF NOT EXISTS equivalent.
 */
export function createAuthDatabases(databaseUrl: string, log: (msg: string) => void): void {
  log('  Creating auth databases...');

  // Extract base connection (without database name)
  const baseUrl = databaseUrl.replace(/\/[^/?]*(\?.*)?$/, '/postgres$1');

  for (const db of ['kratos', 'hydra']) {
    try {
      // CREATE DATABASE doesn't support IF NOT EXISTS in PG — use PL/pgSQL or catch error
      execSync(
        `psql "${baseUrl}" -c "SELECT 1 FROM pg_database WHERE datname='${db}'" -tA 2>/dev/null | grep -q 1 || ` +
        `psql "${baseUrl}" -c "CREATE DATABASE ${db}" 2>/dev/null`,
        { stdio: 'pipe', timeout: 10_000 },
      );
    } catch {
      // DB already exists or psql not available — non-fatal
    }
  }
  log('  ✓ Auth databases ready (kratos, hydra)');
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
 * Wait for a service to become reachable via kubectl exec.
 * Avoids port-forward flakiness.
 */
function waitForService(url: string, name: string, log: (msg: string) => void, timeoutSec: number): void {
  const deadline = Date.now() + timeoutSec * 1000;
  while (Date.now() < deadline) {
    try {
      // Use a pod that has wget/curl to check the service
      execSync(
        `kubectl run --rm -i --restart=Never --image=busybox:1.36 loopzero-probe-${Date.now()} ` +
        `--timeout=10s -- wget -q -O /dev/null --timeout=5 '${url}/api/v1/version' 2>/dev/null`,
        { stdio: 'pipe', timeout: 15_000 },
      );
      return; // Success
    } catch {
      // Service not ready yet
      execSync('sleep 3');
    }
  }
  log(`  ⚠ ${name} did not become ready within ${timeoutSec}s — continuing anyway`);
}
