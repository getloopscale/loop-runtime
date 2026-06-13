import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  MockEventSource,
  registerSSE,
  resetSSERegistrations,
  getSSERegistrationCount,
} from './sse';

describe('mocks/sse — EventSource shim', () => {
  beforeEach(() => {
    resetSSERegistrations();
    vi.useFakeTimers();
  });

  afterEach(() => {
    resetSSERegistrations();
    vi.useRealTimers();
  });

  it('test_starts_with_zero_registrations', () => {
    expect(getSSERegistrationCount()).toBe(0);
  });

  it('test_registers_emitter_for_path_pattern', () => {
    registerSSE('/api/v1/builds/{id}/logs/stream', () => undefined);
    expect(getSSERegistrationCount()).toBe(1);
  });

  it('test_open_fires_onopen_async', () => {
    registerSSE('/api/v1/builds/{id}/logs/stream', () => undefined);
    const es = new MockEventSource('/api/v1/builds/abc/logs/stream');
    expect(es.readyState).toBe(MockEventSource.CONNECTING);
    const handler = vi.fn();
    es.onopen = handler;
    vi.runAllTimers();
    expect(es.readyState).toBe(MockEventSource.OPEN);
    expect(handler).toHaveBeenCalled();
  });

  it('test_emitter_can_dispatch_messages', () => {
    registerSSE('/api/v1/builds/{id}/logs/stream', (es) => {
      setTimeout(() => {
        es.dispatchEvent(
          new MessageEvent('message', { data: JSON.stringify({ msg: 'hello' }) }),
        );
      }, 100);
    });
    const es = new MockEventSource('/api/v1/builds/abc/logs/stream');
    const handler = vi.fn();
    es.onmessage = handler;
    vi.runAllTimers();
    expect(handler).toHaveBeenCalled();
    const arg = handler.mock.calls[0][0] as MessageEvent;
    expect(JSON.parse(arg.data)).toEqual({ msg: 'hello' });
  });

  it('test_unknown_url_fires_onerror', () => {
    const es = new MockEventSource('/api/v1/totally-unknown');
    const handler = vi.fn();
    es.onerror = handler;
    vi.runAllTimers();
    expect(es.readyState).toBe(MockEventSource.CLOSED);
    expect(handler).toHaveBeenCalled();
  });

  it('test_close_stops_emission', () => {
    let cleared = false;
    registerSSE('/api/v1/builds/{id}/logs/stream', () => {
      return () => {
        cleared = true;
      };
    });
    const es = new MockEventSource('/api/v1/builds/abc/logs/stream');
    vi.runAllTimers();
    es.close();
    expect(es.readyState).toBe(MockEventSource.CLOSED);
    expect(cleared).toBe(true);
  });

  it('test_close_before_open_prevents_open_dispatch', () => {
    registerSSE('/api/v1/builds/{id}/logs/stream', () => undefined);
    const es = new MockEventSource('/api/v1/builds/abc/logs/stream');
    const handler = vi.fn();
    es.onopen = handler;
    es.close(); // synchronous close before setTimeout fires
    vi.runAllTimers();
    expect(handler).not.toHaveBeenCalled();
  });

  it('test_strips_engine_prefix', () => {
    registerSSE('/api/v1/builds/{id}/logs/stream', () => undefined);
    const es = new MockEventSource(
      '/engine/api/v1/builds/abc/logs/stream',
    );
    const handler = vi.fn();
    es.onopen = handler;
    vi.runAllTimers();
    expect(handler).toHaveBeenCalled();
  });

  it('test_addEventListener_message_routing', () => {
    registerSSE('/api/v1/x', (es) => {
      setTimeout(() => {
        es.dispatchEvent(new MessageEvent('message', { data: 'ping' }));
      }, 50);
    });
    const es = new MockEventSource('/api/v1/x');
    const listener = vi.fn();
    es.addEventListener('message', listener);
    vi.runAllTimers();
    expect(listener).toHaveBeenCalled();
  });
});
