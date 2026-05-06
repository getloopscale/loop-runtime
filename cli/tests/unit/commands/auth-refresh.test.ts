// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// AuthRefreshCommand tests — closes F-01 + F-03 from /dogfood full
// report (2026-05-06). Validates contract: missing refresh_token →
// exit 4 with code MISSING_REFRESH_TOKEN; refresh fails → exit 4 with
// code REFRESH_FAILED; success → exit 0 + saveToken called with the
// fresh tokens.

import { AuthRefreshCommand, AuthRefreshDeps } from '../../../src/commands/auth-refresh';

function createMockDeps(overrides: Partial<AuthRefreshDeps> = {}): AuthRefreshDeps {
  return {
    getRefreshToken: jest.fn().mockReturnValue('rt_abc'),
    saveToken: jest.fn(),
    getEmail: jest.fn().mockReturnValue('user@example.com'),
    getTeam: jest.fn().mockReturnValue('acme'),
    refreshFn: jest.fn().mockResolvedValue({
      access_token: 'at_new',
      refresh_token: 'rt_new',
      expires_in: 3600,
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('AuthRefreshCommand', () => {
  describe('happy path', () => {
    it('exchanges refresh_token for fresh tokens and saves them', async () => {
      const deps = createMockDeps();
      const cmd = new AuthRefreshCommand(deps);

      const exitCode = await cmd.execute({});

      expect(deps.refreshFn).toHaveBeenCalledWith('rt_abc');
      expect(deps.saveToken).toHaveBeenCalledWith(
        'at_new',
        'user@example.com',
        'acme',
        'rt_new',
        3600,
      );
      expect(exitCode).toBe(0);
    });

    it('outputs human-readable success message', async () => {
      const deps = createMockDeps();
      const cmd = new AuthRefreshCommand(deps);

      await cmd.execute({});

      const messages = (deps.writeTo as jest.Mock).mock.calls.flat().join(' ');
      expect(messages).toContain('Refreshed credentials');
      expect(messages).toContain('3600');
    });

    it('outputs JSON with ok=true when --json', async () => {
      const deps = createMockDeps();
      const cmd = new AuthRefreshCommand(deps);

      await cmd.execute({ json: true });

      const written = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(written);
      expect(parsed).toEqual({ ok: true, expires_in: 3600, team: 'acme' });
    });
  });

  describe('missing refresh token', () => {
    it('returns exit 4 with code MISSING_REFRESH_TOKEN (json)', async () => {
      const deps = createMockDeps({
        getRefreshToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new AuthRefreshCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(4);
      expect(deps.refreshFn).not.toHaveBeenCalled();
      expect(deps.saveToken).not.toHaveBeenCalled();
      const written = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(written);
      expect(parsed.code).toBe('MISSING_REFRESH_TOKEN');
      expect(parsed.exit_code).toBe(4);
      expect(parsed.error).toContain('loopzero login');
    });

    it('hints at seed-local-user.sh in human mode', async () => {
      const deps = createMockDeps({
        getRefreshToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new AuthRefreshCommand(deps);

      await cmd.execute({});

      const errors = (deps.writeError as jest.Mock).mock.calls.flat().join(' ');
      expect(errors).toContain('seed-local-user.sh');
    });
  });

  describe('refresh failure', () => {
    it('returns exit 4 with code REFRESH_FAILED when refreshFn returns null', async () => {
      const deps = createMockDeps({
        refreshFn: jest.fn().mockResolvedValue(null),
      });
      const cmd = new AuthRefreshCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(4);
      expect(deps.saveToken).not.toHaveBeenCalled();
      const written = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(written);
      expect(parsed.code).toBe('REFRESH_FAILED');
    });
  });

  describe('Rule 3 — no internal terms', () => {
    const forbiddenTerms = [
      'argocd',
      'gitops',
      'gitea',
      'minio',
      'kaniko',
      'kubebuilder',
      'envtest',
      'tenant_id',
      'advisory lock',
      'pg_advisory',
    ];

    it('does not leak internal terms in success output', async () => {
      const deps = createMockDeps();
      const cmd = new AuthRefreshCommand(deps);

      await cmd.execute({});

      const all = [
        ...(deps.writeTo as jest.Mock).mock.calls.flat(),
        ...(deps.writeError as jest.Mock).mock.calls.flat(),
      ]
        .join(' ')
        .toLowerCase();
      for (const term of forbiddenTerms) {
        expect(all).not.toContain(term.toLowerCase());
      }
    });

    it('does not leak internal terms in error output (missing token)', async () => {
      const deps = createMockDeps({
        getRefreshToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new AuthRefreshCommand(deps);

      await cmd.execute({ json: true });

      const all = [
        ...(deps.writeTo as jest.Mock).mock.calls.flat(),
        ...(deps.writeError as jest.Mock).mock.calls.flat(),
      ]
        .join(' ')
        .toLowerCase();
      for (const term of forbiddenTerms) {
        expect(all).not.toContain(term.toLowerCase());
      }
    });
  });
});
