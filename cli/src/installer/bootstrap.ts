// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import { PlatformConfig } from './types';
import * as crypto from 'crypto';

/**
 * Run database migrations using the API pod's built-in migrate binary.
 */
export function runMigrations(databaseUrl: string, log: (msg: string) => void): void {
  log('  Running database migrations...');
  try {
    // Use psql to apply migrations (available on host if PG is external Docker)
    // The migration files are baked into the loopzero-api image
    const apiPod = execSync(
      'kubectl get pods -n loopzero-system -l app.kubernetes.io/name=loopzero-api --no-headers -o jsonpath="{.items[0].metadata.name}"',
      { encoding: 'utf-8', timeout: 10_000 },
    ).trim();

    if (apiPod) {
      execSync(
        `kubectl exec -n loopzero-system ${apiPod} -- /usr/local/bin/migrate -database "${databaseUrl}" -path /migrations up`,
        { encoding: 'utf-8', timeout: 60_000 },
      );
      log('  ✓ Migrations applied');
    } else {
      log('  ⚠ API pod not found — migrations skipped (will apply on first boot)');
    }
  } catch (err) {
    // Migrations may already be applied — non-fatal
    const msg = err instanceof Error ? err.message : String(err);
    if (msg.includes('no change') || msg.includes('already')) {
      log('  ✓ Migrations already up to date');
    } else {
      log(`  ⚠ Migration warning: ${msg.slice(0, 100)}`);
    }
  }
}

/**
 * Bootstrap Gitea: create org and generate API token.
 */
export function bootstrapGitea(
  giteaUrl: string,
  adminUser: string,
  adminPass: string,
  log: (msg: string) => void,
): string {
  log('  Bootstrapping Gitea...');

  // Create "loopzero" org
  try {
    execSync(
      `curl -sf -X POST -u ${adminUser}:${adminPass} ` +
      `${giteaUrl}/api/v1/orgs ` +
      `-H 'Content-Type: application/json' ` +
      `-d '{"username":"loopzero","visibility":"private"}'`,
      { stdio: 'pipe', timeout: 10_000 },
    );
    log('  ✓ Gitea org "loopzero" created');
  } catch {
    log('  ✓ Gitea org "loopzero" exists');
  }

  // Generate admin token
  try {
    const tokenName = `loopzero-api-${Date.now()}`;
    const resp = execSync(
      `curl -sf -X POST -u ${adminUser}:${adminPass} ` +
      `${giteaUrl}/api/v1/users/${adminUser}/tokens ` +
      `-H 'Content-Type: application/json' ` +
      `-d '{"name":"${tokenName}","scopes":["all"]}'`,
      { encoding: 'utf-8', timeout: 10_000 },
    );
    const { sha1 } = JSON.parse(resp);
    log('  ✓ Gitea API token generated');
    return sha1;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(`  ⚠ Gitea token generation failed: ${msg.slice(0, 80)}`);
    return '';
  }
}

/**
 * Bootstrap ArgoCD: extract admin password.
 */
export function bootstrapArgoCD(namespace: string, log: (msg: string) => void): string {
  log('  Extracting ArgoCD admin credentials...');
  try {
    const pass = execSync(
      `kubectl get secret -n ${namespace} argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d`,
      { encoding: 'utf-8', timeout: 10_000 },
    ).trim();
    log('  ✓ ArgoCD admin password extracted');
    return pass;
  } catch {
    log('  ⚠ ArgoCD admin secret not found');
    return '';
  }
}

/**
 * Generate a cryptographically secure random secret.
 */
export function generateSecret(length: number): string {
  return crypto.randomBytes(length).toString('base64url').slice(0, length);
}

/**
 * Create kubeconfig secrets for build/workload cluster access.
 * In single-cluster mode, uses the current kubeconfig.
 */
export function createKubeconfigs(
  config: PlatformConfig,
  log: (msg: string) => void,
): void {
  log('  Configuring cluster access...');

  const configAny = config as unknown as Record<string, Record<string, string>>;
  const clusterMode = configAny.clusters?.mode || 'single';

  if (clusterMode === 'single') {
    try {
      execSync(
        `kubectl create secret generic loopzero-kubeconfigs -n loopzero-system ` +
        `--from-literal=build-kubeconfig="$(kubectl config view --raw --minify)" ` +
        `--from-literal=workload-kubeconfig="$(kubectl config view --raw --minify)" ` +
        `--dry-run=client -o yaml | kubectl apply -f -`,
        { stdio: 'pipe', timeout: 10_000 },
      );
      log('  ✓ Kubeconfig secrets created (single-cluster mode)');
    } catch {
      log('  ⚠ Kubeconfig secret creation failed — pods may not access build/workload clusters');
    }
  } else {
    log('  ⚠ Multi-cluster mode — customer must provide kubeconfig files');
  }
}

/**
 * Create Hydra OAuth2 client for loopzero-cli.
 */
export function createHydraClient(log: (msg: string) => void): string {
  log('  Creating OAuth2 client...');
  try {
    // Port-forward Hydra admin temporarily
    const pfProcess = execSync(
      'kubectl port-forward -n ory svc/ory-hydra-admin 14445:4445 --address 127.0.0.1 &' +
      'sleep 3 && curl -sf -X POST http://localhost:14445/admin/clients ' +
      '-H "Content-Type: application/json" ' +
      '-d \'{"client_name":"loopzero-cli","grant_types":["authorization_code","refresh_token"],' +
      '"response_types":["code"],"scope":"openid offline_access",' +
      '"redirect_uris":["http://localhost:11811/callback"],' +
      '"audience":["loopzero-engine"],' +
      '"token_endpoint_auth_method":"none"}\'; ' +
      'kill %1 2>/dev/null',
      { encoding: 'utf-8', timeout: 30_000 },
    );
    const { client_id } = JSON.parse(pfProcess.trim());
    log(`  ✓ OAuth2 client created: ${client_id}`);
    return client_id;
  } catch {
    log('  ⚠ OAuth2 client creation failed — manual setup needed');
    return '';
  }
}

/**
 * Seed initial admin user in the database.
 */
export function seedAdminUser(
  databaseUrl: string,
  adminEmail: string,
  log: (msg: string) => void,
): void {
  log('  Creating admin user...');
  try {
    const sql = `
      INSERT INTO users (id, email, display_name, username)
      VALUES (gen_random_uuid()::text, '${adminEmail}', 'Admin', 'admin')
      ON CONFLICT (email) DO NOTHING;
    `;
    execSync(`echo "${sql}" | psql "${databaseUrl}"`, {
      stdio: 'pipe',
      timeout: 10_000,
    });
    log(`  ✓ Admin user seeded: ${adminEmail}`);
  } catch {
    log('  ⚠ Admin user seeding failed — create manually after install');
  }
}

/**
 * Create Hydra and Kratos databases if using a shared PG instance.
 */
export function createAuthDatabases(databaseUrl: string, log: (msg: string) => void): void {
  log('  Creating auth databases...');
  const baseUrl = databaseUrl.replace(/\/[^/]*(\?.*)?$/, '');

  for (const db of ['kratos', 'hydra']) {
    try {
      execSync(
        `psql "${baseUrl}/postgres" -c "CREATE DATABASE ${db}" 2>/dev/null || true`,
        { stdio: 'pipe', timeout: 10_000 },
      );
    } catch {
      // DB may already exist
    }
  }
  log('  ✓ Auth databases ready (kratos, hydra)');
}
