// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { formatErrorMessage, classifyNetworkError } from '../../../src/output/error-message';

describe('formatErrorMessage', () => {
  it('strips trailing period from err message (no double punctuation)', () => {
    // AUDIT-8 regression: real API message "Plan limit reached. Upgrade your plan to continue."
    // previously rendered as "...continue.. Try again." (double period).
    const result = formatErrorMessage(
      'Failed to create project',
      'Plan limit reached: Starter allows 1 projects (current: 2). Upgrade your plan to continue.',
      'Try again',
    );
    expect(result).toBe(
      'Failed to create project: Plan limit reached: Starter allows 1 projects (current: 2). Upgrade your plan to continue. Try again.',
    );
    expect(result).not.toContain('..');
  });

  it('adds a single period when err message has none', () => {
    const result = formatErrorMessage('Failed to list projects', 'Network timeout', 'Try again');
    expect(result).toBe('Failed to list projects: Network timeout. Try again.');
  });

  it('works without a hint', () => {
    const result = formatErrorMessage('Failed to add member', 'User not found.');
    expect(result).toBe('Failed to add member: User not found.');
    expect(result).not.toContain('..');
  });

  it('strips multiple trailing periods', () => {
    // Defensive: API bug could emit "...continue..."
    const result = formatErrorMessage('Op failed', 'Bad state...', 'Retry');
    expect(result).toBe('Op failed: Bad state. Retry.');
  });

  it('strips trailing period from hint too', () => {
    const result = formatErrorMessage('Op failed', 'Reason', 'Retry later.');
    expect(result).toBe('Op failed: Reason. Retry later.');
  });

  it('trims leading/trailing whitespace around periods', () => {
    const result = formatErrorMessage('Op failed', 'Reason.   ', 'Retry');
    expect(result).toBe('Op failed: Reason. Retry.');
  });

  it('does nothing surprising for a clean pair', () => {
    const result = formatErrorMessage('Failed', 'Nope', 'Try again');
    expect(result).toBe('Failed: Nope. Try again.');
  });
});

// CHG-2026-04-23-AUDIT-24: unifies "API unreachable" detection across
// all list/inspect commands. Without this helper each command wrote its
// own ad-hoc string matching ("fetch failed" vs "ECONNREFUSED" vs
// "network down"), leading to inconsistent UX — some commands hinted
// "Check LOOPZERO_URL", others said the generic "Try again later".
describe('classifyNetworkError', () => {
  it('detects "fetch failed" (undici/Node fetch)', () => {
    const result = classifyNetworkError(new Error('fetch failed'));
    expect(result.isNetwork).toBe(true);
    expect(result.message).toBe('Cannot reach API: fetch failed. Check LOOPZERO_URL.');
  });

  it('detects ECONNREFUSED', () => {
    const result = classifyNetworkError(new Error('connect ECONNREFUSED 127.0.0.1:8080'));
    expect(result.isNetwork).toBe(true);
    expect(result.message).toContain('ECONNREFUSED');
    expect(result.message).toContain('Check LOOPZERO_URL');
  });

  it('detects ENOTFOUND (DNS failure)', () => {
    const result = classifyNetworkError(new Error('getaddrinfo ENOTFOUND wrong.host'));
    expect(result.isNetwork).toBe(true);
    expect(result.message).toContain('ENOTFOUND');
  });

  it('detects ETIMEDOUT', () => {
    const result = classifyNetworkError(new Error('connect ETIMEDOUT'));
    expect(result.isNetwork).toBe(true);
  });

  it('returns isNetwork=false for non-network Error', () => {
    const result = classifyNetworkError(new Error('Some business logic error'));
    expect(result.isNetwork).toBe(false);
  });

  it('returns isNetwork=false for non-Error values', () => {
    expect(classifyNetworkError(null).isNetwork).toBe(false);
    expect(classifyNetworkError(undefined).isNetwork).toBe(false);
    expect(classifyNetworkError('string error').isNetwork).toBe(false);
    expect(classifyNetworkError({ message: 'not an Error' }).isNetwork).toBe(false);
  });

  it('message ends with single period (no double)', () => {
    const result = classifyNetworkError(new Error('fetch failed.'));
    expect(result.message).toBe('Cannot reach API: fetch failed. Check LOOPZERO_URL.');
    expect(result.message).not.toContain('..');
  });
});
