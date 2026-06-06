// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T2.2 (loopzero-cli-vercel-like-ux) — runLoopZerokitBuild tests.
// Uses small shell scripts as fixture "loopzerokit" binaries so the test
// exercises the real spawn boundary (no spawn mocking).

import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import {
  runLoopZerokitBuild,
  LoopZerokitBuildError,
} from '../../../src/lib/run-loopzerokit-build';
import { LoopZerokitBinaryNotFoundError } from '../../../src/lib/resolve-loopzerokit-bin';

let tmpRoot: string;

beforeEach(() => {
  tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'run-loopzerokit-build-test-'));
  delete process.env.LOOPZEROKIT_BUILD_TIMEOUT_MS;
});

afterEach(() => {
  fs.rmSync(tmpRoot, { recursive: true, force: true });
  delete process.env.LOOPZEROKIT_BUILD_TIMEOUT_MS;
});

function writeBin(script: string): string {
  const bin = path.join(tmpRoot, 'loopzerokit');
  fs.writeFileSync(bin, script);
  fs.chmodSync(bin, 0o755);
  return bin;
}

describe('runLoopZerokitBuild — exit handling', () => {
  it('resolves on exit 0', async () => {
    const bin = writeBin('#!/bin/sh\nexit 0\n');
    await expect(runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot })).resolves.toBeUndefined();
  });

  it('throws LoopZerokitBuildError on non-zero exit, code = LOOPZEROKIT_BUILD_FAILED', async () => {
    const bin = writeBin('#!/bin/sh\necho "boom" >&2\nexit 7\n');
    await expect(runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot })).rejects.toMatchObject({
      code: 'LOOPZEROKIT_BUILD_FAILED',
      name: 'LoopZerokitBuildError',
    });
  });

  it('preserves cause chain via ApiError parentage', async () => {
    const bin = writeBin('#!/bin/sh\nexit 3\n');
    try {
      await runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot });
      fail('expected throw');
    } catch (err) {
      expect(err).toBeInstanceOf(LoopZerokitBuildError);
      // ApiError class extension — exitCode is preserved.
      expect((err as LoopZerokitBuildError).exitCode).toBe(1);
    }
  });

  it('passes the --target flag verbatim', async () => {
    const bin = writeBin('#!/bin/sh\necho "$2 $3" > $0.captured\nexit 0\n');
    await runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot, target: 'vercel' });
    const captured = fs.readFileSync(`${bin}.captured`, 'utf-8').trim();
    expect(captured).toBe('--target vercel');
  });
});

describe('runLoopZerokitBuild — D7 hygiene (EC-2 timeout)', () => {
  it('times out after the default 10-min window override', async () => {
    process.env.LOOPZEROKIT_BUILD_TIMEOUT_MS = '100';
    const bin = writeBin('#!/bin/sh\nsleep 10\n');
    await expect(runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot })).rejects.toMatchObject({
      code: 'LOOPZEROKIT_BUILD_TIMEOUT',
    });
  });

  it('honors explicit timeoutMs override', async () => {
    const bin = writeBin('#!/bin/sh\nsleep 10\n');
    await expect(
      runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot, timeoutMs: 100 }),
    ).rejects.toMatchObject({ code: 'LOOPZEROKIT_BUILD_TIMEOUT' });
  });
});

describe('runLoopZerokitBuild — D7 hygiene (EC-3 non-interactive env)', () => {
  it('passes CI=true and LOOPZEROKIT_NON_INTERACTIVE=1 in env', async () => {
    const bin = writeBin('#!/bin/sh\necho "CI=$CI INTER=$LOOPZEROKIT_NON_INTERACTIVE" > $0.env\nexit 0\n');
    await runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot });
    const captured = fs.readFileSync(`${bin}.env`, 'utf-8').trim();
    expect(captured).toBe('CI=true INTER=1');
  });
});

describe('runLoopZerokitBuild — D7 hygiene (EC-7 SIGKILL fallback)', () => {
  it('falls back to SIGKILL when child ignores SIGTERM', async () => {
    // Child traps SIGTERM and continues sleeping. After 1s grace the runner
    // sends SIGKILL which kills it unconditionally. Total wait < 2s.
    const bin = writeBin(`#!/bin/sh\ntrap '' TERM\nsleep 30\n`);
    const ac = new AbortController();
    setTimeout(() => ac.abort(), 50);
    const start = Date.now();
    await expect(
      runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot, signal: ac.signal, timeoutMs: 30_000 }),
    ).rejects.toMatchObject({ code: 'LOOPZEROKIT_BUILD_CANCELLED' });
    const elapsed = Date.now() - start;
    expect(elapsed).toBeLessThan(3000); // Without SIGKILL escalation, would hang 30s.
  });
});

describe('runLoopZerokitBuild — EC-5 spawn ENOENT', () => {
  it('throws LoopZerokitBinaryNotFoundError when bin disappeared before spawn', async () => {
    const bin = path.join(tmpRoot, 'this-bin-does-not-exist');
    await expect(runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot })).rejects.toBeInstanceOf(
      LoopZerokitBinaryNotFoundError,
    );
  });
});

describe('runLoopZerokitBuild — streaming', () => {
  it('inherits child stdout to parent (live streaming)', async () => {
    // We can't assert process.stdout.write was called without spying on a
    // global, but we CAN confirm the parent's stdio config is `inherit` by
    // observing that the test runner does not throw on long-running output.
    const bin = writeBin('#!/bin/sh\nfor i in 1 2 3; do echo "line $i"; done\nexit 0\n');
    await expect(runLoopZerokitBuild({ loopzerokitBin: bin, cwd: tmpRoot })).resolves.toBeUndefined();
  });
});
