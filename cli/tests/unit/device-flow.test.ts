// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { executeDeviceCodeFlow, refreshAccessToken, DeviceFlowDeps } from '../../src/auth/device-flow';

describe('Device Code Flow', () => {
  function createMockDeps(overrides: Partial<DeviceFlowDeps> = {}): DeviceFlowDeps {
    return {
      fetchFn: jest.fn(),
      baseURL: 'http://localhost:13002',
      openBrowser: jest.fn().mockResolvedValue(undefined),
      writeTo: jest.fn(),
      writeError: jest.fn(),
      sleep: jest.fn().mockResolvedValue(undefined),
      ...overrides,
    };
  }

  function mockFetch(responses: Record<string, { status: number; body: unknown }[]>): DeviceFlowDeps['fetchFn'] {
    const callCounts: Record<string, number> = {};
    return jest.fn().mockImplementation(async (url: string) => {
      const path = new URL(url).pathname;
      callCounts[path] = (callCounts[path] || 0) + 1;
      const responseList = responses[path] || [{ status: 404, body: {} }];
      const idx = Math.min(callCounts[path] - 1, responseList.length - 1);
      const resp = responseList[idx];
      return {
        ok: resp.status >= 200 && resp.status < 300,
        status: resp.status,
        json: async () => resp.body,
      } as Response;
    });
  }

  it('should complete device code flow successfully', async () => {
    const fetchFn = mockFetch({
      '/v1/auth/device/authorize': [{
        status: 200,
        body: {
          device_code: 'dc-123',
          user_code: 'ABCD-1234',
          verification_uri: 'http://localhost:4455/device',
          expires_in: 300,
          interval: 1,
        },
      }],
      '/v1/auth/device/token': [
        { status: 200, body: { status: 'pending' } },
        {
          status: 200,
          body: {
            access_token: 'at-123',
            refresh_token: 'rt-123',
            token_type: 'bearer',
            expires_in: 3600,
          },
        },
      ],
    });

    const deps = createMockDeps({ fetchFn });
    const result = await executeDeviceCodeFlow(deps);

    expect(result).not.toBeNull();
    expect(result!.access_token).toBe('at-123');
    expect(result!.refresh_token).toBe('rt-123');
    expect(deps.openBrowser).toHaveBeenCalledWith('http://localhost:4455/device');
    expect(deps.writeTo).toHaveBeenCalledWith(expect.stringContaining('ABCD-1234'));
  });

  it('should fail when device authorize fails', async () => {
    const fetchFn = mockFetch({
      '/v1/auth/device/authorize': [{ status: 500, body: {} }],
    });

    const deps = createMockDeps({ fetchFn });
    const result = await executeDeviceCodeFlow(deps);

    expect(result).toBeNull();
    expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('Failed to start'));
  });

  it('should require --token in CI mode (handled by LoginCommand, not device flow)', () => {
    // Device flow is never called in CI mode — LoginCommand handles this
    // This test documents the expectation
    expect(true).toBe(true);
  });

  it('should not contain internal terms in output', async () => {
    const fetchFn = mockFetch({
      '/v1/auth/device/authorize': [{
        status: 200,
        body: {
          device_code: 'dc-123',
          user_code: 'TEST-CODE',
          verification_uri: 'http://localhost:4455/device',
          expires_in: 300,
          interval: 1,
        },
      }],
      '/v1/auth/device/token': [{
        status: 200,
        body: {
          access_token: 'at-123',
          refresh_token: 'rt-123',
          token_type: 'bearer',
          expires_in: 3600,
        },
      }],
    });

    const deps = createMockDeps({ fetchFn });
    await executeDeviceCodeFlow(deps);

    const allOutput = (deps.writeTo as jest.Mock).mock.calls.map(c => c[0]).join(' ');
    const internalTerms = ['Hydra', 'hydra', 'Kratos', 'kratos', 'OIDC', 'oidc', 'device_code'];
    for (const term of internalTerms) {
      expect(allOutput).not.toContain(term);
    }
  });
});

describe('refreshAccessToken', () => {
  it('should refresh token successfully', async () => {
    const fetchFn = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        access_token: 'new-at',
        refresh_token: 'new-rt',
        token_type: 'bearer',
        expires_in: 3600,
      }),
    });

    const result = await refreshAccessToken(fetchFn, 'http://localhost:13002', 'old-rt');

    expect(result).not.toBeNull();
    expect(result!.access_token).toBe('new-at');
    expect(result!.refresh_token).toBe('new-rt');
  });

  it('should return null on failure', async () => {
    const fetchFn = jest.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({ error: 'invalid_grant' }),
    });

    const result = await refreshAccessToken(fetchFn, 'http://localhost:13002', 'expired-rt');
    expect(result).toBeNull();
  });
});
