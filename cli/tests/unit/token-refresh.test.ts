// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ensureFreshToken, TokenRefreshDeps } from '../../src/auth/token-refresh';

describe('Token Refresh', () => {
  function createDeps(overrides: Partial<TokenRefreshDeps> = {}): TokenRefreshDeps {
    return {
      getToken: jest.fn().mockReturnValue('current-token'),
      getRefreshToken: jest.fn().mockReturnValue('refresh-token'),
      isTokenExpired: jest.fn().mockReturnValue(false),
      saveToken: jest.fn(),
      getEmail: jest.fn().mockReturnValue('user@test.com'),
      getTeam: jest.fn().mockReturnValue('acme'),
      fetchFn: jest.fn(),
      writeError: jest.fn(),
      ...overrides,
    };
  }

  it('should return token as-is when not expired', async () => {
    const deps = createDeps();
    const token = await ensureFreshToken(deps);

    expect(token).toBe('current-token');
    expect(deps.fetchFn).not.toHaveBeenCalled();
  });

  it('should return null when no token exists', async () => {
    const deps = createDeps({ getToken: jest.fn().mockReturnValue(null) });
    const token = await ensureFreshToken(deps);
    expect(token).toBeNull();
  });

  it('should refresh expired token silently', async () => {
    const fetchFn = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        access_token: 'new-token',
        refresh_token: 'new-refresh',
        token_type: 'bearer',
        expires_in: 3600,
      }),
    });

    const deps = createDeps({
      isTokenExpired: jest.fn().mockReturnValue(true),
      fetchFn,
    });

    const token = await ensureFreshToken(deps);

    expect(token).toBe('new-token');
    expect(deps.saveToken).toHaveBeenCalledWith('new-token', 'user@test.com', 'acme', 'new-refresh', 3600);
    expect(deps.writeError).not.toHaveBeenCalled();
  });

  it('should show error when refresh fails', async () => {
    const fetchFn = jest.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({ error: 'invalid_grant' }),
    });

    const deps = createDeps({
      isTokenExpired: jest.fn().mockReturnValue(true),
      fetchFn,
    });

    const token = await ensureFreshToken(deps);

    expect(token).toBeNull();
    expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('Session expired'));
  });

  it('should show error when expired and no refresh token', async () => {
    const deps = createDeps({
      isTokenExpired: jest.fn().mockReturnValue(true),
      getRefreshToken: jest.fn().mockReturnValue(null),
    });

    const token = await ensureFreshToken(deps);

    expect(token).toBeNull();
    expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('loopzero login'));
  });
});
