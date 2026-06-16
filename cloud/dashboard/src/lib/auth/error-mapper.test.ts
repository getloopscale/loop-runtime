import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  mapKratosError,
  KRATOS_ERROR_MAP,
  type KratosErrorInput,
  type KratosErrorMapped,
} from './error-mapper';

// T1.1 — RED specs for the Kratos error mapper. Plan v1.1
// .claude/knowledge-base/plans/2026-06-16-m0-j01-login-polish-plan.md
// Phase 1, Task T1.1. The 9 specs cover: known/unknown IDs, the console.error
// side-effect (ADR D2 — debuggability), the pre-seeded identity collision
// (ADR D3 — EC-J01-1 from blueprint), the 422+redirect chain (ADR D4),
// and four EC-1 input-tolerance branches (null/undefined/missing/partial).

describe('mapKratosError — return shape per known IDs', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('returns product-language for a known ID', () => {
    const input: KratosErrorInput = { error: { id: 'session_inactive' } };
    const out = mapKratosError(input);
    expect(out.message).not.toEqual('');
    expect(out.suggestion).not.toEqual('');
    expect(typeof out.retryable).toBe('boolean');
  });

  it('returns generic fallback for an unknown ID', () => {
    const input: KratosErrorInput = { error: { id: 'no_such_kratos_id_zzz' } };
    const out = mapKratosError(input);
    // Fallback is canonical — known fields present, retryable=false to prevent
    // infinite-retry loops on unknown errors.
    expect(out.message).not.toEqual('');
    expect(out.retryable).toBe(false);
  });
});

describe('mapKratosError — side effects + canonical EC scenarios', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('calls console.error with the raw input BEFORE returning (ADR D2)', () => {
    const input: KratosErrorInput = { error: { id: 'session_inactive' } };
    mapKratosError(input);
    expect(console.error).toHaveBeenCalled();
    // The raw input must be in the call args (so DevTools paste reveals
    // the original Kratos shape).
    const calls = (console.error as ReturnType<typeof vi.fn>).mock.calls;
    const hasRawInput = calls.some((args: unknown[]) =>
      args.some((arg) => arg === input),
    );
    expect(hasRawInput).toBe(true);
  });

  it('handles pre-seeded identity collision distinctly (ADR D3 / EC-J01-1)', () => {
    const input: KratosErrorInput = {
      error: { id: 'account_enumeration_mitigation' },
    };
    const out = mapKratosError(input);
    // Distinct message — NOT the generic "Login failed" fallback. Must
    // explicitly steer the user to a different email or support.
    expect(out.message.toLowerCase()).toMatch(/different email|contact support/);
    expect(out.retryable).toBe(false);
  });

  it('handles 422 + redirect_browser_to chain (ADR D4)', () => {
    const input: KratosErrorInput = {
      redirect_browser_to: 'https://auth-dev.loopzero.example/self-service/login/methods/oidc/callback/github',
    };
    const out = mapKratosError(input);
    expect(out.retryable).toBe(false);
    expect(out.redirectTo).toEqual(
      'https://auth-dev.loopzero.example/self-service/login/methods/oidc/callback/github',
    );
  });
});

describe('mapKratosError — EC-1 input contract tolerance', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('tolerates null without throwing (EC-1.a)', () => {
    expect(() => mapKratosError(null)).not.toThrow();
    const out = mapKratosError(null);
    expect(out.retryable).toBe(false);
    expect(out.message).not.toEqual('');
  });

  it('tolerates undefined without throwing (EC-1.b)', () => {
    expect(() => mapKratosError(undefined)).not.toThrow();
    const out = mapKratosError(undefined);
    expect(out.retryable).toBe(false);
    expect(out.message).not.toEqual('');
  });

  it('tolerates missing .error field (EC-1.c)', () => {
    const input = {} as KratosErrorInput;
    const out = mapKratosError(input);
    expect(out.retryable).toBe(false);
    expect(out.message).not.toEqual('');
  });

  it('tolerates partial .error field (no .id) (EC-1.d)', () => {
    const input: KratosErrorInput = { error: {} };
    const out = mapKratosError(input);
    expect(out.retryable).toBe(false);
    expect(out.message).not.toEqual('');
  });
});

describe('KRATOS_ERROR_MAP — T1.2 catalog coverage', () => {
  // Per plan Appendix A, the 12 canonical Kratos v26 IDs (6 starter + 6
  // extensions). The map MUST have a row for each so T1.1 fallback never
  // fires for a documented ID — drift-prevention.
  const catalogIds = [
    'browser_location_change_required',
    'session_inactive',
    'security_csrf_violation',
    'security_identity_mismatch',
    'oauth2_authorization_denied',
    'account_enumeration_mitigation',
    'self_service_flow_expired',
    'session_already_available',
    'session_aal2_required',
    'session_refresh_required',
    'internal_server_error',
    'rate_limit_exceeded',
  ];

  it.each(catalogIds)('has a canonical entry for %s', (id) => {
    expect(KRATOS_ERROR_MAP).toHaveProperty(id);
    const entry: KratosErrorMapped = KRATOS_ERROR_MAP[id];
    expect(entry.message).not.toEqual('');
    expect(entry.suggestion).not.toEqual('');
    expect(typeof entry.retryable).toBe('boolean');
  });

  it('Rule 3 compliance — no internal terms (Kratos|Hydra|OIDC) in message/suggestion', () => {
    const banned = /\b(kratos|hydra|oidc)\b/i;
    for (const id of catalogIds) {
      const entry = KRATOS_ERROR_MAP[id];
      expect(entry.message, `${id}.message`).not.toMatch(banned);
      expect(entry.suggestion, `${id}.suggestion`).not.toMatch(banned);
    }
  });
});
