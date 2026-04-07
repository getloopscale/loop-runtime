// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient, ApiError } from '../../src/api/client';

describe('ApiClient token refresh on 401', () => {
  function mockFetch(responses: Array<{ status: number; body: unknown }>): jest.Mock {
    const fn = jest.fn();
    for (const r of responses) {
      fn.mockResolvedValueOnce({
        ok: r.status >= 200 && r.status < 300,
        status: r.status,
        json: async () => r.body,
      });
    }
    return fn;
  }

  it('retries with new token after 401 when onUnauthorized succeeds', async () => {
    const fetchFn = mockFetch([
      { status: 401, body: { code: 'AUTH_REQUIRED', message: 'Not authenticated' } },
      { status: 200, body: { ok: true } },
    ]);
    const onUnauthorized = jest.fn().mockResolvedValue('refreshed-token');

    const client = new ApiClient({
      baseURL: 'http://test',
      token: 'expired-token',
      fetchFn,
      onUnauthorized,
      maxRetries: 0,
    });

    const result = await client.get('/test');
    expect(result).toEqual({ ok: true });
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
    // Second call should use the refreshed token
    expect(fetchFn).toHaveBeenCalledTimes(2);
    const secondCallHeaders = fetchFn.mock.calls[1][1].headers;
    expect(secondCallHeaders['Authorization']).toBe('Bearer refreshed-token');
  });

  it('throws 401 error when onUnauthorized returns null', async () => {
    const fetchFn = mockFetch([
      { status: 401, body: { code: 'AUTH_REQUIRED', message: 'Not authenticated' } },
    ]);
    const onUnauthorized = jest.fn().mockResolvedValue(null);

    const client = new ApiClient({
      baseURL: 'http://test',
      token: 'expired-token',
      fetchFn,
      onUnauthorized,
      maxRetries: 0,
    });

    try {
      await client.get('/test');
      fail('Expected ApiError');
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).exitCode).toBe(4);
    }
  });

  it('throws 401 error when no onUnauthorized callback is provided', async () => {
    const fetchFn = mockFetch([
      { status: 401, body: { code: 'AUTH_REQUIRED', message: 'Not authenticated' } },
    ]);

    const client = new ApiClient({
      baseURL: 'http://test',
      token: 'expired-token',
      fetchFn,
      maxRetries: 0,
    });

    await expect(client.get('/test')).rejects.toThrow(ApiError);
  });

  it('does not retry refresh more than once (prevents infinite loop)', async () => {
    const fetchFn = mockFetch([
      { status: 401, body: { code: 'AUTH_REQUIRED', message: 'First 401' } },
      { status: 401, body: { code: 'AUTH_REQUIRED', message: 'Second 401 after refresh' } },
    ]);
    const onUnauthorized = jest.fn().mockResolvedValue('refreshed-but-still-bad');

    const client = new ApiClient({
      baseURL: 'http://test',
      token: 'bad-token',
      fetchFn,
      onUnauthorized,
      maxRetries: 0,
    });

    await expect(client.get('/test')).rejects.toThrow(ApiError);
    // Should only call refresh once, not loop
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
    expect(fetchFn).toHaveBeenCalledTimes(2);
  });
});
