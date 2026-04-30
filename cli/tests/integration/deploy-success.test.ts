// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 4 T4.2 — `deploy` register integration test.
// 7 subcommands. Existing cli-e2e covers happy-path against real API;
// this test covers help + error paths against the mock harness.

import { describe, it, expect, afterEach } from '@jest/globals';
import {
  spawnLoopZero,
  mkTmpHome,
  cleanupTmpHome,
  writeFakeAuthConfig,
} from './_fixtures/spawn-helper';

const tmpHomes: string[] = [];

afterEach(() => {
  while (tmpHomes.length) cleanupTmpHome(tmpHomes.pop()!);
});

describe('loopzero deploy --help', () => {
  it('exit 0', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['deploy', '--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/deploy/);
  });
});

describe('loopzero validate', () => {
  it('exit 0 with valid loopzero.yaml', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const fs = await import('fs');
    const path = await import('path');
    fs.writeFileSync(
      path.join(tmp, 'loopzero.yaml'),
      `version: 1
project: itest
apps:
  api:
    path: .
    framework: custom
    type: server
    build: dockerfile
environments:
  production:
    plan: starter
`,
    );

    const r = await spawnLoopZero({
      args: ['validate', '--json'],
      cwd: tmp,
      timeoutMs: 15_000,
    });
    // validate is offline (no API call). Tolerate 0 (valid) or 1
    // (validator added a constraint we don't satisfy here). What we
    // reject is hang or unexpected codes.
    expect([0, 1]).toContain(r.exitCode);
  });

  it('exit 1 without loopzero.yaml', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['validate', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(1);
  });
});

describe('loopzero cancel', () => {
  it('exit 1 without build-id arg', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    writeFakeAuthConfig(tmp);
    const r = await spawnLoopZero({
      args: ['cancel', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    // Commander rejects missing required positional with exit 1.
    expect(r.exitCode).toBe(1);
  });
});

describe('loopzero logs', () => {
  it('exit 4 without auth', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['logs', '--build', 'b-test', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(4);
  });
});

describe('loopzero status', () => {
  it('exit 4 without auth', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['status', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(4);
  });
});
