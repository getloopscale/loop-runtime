// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// CHG-2026-04-28-LAYERED-BOOTSTRAP — T3.1
// Wrapper around `pulumi` CLI for L1 (cloud infra). Used ONLY when
// `loopzero platform install --provider <name>` is invoked. Self-hosting
// (no `--provider`) skips this entirely.

import { execSync, spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { resolve, join } from 'node:path';

export interface PulumiContext {
  /** Pulumi project directory (where Pulumi.yaml lives) */
  projectDir: string;
  /** Stack name (e.g., dev, prod) */
  stack: string;
  /** Optional env overrides (DO_TOKEN, CF_API_TOKEN, etc.) */
  extraEnv?: Record<string, string>;
  /** Stream output to stdout/stderr */
  streamOutput?: boolean;
}

export interface PulumiResult {
  status: 'success' | 'failed';
  durationMs: number;
  exitCode: number;
  stdout?: string;
  stderr?: string;
}

function runPulumi(args: string[], ctx: PulumiContext): PulumiResult {
  if (!existsSync(ctx.projectDir)) {
    return { status: 'failed', durationMs: 0, exitCode: 1, stderr: `pulumi project dir not found: ${ctx.projectDir}` };
  }

  const start = Date.now();
  const child = spawnSync('npx', ['pulumi', ...args], {
    cwd: ctx.projectDir,
    env: {
      ...process.env,
      ...(ctx.extraEnv ?? {}),
    },
    stdio: ctx.streamOutput === false ? 'pipe' : 'inherit',
    shell: false,
  });
  const durationMs = Date.now() - start;

  if (child.status !== 0) {
    return {
      status: 'failed',
      durationMs,
      exitCode: child.status ?? 1,
      stdout: child.stdout?.toString() ?? '',
      stderr: child.stderr?.toString() ?? '',
    };
  }
  return {
    status: 'success',
    durationMs,
    exitCode: 0,
    stdout: child.stdout?.toString() ?? '',
  };
}

export function pulumiUp(ctx: PulumiContext): PulumiResult {
  return runPulumi(['up', '--stack', ctx.stack, '--yes', '--non-interactive'], ctx);
}

export function pulumiPreview(ctx: PulumiContext): PulumiResult {
  return runPulumi(['preview', '--stack', ctx.stack, '--non-interactive'], ctx);
}

export function pulumiDestroy(ctx: PulumiContext): PulumiResult {
  return runPulumi(['destroy', '--stack', ctx.stack, '--yes', '--non-interactive'], ctx);
}

/**
 * Reads a single Pulumi output (resolves Promise<string>).
 * Equivalent to `pulumi stack output <key> --stack <stack> --show-secrets`.
 */
export function pulumiOutput(ctx: PulumiContext, key: string, includeSecrets = false): string | null {
  const args = ['stack', 'output', key, '--stack', ctx.stack];
  if (includeSecrets) args.push('--show-secrets');
  const child = spawnSync('npx', ['pulumi', ...args], {
    cwd: ctx.projectDir,
    env: { ...process.env, ...(ctx.extraEnv ?? {}) },
    stdio: 'pipe',
    shell: false,
  });
  if (child.status !== 0) return null;
  return child.stdout?.toString().trim() ?? null;
}

/**
 * Reads the full set of stack outputs as a JSON object.
 * Equivalent to `pulumi stack output --json --show-secrets`.
 */
export function pulumiOutputs(ctx: PulumiContext, includeSecrets = false): Record<string, unknown> | null {
  const args = ['stack', 'output', '--json', '--stack', ctx.stack];
  if (includeSecrets) args.push('--show-secrets');
  const child = spawnSync('npx', ['pulumi', ...args], {
    cwd: ctx.projectDir,
    env: { ...process.env, ...(ctx.extraEnv ?? {}) },
    stdio: 'pipe',
    shell: false,
  });
  if (child.status !== 0) return null;
  try {
    return JSON.parse(child.stdout?.toString() ?? '{}');
  } catch {
    return null;
  }
}

export function ensurePulumiAvailable(): { ok: boolean; version?: string; error?: string } {
  try {
    const out = execSync('pulumi version', { encoding: 'utf-8' });
    return { ok: true, version: out.trim() };
  } catch {
    return {
      ok: false,
      error: 'pulumi not found in PATH. Install: https://www.pulumi.com/docs/install/',
    };
  }
}

export function resolvePulumiProjectDir(projectRoot: string): string {
  return join(projectRoot, 'infra', 'pulumi');
}

export function resolveProjectRoot(): string {
  // Walk up from cwd to find package.json with name="loopzero" or infra/pulumi/Pulumi.yaml.
  let cwd = process.cwd();
  for (let i = 0; i < 10; i++) {
    if (existsSync(resolve(cwd, 'infra/pulumi/Pulumi.yaml'))) return cwd;
    const parent = resolve(cwd, '..');
    if (parent === cwd) break;
    cwd = parent;
  }
  return process.cwd();
}
