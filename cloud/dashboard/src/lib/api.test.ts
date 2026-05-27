import { describe, it, expect, vi, beforeEach } from 'vitest';
import { engineFetch, exchangeSessionToken } from './api';

vi.mock('./config', () => ({ API_URL: 'http://test.local' }));

describe('engineFetch — signal propagation (EC-03)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('test_engineFetch_propagates_signal_to_fetch', async () => {
    // First mock: token exchange returns valid JWT shape
    let callCount = 0;
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
      callCount++;
      if (callCount === 1) {
        // Token exchange
        return new Response(
          JSON.stringify({ access_token: 'tk', expires_in: 3600 }),
          { status: 200 },
        );
      }
      // Actual data call
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    });

    // Prime the token cache so the actual data call uses our signal
    await exchangeSessionToken();

    const controller = new AbortController();
    await engineFetch('/foo', { signal: controller.signal });

    // The last fetch call is the data call (after token cache primed)
    expect(fetchSpy.mock.calls.length).toBeGreaterThanOrEqual(2);
    const dataCall = fetchSpy.mock.calls[fetchSpy.mock.calls.length - 1];
    const init = dataCall[1] as RequestInit;
    expect(init.signal).toBe(controller.signal);
  });
});

describe('fetchWithAuth — Idempotency-Key auto-gen (T5.0 / EC-09)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  async function primeTokenAndCapture(method: string, explicitKey?: string): Promise<Record<string, string>> {
    let callCount = 0;
    const captured: Record<string, string> = {};
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, init) => {
      callCount++;
      if (callCount === 1) {
        return new Response(JSON.stringify({ access_token: 'tk', expires_in: 3600 }), { status: 200 });
      }
      Object.assign(captured, (init as RequestInit).headers as Record<string, string>);
      return new Response(JSON.stringify({}), { status: 200 });
    });
    await exchangeSessionToken();
    const requestInit: RequestInit = { method };
    if (explicitKey) (requestInit.headers as Record<string, string>) = { 'Idempotency-Key': explicitKey };
    await engineFetch('/foo', requestInit);
    return captured;
  }

  it('test_POST_request_gets_auto_generated_uuid_idempotency_key', async () => {
    const headers = await primeTokenAndCapture('POST');
    expect(headers['Idempotency-Key']).toBeDefined();
    // RFC 4122 UUID v4-ish shape (relaxed: 36 chars with 4 hyphens).
    expect(headers['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/i);
  });

  it('test_PATCH_and_DELETE_and_PUT_also_get_keys', async () => {
    for (const method of ['PATCH', 'DELETE', 'PUT']) {
      const h = await primeTokenAndCapture(method);
      expect(h['Idempotency-Key'], `method=${method}`).toMatch(/^[0-9a-f-]{36}$/i);
    }
  });

  it('test_GET_request_gets_no_idempotency_key', async () => {
    const headers = await primeTokenAndCapture('GET');
    expect(headers['Idempotency-Key']).toBeUndefined();
  });

  it('test_caller_explicit_key_wins_over_auto_gen', async () => {
    const headers = await primeTokenAndCapture('POST', 'caller-deliberate-replay-key-001');
    expect(headers['Idempotency-Key']).toBe('caller-deliberate-replay-key-001');
  });
});
