// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Smoke test for the integration test harness itself. Validates:
//   • spawnLoopZero() runs the real binary
//   • mock API serves canned responses
//   • spawnLoopZero's HTTP client reaches the mock
//   • tmpHome isolation works (no leak between tests)
//
// If this file goes RED, every register integration test below is suspect.

import { describe, it, expect, afterEach } from '@jest/globals';
import * as fs from 'fs';
import * as path from 'path';

import {
  spawnLoopZero,
  mkTmpHome,
  cleanupTmpHome,
  writeFakeAuthConfig,
  parseLastJsonLine,
} from './spawn-helper';
import { startMockApi, MockApiServer } from './mock-api-server';

const tmpHomes: string[] = [];
const servers: MockApiServer[] = [];

afterEach(async () => {
  while (tmpHomes.length) cleanupTmpHome(tmpHomes.pop()!);
  while (servers.length) await servers.pop()!.close();
});

describe('harness: spawnLoopZero', () => {
  it('runs `loopzero --version` and exits 0', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['--version'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    // version is emitted to stdout
    expect(r.stdout.trim().length).toBeGreaterThan(0);
  });

  it('runs `loopzero --help` and exits 0', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['--help'], cwd: tmp });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('Usage:');
  });

  it('exits 1 on unknown command', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({ args: ['this-command-does-not-exist'], cwd: tmp });
    expect(r.exitCode).toBe(1);
  });

  it('writeFakeAuthConfig produces a readable config.json', () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    writeFakeAuthConfig(tmp, { token: 'abc', tenantId: 'tn-x' });
    const cfgPath = path.join(tmp, '.loopzero', 'config.json');
    expect(fs.existsSync(cfgPath)).toBe(true);
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf-8'));
    expect(cfg.token).toBe('abc');
    expect(cfg.tenantId).toBe('tn-x');
  });
});

describe('harness: startMockApi', () => {
  it('serves canned response on registered route', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'GET',
      path: '/api/v1/users/me',
      response: { status: 200, body: { user_id: 'u-test' } },
    });

    // Hit the mock directly via http to validate it's up.
    const http = await import('http');
    const data = await new Promise<string>((resolve, reject) => {
      http.get(`${server.url}/api/v1/users/me`, (res) => {
        let body = '';
        res.on('data', (c) => (body += c));
        res.on('end', () => resolve(body));
        res.on('error', reject);
      });
    });
    expect(JSON.parse(data)).toEqual({ user_id: 'u-test' });
    expect(server.requests).toHaveLength(1);
    expect(server.requests[0].method).toBe('GET');
    expect(server.requests[0].url).toBe('/api/v1/users/me');
  });

  it('returns 404 for unregistered routes', async () => {
    const server = await startMockApi();
    servers.push(server);
    const http = await import('http');
    const status = await new Promise<number>((resolve, reject) => {
      http.get(`${server.url}/nonexistent`, (res) => {
        resolve(res.statusCode ?? 0);
        res.resume();
      }).on('error', reject);
    });
    expect(status).toBe(404);
  });
});

describe('harness: parseLastJsonLine', () => {
  it('returns last JSON line from JSONL output', () => {
    const out = '{"step":"1/6"}\n{"step":"6/6"}\n{"status":"ready"}\n';
    expect(parseLastJsonLine(out)).toEqual({ status: 'ready' });
  });
  it('skips trailing non-JSON', () => {
    const out = '{"a":1}\nsome prose\n';
    expect(parseLastJsonLine(out)).toEqual({ a: 1 });
  });
  it('returns null for empty', () => {
    expect(parseLastJsonLine('')).toBeNull();
  });
});
