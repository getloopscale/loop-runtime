// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 4 T4.2 — `platform` register integration test.
// Existing unit coverage is best-in-repo (4 dedicated test files);
// this fork-based test verifies bin/loopzero plumbing for `platform status`
// against a mock cluster path.

import { describe, it, expect, afterEach } from '@jest/globals';
import {
  spawnLoopZero,
  mkTmpHome,
  cleanupTmpHome,
} from './_fixtures/spawn-helper';

const tmpHomes: string[] = [];

afterEach(() => {
  while (tmpHomes.length) cleanupTmpHome(tmpHomes.pop()!);
});

describe('loopzero platform --help', () => {
  it('exit 0 + lists subcommands', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['platform', '--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    // sanity: subcommands present
    expect(r.stdout).toMatch(/install/);
    expect(r.stdout).toMatch(/status/);
  });
});

describe('loopzero platform status', () => {
  it('exit 1 when no kubeconfig context (env without K8s)', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    // No KUBECONFIG → command can't query cluster; must fail clean.
    const r = await spawnLoopZero({
      args: ['platform', 'status', '--json'],
      cwd: tmp,
      env: { KUBECONFIG: '/nonexistent/path' },
      timeoutMs: 15_000,
    });
    // Plat status without cluster → typically exit 1 or 5; both signal
    // "cannot determine state" cleanly. Reject 0 (would be lying about
    // ready state).
    expect(r.exitCode).not.toBe(0);
    expect([1, 5]).toContain(r.exitCode);
  });
});

describe('loopzero platform install --help', () => {
  it('lists --validate-l3 flag (T8.1)', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['platform', 'install', '--help'],
      cwd: tmp,
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/--validate-l3/);
    expect(r.stdout).toMatch(/--restore-secrets/);
  });
});
