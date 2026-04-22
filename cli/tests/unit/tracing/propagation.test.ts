// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { injectTraceContext } from '../../../src/tracing/propagation';

describe('injectTraceContext', () => {
  it('returns headers unchanged when no active span', () => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    const result = injectTraceContext(headers);
    expect(result).toBe(headers);
    expect(result['Content-Type']).toBe('application/json');
  });

  it('does not crash with empty headers', () => {
    const headers: Record<string, string> = {};
    expect(() => injectTraceContext(headers)).not.toThrow();
  });

  it('preserves existing headers', () => {
    const headers: Record<string, string> = {
      'Authorization': 'Bearer token123',
      'Correlation-ID': 'abc-def',
    };
    const result = injectTraceContext(headers);
    expect(result['Authorization']).toBe('Bearer token123');
    expect(result['Correlation-ID']).toBe('abc-def');
  });
});
