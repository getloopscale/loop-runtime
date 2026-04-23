// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { AuditCommand, AuditDeps } from '../../../src/commands/audit';
import { ApiError } from '../../../src/api/client';

function makeDeps(overrides: Partial<AuditDeps> = {}): AuditDeps {
  return {
    getToken: () => 'valid-token',
    getAudit: async () => ({ entries: [], total: 0, limit: 20, offset: 0 }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('AuditCommand', () => {
  it('returns 0 and prints entries on success', async () => {
    const cmd = new AuditCommand(makeDeps());
    const code = await cmd.run({});
    expect(code).toBe(0);
  });

  it('returns 4 when no token set (missing config)', async () => {
    const cmd = new AuditCommand(makeDeps({ getToken: () => null }));
    const code = await cmd.run({});
    expect(code).toBe(4);
  });

  // CHG-2026-04-23-AUDIT-14: when API rejects token (401), exit code must be
  // 4 per Rule 4. Previously the ApiError escaped and register's withTimeout
  // catch returned 3 (unexpected error) — CI scripts couldn't distinguish
  // auth failure from other failures.
  it('returns 4 when API throws 401 (token expired/revoked)', async () => {
    const errs: string[] = [];
    const cmd = new AuditCommand(makeDeps({
      getAudit: async () => {
        throw new ApiError('Invalid or expired token', 4, 401, 'AUTH_REQUIRED');
      },
      writeError: (m: string) => errs.push(m),
    }));
    const code = await cmd.run({});
    expect(code).toBe(4);
    expect(errs.join('\n')).toContain('loopzero login');
  });

  it('returns 4 as JSON on 401 with AUTH_REQUIRED code', async () => {
    const out: string[] = [];
    const cmd = new AuditCommand(makeDeps({
      getAudit: async () => {
        throw new ApiError('Invalid or expired token', 4, 401, 'AUTH_REQUIRED');
      },
      writeTo: (m: string) => out.push(m),
    }));
    const code = await cmd.run({ json: true });
    expect(code).toBe(4);
    const parsed = JSON.parse(out[0]);
    expect(parsed.exit_code).toBe(4);
    expect(parsed.code).toBe('AUTH_REQUIRED');
  });

  it('returns 4 on 403 (permission denied — also Rule 4 auth family)', async () => {
    const cmd = new AuditCommand(makeDeps({
      getAudit: async () => {
        throw new ApiError('Forbidden', 4, 403, 'FORBIDDEN');
      },
    }));
    const code = await cmd.run({});
    expect(code).toBe(4);
  });

  it('returns 1 on unexpected error', async () => {
    const cmd = new AuditCommand(makeDeps({
      getAudit: async () => { throw new Error('network down'); },
    }));
    const code = await cmd.run({});
    expect(code).toBe(1);
  });

  // CHG-2026-04-23-AUDIT-23: every error branch of `loopzero audit --json` must
  // emit machine-parseable JSON on stdout (Rule 5 / J5.1 stability contract).
  // Previously only the initial "not authenticated" branch honored --json;
  // network / 403 / null-data branches wrote plain text via writeError,
  // breaking `loopzero audit --json 2>&1 | jq`.
  describe('JSON contract (AUDIT-23)', () => {
    it('network error in --json produces JSON on stdout (not text on stderr)', async () => {
      const out: string[] = [];
      const errs: string[] = [];
      const cmd = new AuditCommand(makeDeps({
        getAudit: async () => { throw new Error('fetch failed'); },
        writeTo: (m: string) => out.push(m),
        writeError: (m: string) => errs.push(m),
      }));

      const code = await cmd.run({ json: true });

      expect(code).toBe(1);
      expect(errs).toHaveLength(0); // no plain-text stderr in JSON mode
      expect(out).toHaveLength(1);
      const parsed = JSON.parse(out[0]);
      expect(parsed.exit_code).toBe(1);
      expect(parsed.error).toContain('fetch failed');
      // AUDIT-23 aligns with AUDIT-22: network failures get API_UNREACHABLE.
      expect(parsed.code).toBe('API_UNREACHABLE');
    });

    it('403 in --json produces JSON (not plain-text writeError)', async () => {
      const out: string[] = [];
      const errs: string[] = [];
      const cmd = new AuditCommand(makeDeps({
        getAudit: async () => { throw new ApiError('Forbidden', 4, 403, 'FORBIDDEN'); },
        writeTo: (m: string) => out.push(m),
        writeError: (m: string) => errs.push(m),
      }));

      const code = await cmd.run({ json: true });

      expect(code).toBe(4);
      expect(errs).toHaveLength(0);
      expect(out).toHaveLength(1);
      const parsed = JSON.parse(out[0]);
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBeDefined();
    });

    it('null data in --json produces JSON (not plain-text writeError)', async () => {
      const out: string[] = [];
      const errs: string[] = [];
      const cmd = new AuditCommand(makeDeps({
        getAudit: async () => null,
        writeTo: (m: string) => out.push(m),
        writeError: (m: string) => errs.push(m),
      }));

      const code = await cmd.run({ json: true });

      expect(code).toBe(1);
      expect(errs).toHaveLength(0);
      expect(out).toHaveLength(1);
      const parsed = JSON.parse(out[0]);
      expect(parsed.exit_code).toBe(1);
    });
  });
});
