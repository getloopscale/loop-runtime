// CHG-2026-04-29-DOGFOOD-REAL-E2E iter 13 — Rule 8 fail-clear regression.
// /api/v1/domains responds with {"error": "..."} not the canonical
// {"code","message"}. Without body.error fallback, the CLI swallowed the
// server's specific reject message ("'X' is a reserved domain") into the
// generic "Request failed (HTTP 400)". That was observed in journey-07.

import { describe, it, expect } from '@jest/globals';
import { ApiClient } from '../../src/api/client';

class TestableApiClient extends ApiClient {
  public callMapError(status: number, body: unknown) {
    // Access private mapError via reflection — the symbol shape used by
    // the unit test is intentional: we want to assert the contract the
    // private helper exposes to internal callers.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return (this as any).mapError(status, body);
  }
}

describe('ApiClient.mapError — body shape resilience', () => {
  const client = new TestableApiClient({
    baseURL: 'http://test.invalid',
    token: 'tok',
  });

  it('uses body.message when present (canonical shape)', () => {
    const err = client.callMapError(409, { code: 'DEPLOY_IN_PROGRESS', message: 'A deploy is in progress.' });
    expect(err.message).toContain('A deploy is in progress.');
    expect(err.code).toBe('DEPLOY_IN_PROGRESS');
  });

  it('falls back to body.error when only the legacy shape is present', () => {
    // /api/v1/domains uses {"error": "..."} — the CLI must surface that
    // verbatim instead of the generic "Request failed (HTTP 400)".
    const err = client.callMapError(400, { error: "'api.example.test' is a reserved domain and cannot be registered" });
    expect(err.message).toContain('reserved domain');
    expect(err.statusCode).toBe(400);
  });

  it('prefers body.message over body.error if both are present', () => {
    const err = client.callMapError(400, { message: 'specific', error: 'fallback' });
    expect(err.message).toContain('specific');
  });

  it('uses generic fallback when neither field is present', () => {
    const err = client.callMapError(400, {});
    expect(err.message).toContain('Request failed (HTTP 400)');
  });
});
