// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { LogoutCommand, LogoutDeps } from '../../../src/commands/logout';

function createMockDeps(overrides: Partial<LogoutDeps> = {}): LogoutDeps {
  return {
    clearToken: jest.fn(),
    writeTo: jest.fn(),
    confirm: jest.fn().mockResolvedValue(true),
    isCI: false,
    ...overrides,
  };
}

describe('LogoutCommand', () => {
  it('should clear token and return exit code 0 when confirmed', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    const exitCode = await cmd.execute();

    expect(deps.confirm).toHaveBeenCalled();
    expect(deps.clearToken).toHaveBeenCalled();
    expect(exitCode).toBe(0);
  });

  it('should output human-readable confirmation', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    await cmd.execute();

    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
    expect(output).toContain('Logged out');
  });

  it('should output JSON when json flag is set', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    await cmd.execute({ json: true, yes: true });

    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
    const parsed = JSON.parse(output);
    expect(parsed.authenticated).toBe(false);
  });

  it('should not throw if no token exists', async () => {
    const deps = createMockDeps({
      clearToken: jest.fn(),
    });
    const cmd = new LogoutCommand(deps);

    const exitCode = await cmd.execute({ yes: true });
    expect(exitCode).toBe(0);
  });

  it('should not clear token when user declines confirmation', async () => {
    const deps = createMockDeps({
      confirm: jest.fn().mockResolvedValue(false),
    });
    const cmd = new LogoutCommand(deps);

    const exitCode = await cmd.execute();

    expect(deps.clearToken).not.toHaveBeenCalled();
    expect(exitCode).toBe(0);
    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
    expect(output).toContain('cancelled');
  });

  it('should output JSON cancelled when user declines with --json', async () => {
    const deps = createMockDeps({
      confirm: jest.fn().mockResolvedValue(false),
    });
    const cmd = new LogoutCommand(deps);

    await cmd.execute({ json: true });

    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
    const parsed = JSON.parse(output);
    expect(parsed.status).toBe('cancelled');
  });

  it('should skip confirmation with --yes flag', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    await cmd.execute({ yes: true });

    expect(deps.confirm).not.toHaveBeenCalled();
    expect(deps.clearToken).toHaveBeenCalled();
  });

  it('should skip confirmation in CI mode', async () => {
    const deps = createMockDeps({ isCI: true });
    const cmd = new LogoutCommand(deps);

    await cmd.execute();

    expect(deps.confirm).not.toHaveBeenCalled();
    expect(deps.clearToken).toHaveBeenCalled();
  });

  // F-06 (CHG-2026-04-29-DOGFOOD T1.2) — quando hasToken() retorna false, o
  // CLI emite "Already logged out." em vez de "Logout cancelled.". Sem
  // hasToken (deps anteriores), comportamento default fica intacto.
  describe('F-06: not authenticated path', () => {
    it('emits "Already logged out." when hasToken returns false (human mode)', async () => {
      const deps = createMockDeps({ hasToken: jest.fn().mockReturnValue(false) });
      const cmd = new LogoutCommand(deps);

      const exitCode = await cmd.execute();

      const lines = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(lines.some(l => /Already logged out/i.test(l))).toBe(true);
      expect(lines.some(l => /Logout cancelled/i.test(l))).toBe(false);
      expect(exitCode).toBe(0);
      expect(deps.confirm).not.toHaveBeenCalled(); // skip prompt — nothing to cancel
    });

    it('emits status:already_logged_out in JSON when hasToken returns false', async () => {
      const deps = createMockDeps({ hasToken: jest.fn().mockReturnValue(false) });
      const cmd = new LogoutCommand(deps);

      await cmd.execute({ json: true });

      const out = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(out);
      expect(parsed.authenticated).toBe(false);
      expect(parsed.status).toBe('already_logged_out');
    });

    it('still calls clearToken (idempotent cleanup of any stale state)', async () => {
      const deps = createMockDeps({ hasToken: jest.fn().mockReturnValue(false) });
      const cmd = new LogoutCommand(deps);

      await cmd.execute();

      expect(deps.clearToken).toHaveBeenCalledTimes(1);
    });

    it('falls back to original "Logout cancelled" path when hasToken is undefined (backward compat)', async () => {
      const deps = createMockDeps({ confirm: jest.fn().mockResolvedValue(false) });
      const cmd = new LogoutCommand(deps);

      await cmd.execute();

      const lines = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(lines.some(l => /Logout cancelled/.test(l))).toBe(true);
    });
  });
});
