// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';

/**
 * Execute a Helm command and return stdout.
 * Throws on non-zero exit code.
 */
export function helm(args: string, opts?: { cwd?: string; timeout?: number }): string {
  const timeout = opts?.timeout ?? 300_000; // 5 min default
  return execSync(`helm ${args}`, {
    encoding: 'utf-8',
    timeout,
    cwd: opts?.cwd,
    stdio: ['pipe', 'pipe', 'pipe'],
  }).trim();
}

/**
 * Check if a Helm release exists in a namespace.
 */
export function releaseExists(name: string, namespace: string): boolean {
  try {
    helm(`status ${name} -n ${namespace} --output json`);
    return true;
  } catch {
    return false;
  }
}

/**
 * Install or upgrade a Helm release.
 */
export function installOrUpgrade(opts: {
  name: string;
  chart: string;
  version: string;
  namespace: string;
  createNamespace?: boolean;
  values?: Record<string, unknown>;
  wait?: boolean;
  timeout?: string;
}): void {
  const args = [
    `upgrade --install ${opts.name} ${opts.chart}`,
    `--version ${opts.version}`,
    `-n ${opts.namespace}`,
  ];

  if (opts.createNamespace) args.push('--create-namespace');
  if (opts.wait !== false) args.push('--wait');
  if (opts.timeout) args.push(`--timeout ${opts.timeout}`);

  if (opts.values) {
    for (const [key, value] of Object.entries(opts.values)) {
      const v = typeof value === 'string' ? value : JSON.stringify(value);
      args.push(`--set ${key}=${v}`);
    }
  }

  helm(args.join(' '), { timeout: 600_000 });
}

/**
 * Uninstall a Helm release.
 */
export function uninstall(name: string, namespace: string): void {
  try {
    helm(`uninstall ${name} -n ${namespace} --wait`);
  } catch {
    // Release may not exist — ignore
  }
}

/**
 * Run helm test on a release.
 */
export function test(name: string, namespace: string): boolean {
  try {
    helm(`test ${name} -n ${namespace} --timeout 2m`);
    return true;
  } catch {
    return false;
  }
}
