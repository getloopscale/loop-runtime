// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Defensive-shape tests for src/lib/rum.ts. Both `reportEvent` and
// `trackInteraction` must never throw — they are telemetry transports and
// any thrown exception would cascade into the React error boundary or the
// caller's hot path.
//
// Vitest defaults to import.meta.env.DEV=true; prod-mode sendBeacon path
// is verified by contract (the source file documents the URL + body shape
// and the backend handler tests in cloud/tests/unit/telemetry_test.go pin
// the receive contract). Same convention as error-reporter.test.ts.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { reportEvent, trackInteraction, viewportBucket } from './rum';

// Shared console spy. vi.restoreAllMocks() in afterEach prevents bleed
// between tests AND describe blocks — `vi.spyOn(console, 'log')` returns
// the SAME mock instance until restored, so cumulative call counts would
// otherwise span the whole file.

describe('reportEvent — telemetry transport (T1.2 / EC-11)', () => {
  let consoleSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('test_dev_mode_logs_event_with_activation_prefix', () => {
    reportEvent('onboarding_step_completed', { step: 1 });
    expect(consoleSpy).toHaveBeenCalledTimes(1);
    const [tag, payload] = consoleSpy.mock.calls[0];
    expect(tag).toBe('[activation] onboarding_step_completed');
    expect(payload).toEqual({ step: 1 });
  });

  it('test_default_empty_payload_omitted_object_logged', () => {
    reportEvent('cli_command_copied');
    expect(consoleSpy).toHaveBeenCalledTimes(1);
    const [, payload] = consoleSpy.mock.calls[0];
    expect(payload).toEqual({});
  });

  it('test_does_not_throw_on_circular_payload', () => {
    // JSON.stringify of a self-referencing object normally throws —
    // reportEvent's outer try/catch must swallow it.
    const cycle: Record<string, unknown> = { a: 1 };
    cycle.self = cycle;
    expect(() =>
      reportEvent('onboarding_dismissed', cycle as Record<string, unknown>),
    ).not.toThrow();
  });

  it('test_does_not_throw_when_sendBeacon_throws', () => {
    // Even with a hostile transport, reportEvent must not bubble.
    const originalBeacon = navigator.sendBeacon;
    Object.defineProperty(navigator, 'sendBeacon', {
      configurable: true,
      writable: true,
      value: () => {
        throw new Error('beacon transport blew up');
      },
    });
    try {
      expect(() => reportEvent('first_deploy_celebrated', { id: 'd1' })).not.toThrow();
    } finally {
      Object.defineProperty(navigator, 'sendBeacon', {
        configurable: true,
        writable: true,
        value: originalBeacon,
      });
    }
  });
});

describe('trackInteraction — UX interaction taxonomy (sponsor criterion #17)', () => {
  let consoleSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('test_dev_mode_logs_category_dot_action', () => {
    trackInteraction('rollback', 'clicked', { release_id: 'r1' });
    expect(consoleSpy).toHaveBeenCalledTimes(1);
    const [tag, payload] = consoleSpy.mock.calls[0];
    expect(tag).toBe('[interaction] rollback.clicked');
    expect(payload).toEqual({ release_id: 'r1' });
  });

  it('test_supports_all_documented_categories', () => {
    const categories: Array<
      'rollback' | 'secrets' | 'logs' | 'scale' | 'team' | 'billing' | 'domains' | 'deploys'
    > = ['rollback', 'secrets', 'logs', 'scale', 'team', 'billing', 'domains', 'deploys'];
    // Any unknown category would be a TS-only error; here we just
    // verify the runtime path accepts each documented value without
    // throwing.
    for (const c of categories) {
      expect(() => trackInteraction(c, 'tested', {})).not.toThrow();
    }
    expect(consoleSpy).toHaveBeenCalledTimes(categories.length);
  });

  it('test_default_empty_payload_logged_as_empty_object', () => {
    trackInteraction('billing', 'portal_open');
    const [, payload] = consoleSpy.mock.calls[0];
    expect(payload).toEqual({});
  });

  it('test_does_not_throw_on_circular_payload', () => {
    const cycle: Record<string, unknown> = { x: 1 };
    cycle.self = cycle;
    expect(() => trackInteraction('logs', 'open', cycle)).not.toThrow();
  });
});

// =============================================================================
// Plan 2026-05-29 T2.2 — FAANG-grade auth telemetry (D5)
// =============================================================================

describe('reportEvent — auth.method.* events (T2.2 / D5)', () => {
  let consoleSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('test_rum_accepts_auth_method_attempt_event', () => {
    reportEvent('auth.method.attempt', {
      v: 1,
      method: 'github',
      correlation_id: '11111111-2222-3333-4444-555555555555',
      viewport_bucket: 'desktop',
      timestamp: Date.now(),
    });
    expect(consoleSpy).toHaveBeenCalledTimes(1);
    const [tag, payload] = consoleSpy.mock.calls[0];
    expect(tag).toBe('[activation] auth.method.attempt');
    expect((payload as Record<string, unknown>).v).toBe(1);
    expect((payload as Record<string, unknown>).method).toBe('github');
  });

  it('test_rum_accepts_auth_method_failure_with_reason', () => {
    reportEvent('auth.method.failure', {
      v: 1,
      method: 'password',
      correlation_id: '11111111-2222-3333-4444-555555555555',
      latency_ms: 340,
      reason: 'kratos_5xx',
      viewport_bucket: 'mobile',
      timestamp: Date.now(),
    });
    expect(consoleSpy).toHaveBeenCalledTimes(1);
    const [, payload] = consoleSpy.mock.calls[0];
    expect((payload as Record<string, unknown>).reason).toBe('kratos_5xx');
    expect((payload as Record<string, unknown>).latency_ms).toBe(340);
  });

  it('test_rum_accepts_all_four_auth_method_events', () => {
    const events = [
      'auth.method.attempt',
      'auth.method.success',
      'auth.method.failure',
      'auth.method.cancelled',
    ] as const;
    for (const ev of events) {
      expect(() =>
        reportEvent(ev, {
          v: 1,
          method: 'password',
          correlation_id: 'x',
          viewport_bucket: 'desktop',
          timestamp: 0,
        }),
      ).not.toThrow();
    }
    expect(consoleSpy).toHaveBeenCalledTimes(events.length);
  });
});

describe('viewportBucket — categorical viewport (T2.2 / D5)', () => {
  let originalInnerWidth: number;

  beforeEach(() => {
    originalInnerWidth = window.innerWidth;
  });

  afterEach(() => {
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: originalInnerWidth,
    });
  });

  function setViewport(width: number): void {
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: width,
    });
  }

  it('test_rum_viewport_bucket_mobile', () => {
    setViewport(400);
    expect(viewportBucket()).toBe('mobile');
  });

  it('test_rum_viewport_bucket_mobile_at_boundary', () => {
    setViewport(639);
    expect(viewportBucket()).toBe('mobile');
  });

  it('test_rum_viewport_bucket_tablet', () => {
    setViewport(800);
    expect(viewportBucket()).toBe('tablet');
  });

  it('test_rum_viewport_bucket_tablet_at_lower_boundary', () => {
    setViewport(640);
    expect(viewportBucket()).toBe('tablet');
  });

  it('test_rum_viewport_bucket_tablet_at_upper_boundary', () => {
    setViewport(1023);
    expect(viewportBucket()).toBe('tablet');
  });

  it('test_rum_viewport_bucket_desktop', () => {
    setViewport(1440);
    expect(viewportBucket()).toBe('desktop');
  });

  it('test_rum_viewport_bucket_desktop_at_boundary', () => {
    setViewport(1024);
    expect(viewportBucket()).toBe('desktop');
  });
});
