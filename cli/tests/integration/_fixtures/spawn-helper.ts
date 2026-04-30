// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 4 T4.1 — spawn helper for
// integration tests. Wraps `child_process.spawnSync` against the real
// `cli/bin/loopzero` binary so tests exercise the full plumbing
// (Commander → execute → bin/loopzero exit-code wiring) instead of mocked
// objects in isolation.
//
// Pattern matches existing tests/integration/exit-code-contract.test.ts.
// The two are intentional — exit-code-contract is the floor (Rule 4
// enforcement only), per-register tests in this Phase build on top with
// success-path + error-path assertions, JSON shape validation, and
// optional in-process mock API server (mock-api-server.ts).

import { spawn, spawnSync, SpawnSyncReturns } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';

export const BIN_PATH = path.resolve(__dirname, '../../../bin/loopzero');

export interface SpawnLoopZeroOptions {
  /** Subcommand args, e.g. ['platform', 'status', '--json']. */
  args: string[];
  /** Override env. Always merged with process.env first. */
  env?: NodeJS.ProcessEnv;
  /** Working directory for the child. Defaults to a fresh tmpHome. */
  cwd?: string;
  /** Stdin contents to feed the child. */
  input?: string;
  /** Wall-clock cap (ms). Default 30s — generous for fork + spawn but
   * far below Jest's per-test 5min ceiling. */
  timeoutMs?: number;
}

export interface SpawnLoopZeroResult {
  /** Process exit code. -1 sentinel if killed (timeout/signal). */
  exitCode: number;
  /** Stdout as string (utf-8). */
  stdout: string;
  /** Stderr as string (utf-8). */
  stderr: string;
  /** Raw spawn result for advanced assertions. */
  raw: SpawnSyncReturns<string>;
}

/**
 * Run `node bin/loopzero <args>` with isolated env + cwd. Returns exit code,
 * stdout, stderr.
 *
 * **Async, NOT sync.** Critical for tests using `mock-api-server.ts`:
 * the mock HTTP server runs in the same Node process as Jest. If we
 * blocked the event loop with `spawnSync`, the mock server could not
 * serve requests, and the CLI child would hang waiting for a response
 * that never comes. The async variant keeps the parent's event loop
 * free while awaiting the child.
 *
 * Defaults that prevent flake:
 *   - HOME = fresh tmpHome (config + cache scoped to test)
 *   - NO_COLOR = "1" (defeat ansi)
 *   - LOOPZERO_URL = http://127.0.0.1:1 unless caller overrides
 *   - LOOPZERO_TELEMETRY_DISABLED = "1"
 *   - CI = "true"
 */
export function spawnLoopZero(opts: SpawnLoopZeroOptions): Promise<SpawnLoopZeroResult> {
  const tmpHome = opts.cwd ?? mkTmpHome();
  const baseEnv: NodeJS.ProcessEnv = {
    ...process.env,
    HOME: tmpHome,
    NO_COLOR: '1',
    // Default to unreachable. Tests using mock-api-server.ts override this.
    LOOPZERO_URL: 'http://127.0.0.1:1',
    LOOPZERO_TELEMETRY_DISABLED: '1',
    CI: 'true',
    ...opts.env,
  };
  const timeoutMs = opts.timeoutMs ?? 30_000;

  return new Promise<SpawnLoopZeroResult>((resolve) => {
    const child = spawn('node', [BIN_PATH, ...opts.args], {
      env: baseEnv,
      cwd: opts.cwd ?? tmpHome,
      stdio: ['pipe', 'pipe', 'pipe'],
    });

    let stdout = '';
    let stderr = '';
    let killed = false;

    child.stdout?.on('data', (chunk: Buffer) => {
      stdout += chunk.toString('utf-8');
    });
    child.stderr?.on('data', (chunk: Buffer) => {
      stderr += chunk.toString('utf-8');
    });

    if (opts.input) {
      child.stdin?.write(opts.input);
      child.stdin?.end();
    } else {
      child.stdin?.end();
    }

    const timer = setTimeout(() => {
      killed = true;
      child.kill('SIGKILL');
    }, timeoutMs);

    child.on('close', (code, signal) => {
      clearTimeout(timer);
      let exitCode: number;
      if (code !== null) {
        exitCode = code;
      } else if (signal) {
        // Surface signal-killed runs as -1 (consistent with spawnSync semantics).
        exitCode = -1;
      } else {
        exitCode = -1;
      }
      // Approximation of SpawnSyncReturns (callers rarely use raw).
      const raw = {
        pid: child.pid ?? 0,
        output: [null, stdout, stderr] as Array<string | null>,
        stdout,
        stderr,
        status: code,
        signal,
        error: killed ? new Error(`spawnLoopZero: timed out after ${timeoutMs}ms`) : undefined,
      } as unknown as SpawnSyncReturns<string>;
      resolve({ exitCode, stdout, stderr, raw });
    });
  });
}

/**
 * Synchronous variant — use ONLY when no in-process mock server is involved
 * (e.g., testing exit codes against unreachable URLs). Most tests should
 * prefer the async `spawnLoopZero()`.
 */
export function spawnLoopZeroSync(opts: SpawnLoopZeroOptions): SpawnLoopZeroResult {
  const tmpHome = opts.cwd ?? mkTmpHome();
  const baseEnv: NodeJS.ProcessEnv = {
    ...process.env,
    HOME: tmpHome,
    NO_COLOR: '1',
    LOOPZERO_URL: 'http://127.0.0.1:1',
    LOOPZERO_TELEMETRY_DISABLED: '1',
    CI: 'true',
    ...opts.env,
  };

  const raw = spawnSync('node', [BIN_PATH, ...opts.args], {
    env: baseEnv,
    cwd: opts.cwd ?? tmpHome,
    input: opts.input,
    encoding: 'utf-8',
    timeout: opts.timeoutMs ?? 30_000,
  });

  return {
    exitCode: raw.status ?? -1,
    stdout: raw.stdout ?? '',
    stderr: raw.stderr ?? '',
    raw,
  };
}

/**
 * Create a fresh tmp directory for HOME isolation. Caller responsible for
 * cleanup via cleanupTmpHome() (registered in afterEach).
 */
export function mkTmpHome(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-cli-itest-'));
}

/**
 * Best-effort cleanup. Tolerates already-deleted dirs (idempotent).
 */
export function cleanupTmpHome(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore — test cleanup is best-effort
  }
}

/**
 * Parse the LAST line of stdout as JSON. Useful for commands that emit
 * JSONL progress + a final summary object. Returns null if not parseable.
 */
export function parseLastJsonLine(stdout: string): Record<string, unknown> | null {
  const lines = stdout.trim().split('\n').filter((l) => l.trim());
  if (lines.length === 0) return null;
  for (let i = lines.length - 1; i >= 0; i--) {
    try {
      const parsed = JSON.parse(lines[i]);
      if (typeof parsed === 'object' && parsed !== null) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      // not JSON, try previous line
    }
  }
  return null;
}

/**
 * Parse ALL JSON lines from stdout (JSONL). Returns array; non-JSON lines
 * are dropped silently.
 */
export function parseAllJsonLines(stdout: string): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = [];
  for (const line of stdout.trim().split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const parsed = JSON.parse(trimmed);
      if (typeof parsed === 'object' && parsed !== null) {
        out.push(parsed as Record<string, unknown>);
      }
    } catch {
      // ignore
    }
  }
  return out;
}

/**
 * Write a fake auth config to tmpHome so commands gated by auth pass
 * through to the API call (which the mock server then handles).
 *
 * Mirrors the canonical `~/.loopzero/config.json` shape: `{token, loopzeroUrl,
 * tenantId}`. Token is intentionally synthetic — mock API decides what
 * to do with it.
 */
export function writeFakeAuthConfig(
  tmpHome: string,
  overrides: { token?: string; loopzeroUrl?: string; tenantId?: string } = {},
): void {
  const dir = path.join(tmpHome, '.loopzero');
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  const cfg = {
    token: overrides.token ?? 'fake-test-token',
    loopzeroUrl: overrides.loopzeroUrl ?? 'http://127.0.0.1:1',
    tenantId: overrides.tenantId ?? 'tn-test',
  };
  fs.writeFileSync(path.join(dir, 'config.json'), JSON.stringify(cfg, null, 2), { mode: 0o600 });
}

/**
 * Write a minimal `loopzero.yaml` to tmpHome cwd so commands needing project
 * context resolve it. Single-app, single-env starter shape.
 */
export function writeFakeLoopZeroYaml(
  tmpHome: string,
  overrides: { project?: string; env?: string } = {},
): void {
  const project = overrides.project ?? 'test-project';
  const env = overrides.env ?? 'production';
  const yaml = `version: 1
project: ${project}
apps:
  api:
    path: .
    framework: custom
    type: server
    build: dockerfile
environments:
  ${env}:
    plan: starter
`;
  fs.writeFileSync(path.join(tmpHome, 'loopzero.yaml'), yaml);
}
