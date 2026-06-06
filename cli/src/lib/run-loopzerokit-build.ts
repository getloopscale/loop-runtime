// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T2.2 (loopzero-cli-vercel-like-ux) — spawn `loopzerokit build` as
// a subprocess with ADR D7 hygiene defaults:
//   1. Timeout: AbortSignal.timeout(LOOPZEROKIT_BUILD_TIMEOUT_MS ?? 600000)
//   2. CI=true + LOOPZEROKIT_NON_INTERACTIVE=1 env
//   3. SIGTERM → 1s grace → SIGKILL escalation
// Plus EC-5: spawn ENOENT → rethrow as LoopZerokitBinaryNotFoundError.

import { spawn } from 'child_process';
import { ApiError } from '../api/client';
import { LoopZerokitBinaryNotFoundError } from './resolve-loopzerokit-bin';

const DEFAULT_TIMEOUT_MS = 10 * 60 * 1000;
const SIGTERM_GRACE_MS = 1000;
const STDERR_TAIL_LENGTH = 500;

export interface RunLoopZerokitBuildOpts {
  loopzerokitBin: string;
  cwd: string;
  target?: string;
  /** Caller-provided cancellation signal. Combined with the default timeout. */
  signal?: AbortSignal;
  /** Override the default 10-min timeout (also overridable via env var). */
  timeoutMs?: number;
}

/**
 * Subprocess failure carrying the loopzerokit exit code and a tail of stderr.
 * Extends ApiError so the existing handleError path in deploy-helpers routes
 * it through exitWithError uniformly.
 */
export class LoopZerokitBuildError extends ApiError {
  constructor(message: string, code: string, cause?: unknown) {
    super(message, 1, 0, code, cause);
    this.name = 'LoopZerokitBuildError';
  }
}

function resolveTimeout(opts: RunLoopZerokitBuildOpts): number {
  if (typeof opts.timeoutMs === 'number') return opts.timeoutMs;
  const envValue = process.env.LOOPZEROKIT_BUILD_TIMEOUT_MS;
  if (envValue !== undefined) {
    const parsed = parseInt(envValue, 10);
    if (!Number.isNaN(parsed) && parsed > 0) return parsed;
  }
  return DEFAULT_TIMEOUT_MS;
}

function combineSignals(opts: RunLoopZerokitBuildOpts): { signal: AbortSignal; timeoutHandle: NodeJS.Timeout; reasonRef: { value: 'timeout' | 'cancelled' | null } } {
  const ac = new AbortController();
  const reasonRef: { value: 'timeout' | 'cancelled' | null } = { value: null };
  const timeoutMs = resolveTimeout(opts);
  const timeoutHandle = setTimeout(() => {
    reasonRef.value = 'timeout';
    ac.abort();
  }, timeoutMs);
  if (opts.signal) {
    if (opts.signal.aborted) {
      reasonRef.value = 'cancelled';
      ac.abort();
    } else {
      opts.signal.addEventListener('abort', () => {
        reasonRef.value = 'cancelled';
        ac.abort();
      }, { once: true });
    }
  }
  return { signal: ac.signal, timeoutHandle, reasonRef };
}

/**
 * Spawn `loopzerokit build --target <target>` as a child process. Inherits stdio
 * for live progress; tails the last 500 chars of stderr for error messages.
 *
 * Throws LoopZerokitBuildError on non-zero exit, timeout, or cancellation.
 * Throws LoopZerokitBinaryNotFoundError on spawn ENOENT (EC-5 — binary
 * disappeared between resolution and spawn).
 */
export function runLoopZerokitBuild(opts: RunLoopZerokitBuildOpts): Promise<void> {
  const target = opts.target ?? 'loopzero-cloud';
  const { signal, timeoutHandle, reasonRef } = combineSignals(opts);

  return new Promise<void>((resolve, reject) => {
    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(opts.loopzerokitBin, ['build', '--target', target], {
        cwd: opts.cwd,
        // EC-3 absorbed (D7) — force non-interactive defaults so transitive
        // npm tools (husky, vite plugins, license dialogs) take the CI path.
        env: { ...process.env, CI: 'true', LOOPZEROKIT_NON_INTERACTIVE: '1' },
        stdio: ['inherit', 'inherit', 'pipe'],
        // EC-7 absorbed — detached makes the child a process-group leader so
        // we can SIGTERM/SIGKILL the entire tree (including any sub-builds
        // it spawns) via process.kill(-pid, signal).
        detached: process.platform !== 'win32',
      });
    } catch (spawnErr) {
      clearTimeout(timeoutHandle);
      reject(new LoopZerokitBuildError(
        `Failed to spawn loopzerokit at ${opts.loopzerokitBin}: ${(spawnErr as Error).message}`,
        'LOOPZEROKIT_SPAWN_FAILED',
        spawnErr,
      ));
      return;
    }

    let stderrTail = '';
    if (child.stderr) {
      child.stderr.on('data', (chunk: Buffer) => {
        const text = chunk.toString('utf-8');
        process.stderr.write(text);
        stderrTail = (stderrTail + text).slice(-STDERR_TAIL_LENGTH);
      });
    }

    let sigtermSent = false;
    let sigkillTimer: NodeJS.Timeout | null = null;
    const killTree = (signal: NodeJS.Signals): void => {
      const pid = child.pid;
      if (pid === undefined) return;
      try {
        if (process.platform === 'win32') {
          child.kill(signal);
        } else {
          // Negative PID targets the entire process group (detached:true above).
          process.kill(-pid, signal);
        }
      } catch {
        // already dead / no permission
      }
    };
    const onAbort = (): void => {
      if (sigtermSent || child.exitCode !== null) return;
      sigtermSent = true;
      killTree('SIGTERM');
      // EC-7 — SIGTERM → 1s grace → SIGKILL escalation.
      sigkillTimer = setTimeout(() => {
        if (child.exitCode === null) {
          killTree('SIGKILL');
        }
      }, SIGTERM_GRACE_MS);
    };
    signal.addEventListener('abort', onAbort);

    child.on('error', (err: NodeJS.ErrnoException) => {
      clearTimeout(timeoutHandle);
      if (sigkillTimer) clearTimeout(sigkillTimer);
      signal.removeEventListener('abort', onAbort);
      // EC-5 absorbed — ENOENT after a successful resolveLoopZerokitBin means
      // the binary disappeared between resolution and spawn (likely a
      // concurrent `pnpm install`).
      if (err.code === 'ENOENT') {
        reject(new LoopZerokitBinaryNotFoundError(
          `loopzerokit binary at ${opts.loopzerokitBin} disappeared between resolution and spawn — likely a concurrent npm/pnpm install; re-run \`loopzero deploy\`.`,
        ));
        return;
      }
      reject(new LoopZerokitBuildError(
        `loopzerokit subprocess error: ${err.message}`,
        'LOOPZEROKIT_SPAWN_FAILED',
        err,
      ));
    });

    child.on('close', (code: number | null) => {
      clearTimeout(timeoutHandle);
      if (sigkillTimer) clearTimeout(sigkillTimer);
      signal.removeEventListener('abort', onAbort);
      if (reasonRef.value === 'timeout') {
        const timeoutMs = resolveTimeout(opts);
        reject(new LoopZerokitBuildError(
          `loopzerokit build exceeded ${timeoutMs} ms; re-run with LOOPZEROKIT_BUILD_TIMEOUT_MS=<ms> to extend.`,
          'LOOPZEROKIT_BUILD_TIMEOUT',
        ));
        return;
      }
      if (reasonRef.value === 'cancelled') {
        reject(new LoopZerokitBuildError(
          'loopzerokit build cancelled.',
          'LOOPZEROKIT_BUILD_CANCELLED',
        ));
        return;
      }
      if (code === 0) {
        resolve();
        return;
      }
      reject(new LoopZerokitBuildError(
        `loopzerokit build failed with exit code ${code}.${stderrTail ? ` Last stderr: ${stderrTail}` : ''}`,
        'LOOPZEROKIT_BUILD_FAILED',
      ));
    });
  });
}
