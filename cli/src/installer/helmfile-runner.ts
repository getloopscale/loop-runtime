// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// CHG-2026-04-28-LAYERED-BOOTSTRAP — T3.1
// Wrapper around the `helmfile` CLI. Used by `loopzero platform install/upgrade/uninstall`
// as the SOLE mechanism to install Helm charts (replaces the in-CLI helm runner).

import { execSync, spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { resolve, join } from 'node:path';

export interface HelmfileEnv {
  /** Path to the helmfile.yaml */
  filePath: string;
  /** Helmfile environment name (dev | prod | standalone) */
  environment: string;
  /** Optional KUBECONFIG override */
  kubeconfig?: string;
  /** Extra env vars (e.g., LOOPZERO_DB_HOST, LOOPZERO_CF_TUNNEL_TOKEN) */
  extraEnv?: Record<string, string>;
  /** Stream output to stdout/stderr (default true) */
  streamOutput?: boolean;
  /** Dry run — `helmfile diff` instead of sync */
  dryRun?: boolean;
}

export interface HelmfileResult {
  status: 'success' | 'failed';
  durationMs: number;
  exitCode: number;
  stderr?: string;
}

function runHelmfile(action: 'sync' | 'diff' | 'destroy' | 'list' | 'lint' | 'apply', env: HelmfileEnv): HelmfileResult {
  if (!existsSync(env.filePath)) {
    return { status: 'failed', durationMs: 0, exitCode: 1, stderr: `helmfile not found: ${env.filePath}` };
  }

  const args = ['-f', env.filePath, '-e', env.environment, action];
  const start = Date.now();
  const child = spawnSync('helmfile', args, {
    cwd: resolve(env.filePath, '..'),
    env: {
      ...process.env,
      ...(env.kubeconfig ? { KUBECONFIG: env.kubeconfig } : {}),
      ...(env.extraEnv ?? {}),
    },
    stdio: env.streamOutput === false ? 'pipe' : 'inherit',
    shell: false,
  });
  const durationMs = Date.now() - start;

  if (child.status !== 0) {
    return {
      status: 'failed',
      durationMs,
      exitCode: child.status ?? 1,
      stderr: child.stderr?.toString() ?? '',
    };
  }
  return { status: 'success', durationMs, exitCode: 0 };
}

export function helmfileSync(env: HelmfileEnv): HelmfileResult {
  return runHelmfile(env.dryRun ? 'diff' : 'sync', env);
}

export function helmfileDiff(env: HelmfileEnv): HelmfileResult {
  return runHelmfile('diff', env);
}

export function helmfileDestroy(env: HelmfileEnv): HelmfileResult {
  return runHelmfile('destroy', env);
}

export function helmfileList(env: HelmfileEnv): HelmfileResult {
  return runHelmfile('list', env);
}

export function helmfileLint(env: HelmfileEnv): HelmfileResult {
  return runHelmfile('lint', env);
}

/**
 * Verifies that the `helmfile` binary is available in PATH and v1+.
 */
export function ensureHelmfileAvailable(): { ok: boolean; version?: string; error?: string } {
  try {
    const out = execSync('helmfile --version', { encoding: 'utf-8' });
    const match = out.match(/version\s+(v?\d+\.\d+\.\d+)/i);
    return { ok: true, version: match?.[1] ?? out.trim() };
  } catch {
    return {
      ok: false,
      error: 'helmfile not found in PATH. Install: https://helmfile.readthedocs.io/en/latest/#installation',
    };
  }
}

/**
 * Resolves the helmfile path for a cluster role given the project root.
 */
export function resolveHelmfilePath(projectRoot: string, clusterRole: 'mgmt' | 'build' | 'runtime'): string {
  // Helmfile v1 expects .gotmpl extension when the file uses Go template syntax
  // (e.g., conditional `installed: {{ .Values.X }}` for env-specific releases).
  return join(projectRoot, 'infra', 'helmfile', `${clusterRole}.helmfile.yaml.gotmpl`);
}
