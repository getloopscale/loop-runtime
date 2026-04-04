// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../../src/api/client';

describe('Plan Limit (402) Handling', () => {
  it('should create ApiError with exit code 3 for 402', () => {
    const error = ApiError.fromHttpStatus(
      402,
      'Plan limit reached: Starter allows 2 projects (current: 2). Upgrade your plan to continue.',
      'PLAN_LIMIT_EXCEEDED'
    );

    expect(error.exitCode).toBe(3);
    expect(error.statusCode).toBe(402);
    expect(error.code).toBe('PLAN_LIMIT_EXCEEDED');
    expect(error.message).toContain('Plan limit');
    expect(error.message).toContain('Upgrade');
  });

  it('should use default message when server message is null', () => {
    const error = ApiError.fromHttpStatus(402);

    expect(error.exitCode).toBe(3);
    expect(error.code).toBe('PLAN_LIMIT_EXCEEDED');
    expect(error.message).toContain('Plan limit');
  });

  it('should not contain internal terms in default 402 message', () => {
    const error = ApiError.fromHttpStatus(402);
    const internalTerms = ['Stripe', 'stripe', 'subscription', 'invoice', 'webhook', 'tenant_id'];

    for (const term of internalTerms) {
      expect(error.message).not.toContain(term);
    }
  });

  it('should serialize correctly for --json output', () => {
    const error = ApiError.fromHttpStatus(
      402,
      'Plan limit reached: Starter allows 100 builds per month.',
      'PLAN_LIMIT_EXCEEDED'
    );

    const json = {
      error: error.message,
      exit_code: error.exitCode,
      code: error.code,
    };

    expect(json.exit_code).toBe(3);
    expect(json.code).toBe('PLAN_LIMIT_EXCEEDED');
    expect(json.error).toContain('builds');
  });
});
