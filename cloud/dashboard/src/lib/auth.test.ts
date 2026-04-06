import { describe, it, expect } from 'vitest';
import { isEmailVerified } from './auth';
import type { Session } from './auth';

// Helper to build a minimal session object for testing.
function makeSession(overrides: Partial<Session> = {}): Session {
  return {
    active: true,
    identity: {
      id: 'test-id',
      traits: { email: 'user@example.com' },
    },
    ...overrides,
  };
}

describe('isEmailVerified', () => {
  // Scenario 1: Password user with unverified email → should return false
  it('returns false for password user with unverified email', () => {
    const session = makeSession({
      identity: {
        id: 'pwd-user',
        traits: { email: 'user@example.com' },
        verifiable_addresses: [
          { value: 'user@example.com', verified: false, via: 'email' },
        ],
      },
    });
    expect(isEmailVerified(session)).toBe(false);
  });

  // Scenario 2: Password user with verified email → should return true
  it('returns true for password user with verified email', () => {
    const session = makeSession({
      identity: {
        id: 'pwd-user',
        traits: { email: 'user@example.com' },
        verifiable_addresses: [
          { value: 'user@example.com', verified: true, via: 'email' },
        ],
      },
    });
    expect(isEmailVerified(session)).toBe(true);
  });

  // Scenario 3: OIDC user with NO verifiable_addresses → should return true
  // (current behavior — OIDC users without verifiable_addresses are treated as verified)
  it('returns true for OIDC user with no verifiable_addresses', () => {
    const session = makeSession({
      identity: {
        id: 'oidc-user',
        traits: { email: 'user@gmail.com' },
        // No verifiable_addresses field at all
      },
    });
    expect(isEmailVerified(session)).toBe(true);
  });

  // Scenario 4: OIDC user (Google) with verifiable_addresses marked false
  // → should return true because provider already verified the email.
  // THIS IS THE BUG: currently returns false because it only checks verifiable_addresses.
  it('returns true for Google OIDC user even with unverified verifiable_addresses', () => {
    const session = makeSession({
      identity: {
        id: 'google-user',
        traits: { email: 'user@gmail.com' },
        verifiable_addresses: [
          { value: 'user@gmail.com', verified: false, via: 'email' },
        ],
      },
      authentication_methods: [
        { method: 'oidc', provider: 'google', completed_at: '2026-04-06T00:00:00Z' },
      ],
    });
    expect(isEmailVerified(session)).toBe(true);
  });

  // Scenario 5: GitHub OIDC user with verifiable_addresses marked false
  // → should return true (same as Google)
  it('returns true for GitHub OIDC user even with unverified verifiable_addresses', () => {
    const session = makeSession({
      identity: {
        id: 'github-user',
        traits: { email: 'user@github.com' },
        verifiable_addresses: [
          { value: 'user@github.com', verified: false, via: 'email' },
        ],
      },
      authentication_methods: [
        { method: 'oidc', provider: 'github', completed_at: '2026-04-06T00:00:00Z' },
      ],
    });
    expect(isEmailVerified(session)).toBe(true);
  });

  // Scenario 6: Untrusted OIDC provider with unverified email → should return false
  // Security: only explicitly trusted providers (github, google) bypass verification.
  it('returns false for untrusted OIDC provider with unverified email', () => {
    const session = makeSession({
      identity: {
        id: 'untrusted-user',
        traits: { email: 'user@evil.com' },
        verifiable_addresses: [
          { value: 'user@evil.com', verified: false, via: 'email' },
        ],
      },
      authentication_methods: [
        { method: 'oidc', provider: 'some-random-provider', completed_at: '2026-04-06T00:00:00Z' },
      ],
    });
    expect(isEmailVerified(session)).toBe(false);
  });

  // Scenario 7: Password-only auth with unverified email (no OIDC in methods)
  // → should return false (password users must verify)
  it('returns false for password-only user with unverified email and authentication_methods', () => {
    const session = makeSession({
      identity: {
        id: 'pwd-user-2',
        traits: { email: 'user@example.com' },
        verifiable_addresses: [
          { value: 'user@example.com', verified: false, via: 'email' },
        ],
      },
      authentication_methods: [
        { method: 'password', completed_at: '2026-04-06T00:00:00Z' },
      ],
    });
    expect(isEmailVerified(session)).toBe(false);
  });
});
