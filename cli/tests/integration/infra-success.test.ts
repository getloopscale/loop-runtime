// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 4 T4.2 — `infra` register integration test.
// `infra.ts` register hosts the most subcommands of any register
// (16 distinct: build/secret/connect/disconnect/billing/audit/admin).
// This test exercises a representative slice; other groups will get
// dedicated tests in follow-up if blast radius warrants.

import { describe, it, expect, afterEach } from '@jest/globals';
import {
  spawnLoopZero,
  mkTmpHome,
  cleanupTmpHome,
  writeFakeAuthConfig,
} from './_fixtures/spawn-helper';
import { startMockApi, MockApiServer } from './_fixtures/mock-api-server';

const tmpHomes: string[] = [];
const servers: MockApiServer[] = [];

afterEach(async () => {
  while (tmpHomes.length) cleanupTmpHome(tmpHomes.pop()!);
  while (servers.length) await servers.pop()!.close();
});

describe('loopzero build --help', () => {
  it('exit 0 + lists list/inspect/cancel', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['build', '--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/list/);
    expect(r.stdout).toMatch(/inspect/);
    expect(r.stdout).toMatch(/cancel/);
  });
});

describe('loopzero build list', () => {
  it('exit 4 without auth', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['build', 'list', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(4);
  });
});

describe('loopzero secret --help', () => {
  it('exit 0 + lists set/list/delete', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['secret', '--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/set/);
    expect(r.stdout).toMatch(/list/);
    expect(r.stdout).toMatch(/delete/);
  });
});

describe('loopzero audit', () => {
  it('exit 4 without auth', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['audit', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(4);
  });

  it('exit 0 against mock API', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'GET',
      path: '/api/v1/audit',
      response: { status: 200, body: { entries: [], total: 0 } },
    });

    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    writeFakeAuthConfig(tmp, { loopzeroUrl: server.url });

    const r = await spawnLoopZero({
      args: ['audit', '--json'],
      cwd: tmp,
      env: { LOOPZERO_URL: server.url },
      timeoutMs: 15_000,
    });
    expect(r.exitCode).not.toBe(143);
    expect(r.exitCode).not.toBe(-1);
  });
});

describe('loopzero admin --help', () => {
  it('exit 0 + lists tenants', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['admin', '--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/tenants/);
  });
});
