// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 5 T5.5 — `loopzero admin tenants pin` integration test.

import { describe, it, expect, afterEach } from '@jest/globals';
import {
  spawnLoopZero,
  mkTmpHome,
  cleanupTmpHome,
} from './_fixtures/spawn-helper';
import { startMockApi, MockApiServer } from './_fixtures/mock-api-server';

const tmpHomes: string[] = [];
const servers: MockApiServer[] = [];

afterEach(async () => {
  while (tmpHomes.length) cleanupTmpHome(tmpHomes.pop()!);
  while (servers.length) await servers.pop()!.close();
});

describe('loopzero admin tenants pin --help', () => {
  it('exit 0 + lists --tenant + --cell flags', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['admin', 'tenants', 'pin', '--help'],
      cwd: tmp,
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toMatch(/--tenant/);
    expect(r.stdout).toMatch(/--cell/);
  });
});

describe('loopzero admin tenants pin', () => {
  it('exit 4 without LOOPZERO_SERVICE_AUTH', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['admin', 'tenants', 'pin', '--tenant', 'tn-x', '--cell', 'cell-2', '--json'],
      cwd: tmp,
      env: { LOOPZERO_SERVICE_AUTH: '' },
      timeoutMs: 10_000,
    });
    expect(r.exitCode).toBe(4);
  });

  it('exit 1 on missing required --tenant', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['admin', 'tenants', 'pin', '--cell', 'cell-2'],
      cwd: tmp,
      env: { LOOPZERO_SERVICE_AUTH: 'secret' },
      timeoutMs: 10_000,
    });
    // Commander rejects missing required option with exit 1.
    expect(r.exitCode).toBe(1);
  });

  it('exit 0 on success against mock API', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'POST',
      path: '/api/v1/admin/tenants/pin',
      response: {
        status: 200,
        body: {
          tenant_id: 'tn-x',
          previous_cell: 'cell-default',
          new_cell: 'cell-2',
          updated: true,
        },
      },
    });

    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['admin', 'tenants', 'pin', '--tenant', 'tn-x', '--cell', 'cell-2', '--json'],
      cwd: tmp,
      env: {
        LOOPZERO_URL: server.url,
        LOOPZERO_SERVICE_AUTH: 'secret',
      },
      timeoutMs: 15_000,
    });
    expect(r.exitCode).toBe(0);
    expect(server.requests).toHaveLength(1);
    expect(server.requests[0].method).toBe('POST');
    expect(server.requests[0].url).toBe('/api/v1/admin/tenants/pin');
    expect(server.requests[0].headers['x-service-auth']).toBe('secret');
    const body = JSON.parse(server.requests[0].body);
    expect(body).toEqual({ tenant_id: 'tn-x', cell_id: 'cell-2' });
  });

  it('exit 5 on 409 (cell at capacity)', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'POST',
      path: '/api/v1/admin/tenants/pin',
      response: {
        status: 409,
        body: { code: 'cell_at_capacity', message: 'cell at capacity: 500/500' },
      },
    });

    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['admin', 'tenants', 'pin', '--tenant', 'tn-x', '--cell', 'cell-2', '--json'],
      cwd: tmp,
      env: {
        LOOPZERO_URL: server.url,
        LOOPZERO_SERVICE_AUTH: 'secret',
      },
      timeoutMs: 15_000,
    });
    expect(r.exitCode).toBe(5);
  });

  it('exit 4 on 403 (admin endpoint disabled)', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'POST',
      path: '/api/v1/admin/tenants/pin',
      response: {
        status: 403,
        body: { code: 'forbidden', message: 'admin endpoint requires X-Service-Auth' },
      },
    });

    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['admin', 'tenants', 'pin', '--tenant', 'tn-x', '--cell', 'cell-2', '--json'],
      cwd: tmp,
      env: {
        LOOPZERO_URL: server.url,
        LOOPZERO_SERVICE_AUTH: 'secret',
      },
      timeoutMs: 15_000,
    });
    expect(r.exitCode).toBe(4);
  });
});
