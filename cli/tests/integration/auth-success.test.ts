// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 4 T4.2 — `auth` register integration test.
// Covers: loopzero whoami, loopzero logout.
// Closes: T0.3 audit gap "auth register has 2 unit tests but no
// integration coverage for whoami/logout".

import { describe, it, expect, afterEach } from '@jest/globals';
import {
  spawnLoopZero,
  mkTmpHome,
  cleanupTmpHome,
  writeFakeAuthConfig,
  parseLastJsonLine,
} from './_fixtures/spawn-helper';
import { startMockApi, MockApiServer } from './_fixtures/mock-api-server';

const tmpHomes: string[] = [];
const servers: MockApiServer[] = [];

afterEach(async () => {
  while (tmpHomes.length) cleanupTmpHome(tmpHomes.pop()!);
  while (servers.length) await servers.pop()!.close();
});

// Endpoint per cli/src/register/auth.ts:140 — GET /api/v1/auth/whoami
// returns {email, username?, team}.
describe('loopzero whoami', () => {
  it('exit 0 + JSON shape on success', async () => {
    const server = await startMockApi();
    servers.push(server);
    server.on({
      method: 'GET',
      path: '/api/v1/auth/whoami',
      response: {
        status: 200,
        body: { email: 'paulo@example.com', username: 'paulo', team: 'tn-test' },
      },
    });

    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    writeFakeAuthConfig(tmp, { loopzeroUrl: server.url });

    const r = await spawnLoopZero({
      args: ['whoami', '--json'],
      cwd: tmp,
      env: { LOOPZERO_URL: server.url },
      timeoutMs: 25_000,
    });

    // Diagnose if test fails: dump stdout/stderr.
    if (r.exitCode !== 0) {
      // eslint-disable-next-line no-console
      console.error('whoami spawn diag:', { exit: r.exitCode, stdout: r.stdout, stderr: r.stderr });
    }
    expect(r.exitCode).toBe(0);
    const body = parseLastJsonLine(r.stdout);
    expect(body).not.toBeNull();
    // shape per cli/src/commands/whoami.ts:88-93
    expect(body).toHaveProperty('email');
  });

  it('returns "no token" path when no config', async () => {
    // Per cli/src/commands/whoami.ts the exact code differs based on whether
    // verifyToken is invoked. With no token at all, the "Not logged in"
    // path runs entirely client-side. Exit code is non-zero per the
    // existing exit-code-contract.test.ts case 'whoami → 4'.
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    const r = await spawnLoopZero({
      args: ['whoami', '--json'],
      cwd: tmp,
      timeoutMs: 10_000,
    });
    // exit-code-contract pins this to 4 (auth error). If implementation
    // ever drifts to 1 (usage), THAT is a regression we want to catch.
    expect(r.exitCode).toBe(4);
  });
});

describe('loopzero logout', () => {
  it('exit 0 when logged in (clears config)', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    writeFakeAuthConfig(tmp);
    const r = await spawnLoopZero({
      args: ['logout', '--json'],
      cwd: tmp,
    });
    expect(r.exitCode).toBe(0);
  });

  it('exit 0 when already logged out (idempotent)', async () => {
    const tmp = mkTmpHome();
    tmpHomes.push(tmp);
    // no auth config — F-06 fix says this prints "Already logged out." with exit 0
    const r = await spawnLoopZero({
      args: ['logout'],
      cwd: tmp,
    });
    expect(r.exitCode).toBe(0);
  });
});
