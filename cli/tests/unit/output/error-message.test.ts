// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { formatErrorMessage } from '../../../src/output/error-message';

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
