// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { promptConfirm } from '../../../src/output/prompt-confirm';

describe('promptConfirm', () => {
  const originalIsTTY = process.stdin.isTTY;

  afterEach(() => {
    // Restore — some tests force isTTY = false and later tests in the same
    // suite would otherwise hang if Jest isn't configured with tty.
    Object.defineProperty(process.stdin, 'isTTY', {
      value: originalIsTTY,
      writable: true,
      configurable: true,
    });
  });

  // CHG-2026-04-23-AUDIT-9: the original inline implementation hung forever
  // here (readline.question waited for data that never arrived). The helper
  // must detect non-TTY and fail safe to "no".
  it('returns false immediately when stdin is not a TTY', async () => {
    Object.defineProperty(process.stdin, 'isTTY', {
      value: false,
      writable: true,
      configurable: true,
    });

    const start = Date.now();
    const result = await promptConfirm('Delete nonexistent? [y/N] ');
    const elapsed = Date.now() - start;

    expect(result).toBe(false);
    // Must be essentially instant — not the 30+ seconds of the old bug.
    expect(elapsed).toBeLessThan(500);
  });

  it('returns false when isTTY is undefined (edge case)', async () => {
    Object.defineProperty(process.stdin, 'isTTY', {
      value: undefined,
      writable: true,
      configurable: true,
    });

    const result = await promptConfirm('Confirm? ');
    expect(result).toBe(false);
  });
});
