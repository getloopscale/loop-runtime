// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP — T4.1
// Programmatic health checks invoked between phases of the platform install
// flow. Wraps `infra/helmfile/scripts/health-check.sh` for parity with the
// hooks-based execution path.

import { spawnSync } from 'node:child_process';
import { join } from 'node:path';
import { resolveProjectRoot } from './pulumi-runner';

export type HealthCheckRole = 'infra' | 'mgmt' | 'build' | 'runtime' | 'wire';

export interface HealthCheckResult {
  role: HealthCheckRole;
  status: 'pass' | 'fail';
  durationMs: number;
  exitCode: number;
  stderr?: string;
}

export interface HealthCheckOptions {
  domain?: string;
  retries?: number;
  retryIntervalSec?: number;
  extraEnv?: Record<string, string>;
}

export function runHealthCheck(
  role: HealthCheckRole,
  opts: HealthCheckOptions = {},
): HealthCheckResult {
  const projectRoot = resolveProjectRoot();
  const scriptPath = join(projectRoot, 'infra', 'helmfile', 'scripts', 'health-check.sh');
  const args = [scriptPath, role];
  if (opts.domain) args.push(opts.domain);

  const start = Date.now();
  const child = spawnSync('bash', args, {
    env: {
      ...process.env,
      ...(opts.retries ? { HEALTH_CHECK_RETRIES: String(opts.retries) } : {}),
      ...(opts.retryIntervalSec ? { HEALTH_CHECK_INTERVAL_SEC: String(opts.retryIntervalSec) } : {}),
      ...(opts.extraEnv ?? {}),
    },
    stdio: 'pipe',
  });
  const durationMs = Date.now() - start;

  if (child.status !== 0) {
    return {
      role,
      status: 'fail',
      durationMs,
      exitCode: child.status ?? 1,
      stderr: child.stderr?.toString(),
    };
  }
  return { role, status: 'pass', durationMs, exitCode: 0 };
}

/**
 * Run health checks for ALL roles in install order, stopping on first failure.
 */
export function runFullHealthCheck(
  opts: HealthCheckOptions = {},
): { ok: boolean; results: HealthCheckResult[] } {
  const roles: HealthCheckRole[] = ['infra', 'build', 'mgmt', 'runtime', 'wire'];
  const results: HealthCheckResult[] = [];
  for (const role of roles) {
    const r = runHealthCheck(role, opts);
    results.push(r);
    if (r.status !== 'pass') {
      return { ok: false, results };
    }
  }
  return { ok: true, results };
}
