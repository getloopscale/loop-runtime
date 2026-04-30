// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 4 T4.2 — `domain` register integration test.
// Closes T0.3 audit gap "domain register has 0 dedicated tests"
// (highest gap × highest blast radius — customer-facing path).

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

describe('loopzero domain --help', () => {
  it('exit 0 + lists subcommands', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['domain', '--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/add/);
    expect(r.stdout).toMatch(/list/);
    expect(r.stdout).toMatch(/remove/);
    expect(r.stdout).toMatch(/verify|status/);
  });
});

describe('loopzero domain list', () => {
  it('exit 4 without auth', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['domain', 'list', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(4);
  });

  it('does not hang against mock API', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'GET',
      path: '/api/v1/domains',
      response: { status: 200, body: { domains: [{ domain: 'app.example.com', state: 'active' }] } },
    });

    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    writeFakeAuthConfig(tmp, { loopzeroUrl: server.url });

    const r = await spawnLoopZero({
      args: ['domain', 'list', '--json'],
      cwd: tmp,
      env: { LOOPZERO_URL: server.url },
      timeoutMs: 15_000,
    });
    expect(r.exitCode).not.toBe(143);
    expect(r.exitCode).not.toBe(-1);
  });
});

describe('loopzero domain add', () => {
  it('exit non-zero on bad domain format (usage)', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['domain', 'add', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    // missing required <domain> arg → Commander exits 1
    expect(r.exitCode).toBe(1);
  });
});
