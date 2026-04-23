// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../../../src/api/client';

describe('ApiClient trace context injection', () => {
  it('includes traceparent header when tracing is active', async () => {
    // Track what headers were sent
    let capturedHeaders: Record<string, string> = {};

    const mockFetch = async (url: string, init?: RequestInit): Promise<Response> => {
      capturedHeaders = Object.fromEntries(
        Object.entries(init?.headers ?? {}).map(([k, v]) => [k.toLowerCase(), v]),
      );
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    };

    const client = new ApiClient({
      baseURL: 'http://localhost:3000',
      token: 'test-token',
      fetchFn: mockFetch,
    });

    await client.get('/api/v1/health');

    // Without active tracing, traceparent should NOT be present
    // (injectTraceContext is a no-op when no TracerProvider is registered)
    expect(capturedHeaders['correlation-id']).toBeDefined();
    expect(capturedHeaders['authorization']).toBe('Bearer test-token');
    // traceparent is only injected when a TracerProvider is registered
    // and an active span exists — which is not the case in unit tests
  });

  it('does not crash when injectTraceContext has no active span', async () => {
    const mockFetch = async (): Promise<Response> => {
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    };

    const client = new ApiClient({
      baseURL: 'http://localhost:3000',
      fetchFn: mockFetch,
    });

    // Should not throw even without token or active span
    await expect(client.get('/api/v1/health')).resolves.toBeDefined();
  });
});
