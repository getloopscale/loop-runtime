/**
 * use-status-stream.test.tsx — hermetic vitest for T2.1 dichotomy reducer +
 * T3.1 (J-18 T1.3.b) EventSource backend wire-up.
 *
 * Validates ADR D3 v1.1 dichotomy absorption (EC-4):
 *  - TERMINAL_ABSORBING signals (status, alerts_active) — terminal absorbs
 *  - OSCILLATING signals (replicas, rps, p95, uptime, error_rate, last_deploy)
 *    — always-latest, never frozen
 *  - Signal categorization sets are exhaustive and disjoint
 *
 * T3.1 adds EventSource wire-up tests (J-18 T1.3.b plan v1.1):
 *  - EC-4 vi.stubGlobal in beforeEach BEFORE renderHook
 *  - EC-6 cleanup on unmount (es.close called)
 *  - connect via addEventListener('signal.changed') per memory feedback
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import {
  applySignalEvent,
  isTerminalValue,
  OSCILLATING_SIGNALS,
  TERMINAL_ABSORBING_SIGNALS,
  useStatusStream,
  type SignalEvent,
  type SignalName,
  type SignalsMap,
} from './use-status-stream';

/**
 * MockEventSource — minimal EventSource impl used by T3.1 tests.
 * Public _dispatch(eventName, data) drives events from test bodies.
 */
class MockEventSource {
  public url: string;
  public readyState = 0;
  public closeCalls = 0;
  private listeners = new Map<string, Set<EventListener>>();
  public static lastInstance: MockEventSource | null = null;

  constructor(url: string) {
    this.url = url;
    MockEventSource.lastInstance = this;
  }

  addEventListener(type: string, listener: EventListener): void {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, new Set());
    }
    this.listeners.get(type)!.add(listener);
  }

  removeEventListener(type: string, listener: EventListener): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closeCalls += 1;
    this.readyState = 2;
  }

  /** Test-only helper to dispatch events synchronously. */
  _dispatch(type: string, data?: unknown): void {
    const ev = new MessageEvent(type, {
      data: typeof data === 'string' ? data : JSON.stringify(data ?? {}),
    });
    this.listeners.get(type)?.forEach((l) => l(ev));
  }
}

describe('useStatusStream — T2.1 dichotomy reducer (ADR D3 v1.1)', () => {
  describe('signal categorization (EC-4 absorption)', () => {
    it('TERMINAL_ABSORBING contains exactly {status, alerts_active}', () => {
      expect(TERMINAL_ABSORBING_SIGNALS.size).toBe(2);
      expect(TERMINAL_ABSORBING_SIGNALS.has('status')).toBe(true);
      expect(TERMINAL_ABSORBING_SIGNALS.has('alerts_active')).toBe(true);
    });

    it('OSCILLATING contains 6 signals (replicas, uptime_24h, p95_latency, error_rate, rps, last_deploy_time)', () => {
      expect(OSCILLATING_SIGNALS.size).toBe(6);
      const expected: SignalName[] = [
        'replicas',
        'uptime_24h',
        'p95_latency',
        'error_rate',
        'rps',
        'last_deploy_time',
      ];
      expected.forEach((s) => expect(OSCILLATING_SIGNALS.has(s)).toBe(true));
    });

    it('TERMINAL_ABSORBING and OSCILLATING are disjoint (no signal in both)', () => {
      for (const s of TERMINAL_ABSORBING_SIGNALS) {
        expect(OSCILLATING_SIGNALS.has(s)).toBe(false);
      }
    });

    it('Together TERMINAL_ABSORBING + OSCILLATING cover 8 canonical signals', () => {
      expect(TERMINAL_ABSORBING_SIGNALS.size + OSCILLATING_SIGNALS.size).toBe(8);
    });
  });

  describe('isTerminalValue — terminal-value detection per signal', () => {
    it('status=Healthy is terminal', () => {
      expect(isTerminalValue('status', 'Healthy')).toBe(true);
    });
    it('status=Progressing is NOT terminal', () => {
      expect(isTerminalValue('status', 'Progressing')).toBe(false);
    });
    it('alerts_active=0 is terminal (alerts cleared)', () => {
      expect(isTerminalValue('alerts_active', 0)).toBe(true);
    });
    it('alerts_active=1 is NOT terminal', () => {
      expect(isTerminalValue('alerts_active', 1)).toBe(false);
    });
    it('replicas (OSCILLATING) is never terminal', () => {
      expect(isTerminalValue('replicas', 5)).toBe(false);
      expect(isTerminalValue('replicas', 0)).toBe(false);
    });
  });

  describe('applySignalEvent — TERMINAL_ABSORBING absorption (EC-4 key invariant)', () => {
    it('status: Progressing → Healthy → Progressing — terminal absorbs Progressing replay', () => {
      let state: SignalsMap = {};
      // 1. Progressing arrives
      state = applySignalEvent(state, { signal: 'status', value: 'Progressing' });
      expect(state.status?.value).toBe('Progressing');
      expect(state.status?.terminal).toBe(false);
      // 2. Healthy arrives — terminal reached
      state = applySignalEvent(state, { signal: 'status', value: 'Healthy' });
      expect(state.status?.value).toBe('Healthy');
      expect(state.status?.terminal).toBe(true);
      // 3. Replay of Progressing — must be absorbed (state UNCHANGED)
      const next = applySignalEvent(state, { signal: 'status', value: 'Progressing' });
      expect(next.status?.value).toBe('Healthy');
      expect(next.status?.terminal).toBe(true);
      // Defense: state object identity preserved when fully absorbed
      expect(next).toBe(state);
    });

    it('alerts_active: 3 → 0 → 5 — terminal absorbs 5 (alerts cleared stays cleared in this stream)', () => {
      let state: SignalsMap = {};
      state = applySignalEvent(state, { signal: 'alerts_active', value: 3 });
      expect(state.alerts_active?.value).toBe(3);
      expect(state.alerts_active?.terminal).toBe(false);
      // Cleared → terminal
      state = applySignalEvent(state, { signal: 'alerts_active', value: 0 });
      expect(state.alerts_active?.value).toBe(0);
      expect(state.alerts_active?.terminal).toBe(true);
      // New 5 arrives — absorbed (the "cleared" state holds)
      state = applySignalEvent(state, { signal: 'alerts_active', value: 5 });
      expect(state.alerts_active?.value).toBe(0);
      expect(state.alerts_active?.terminal).toBe(true);
    });
  });

  describe('applySignalEvent — OSCILLATING always-latest (EC-4 second invariant)', () => {
    it('replicas: 5 → 2 → 3 — reducer reflects 3 (NOT frozen at 5)', () => {
      let state: SignalsMap = {};
      state = applySignalEvent(state, { signal: 'replicas', value: 5 });
      expect(state.replicas?.value).toBe(5);
      state = applySignalEvent(state, { signal: 'replicas', value: 2 });
      expect(state.replicas?.value).toBe(2);
      state = applySignalEvent(state, { signal: 'replicas', value: 3 });
      expect(state.replicas?.value).toBe(3);
      // Defense: terminal flag stays false on OSCILLATING — never absorbs
      expect(state.replicas?.terminal).toBe(false);
    });

    it('rps: 100 → 50 → 200 — reducer reflects 200', () => {
      let state: SignalsMap = {};
      state = applySignalEvent(state, { signal: 'rps', value: 100 });
      state = applySignalEvent(state, { signal: 'rps', value: 50 });
      state = applySignalEvent(state, { signal: 'rps', value: 200 });
      expect(state.rps?.value).toBe(200);
      expect(state.rps?.terminal).toBe(false);
    });

    it('p95_latency, error_rate, uptime_24h, last_deploy_time: all naturally oscillate', () => {
      let state: SignalsMap = {};
      // Sequence each signal through 3 values
      const signals: SignalName[] = ['p95_latency', 'error_rate', 'uptime_24h', 'last_deploy_time'];
      for (const sig of signals) {
        state = applySignalEvent(state, { signal: sig, value: 10 });
        state = applySignalEvent(state, { signal: sig, value: 20 });
        state = applySignalEvent(state, { signal: sig, value: 30 });
        expect(state[sig]?.value).toBe(30);
        expect(state[sig]?.terminal).toBe(false);
      }
    });
  });

  describe('applySignalEvent — interleaved + multi-signal stream', () => {
    it('mixed stream: status terminal-absorbs while replicas oscillate independently', () => {
      let state: SignalsMap = {};
      const events: SignalEvent[] = [
        { signal: 'status', value: 'Progressing' },
        { signal: 'replicas', value: 5 },
        { signal: 'status', value: 'Healthy' }, // → terminal
        { signal: 'replicas', value: 2 },
        { signal: 'status', value: 'Progressing' }, // → ABSORBED
        { signal: 'replicas', value: 3 },
      ];
      state = events.reduce(applySignalEvent, state);

      // status: terminal-absorbed at Healthy
      expect(state.status?.value).toBe('Healthy');
      expect(state.status?.terminal).toBe(true);
      // replicas: latest 3 (oscillated)
      expect(state.replicas?.value).toBe(3);
      expect(state.replicas?.terminal).toBe(false);
    });

    it('reducer preserves other signals when one absorbs', () => {
      let state: SignalsMap = {};
      state = applySignalEvent(state, { signal: 'status', value: 'Healthy' });
      state = applySignalEvent(state, { signal: 'replicas', value: 5 });

      const stateAfterAbsorbedStatus = applySignalEvent(state, {
        signal: 'status',
        value: 'Progressing',
      });

      // Same reference (absorbed) — replicas state still 5
      expect(stateAfterAbsorbedStatus.replicas?.value).toBe(5);
      expect(stateAfterAbsorbedStatus.status?.value).toBe('Healthy');
    });
  });

  describe('applySignalEvent — timestamp propagation', () => {
    it('event ts is preserved in state', () => {
      const state = applySignalEvent({}, {
        signal: 'replicas',
        value: 5,
        ts: '2026-06-14T22:00:00Z',
      });
      expect(state.replicas?.ts).toBe('2026-06-14T22:00:00Z');
    });
  });
});

describe('useStatusStream — T3.1 J-18 T1.3.b EventSource backend wire-up', () => {
  beforeEach(() => {
    // EC-4 absorption: vi.stubGlobal MUST run in beforeEach BEFORE renderHook
    // so useEffect's EventSource constructor picks up the mock. Setting
    // inside `it()` AFTER renderHook would have already opened the real
    // (or undefined) EventSource.
    MockEventSource.lastInstance = null;
    vi.stubGlobal('EventSource', MockEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('opens EventSource at canonical backend URL when projectName + env provided', () => {
    renderHook(() => useStatusStream('my-app', 'production'));
    expect(MockEventSource.lastInstance).not.toBeNull();
    expect(MockEventSource.lastInstance!.url).toBe(
      '/engine/api/v1/projects/my-app/signals/stream?env=production',
    );
  });

  it('does NOT open EventSource when projectName is undefined (disabled state)', () => {
    renderHook(() => useStatusStream(undefined, 'production'));
    expect(MockEventSource.lastInstance).toBeNull();
  });

  it('does NOT open EventSource when env is undefined (disabled state)', () => {
    renderHook(() => useStatusStream('my-app', undefined));
    expect(MockEventSource.lastInstance).toBeNull();
  });

  it('connected=true after open event; signals reduce on signal.changed dispatch', () => {
    const { result } = renderHook(() => useStatusStream('my-app', 'production'));
    const mockES = MockEventSource.lastInstance!;
    expect(result.current.connected).toBe(false);

    act(() => {
      mockES._dispatch('open');
    });
    expect(result.current.connected).toBe(true);

    act(() => {
      mockES._dispatch('signal.changed', { signal: 'status', value: 'Healthy' });
    });
    expect(result.current.signals.status?.value).toBe('Healthy');
    expect(result.current.signals.status?.terminal).toBe(true);
  });

  it('EC-6 absorption: closes EventSource on unmount (no zombie connection)', () => {
    const { unmount } = renderHook(() => useStatusStream('my-app', 'production'));
    const mockES = MockEventSource.lastInstance!;
    expect(mockES.closeCalls).toBe(0);

    unmount();
    expect(mockES.closeCalls).toBe(1);
  });

  it('reduces multiple signal.changed events via canonical dichotomy reducer', () => {
    const { result } = renderHook(() => useStatusStream('my-app', 'production'));
    const mockES = MockEventSource.lastInstance!;

    act(() => {
      mockES._dispatch('signal.changed', { signal: 'replicas', value: 5 });
      mockES._dispatch('signal.changed', { signal: 'replicas', value: 2 });
      mockES._dispatch('signal.changed', { signal: 'replicas', value: 3 });
    });
    // OSCILLATING signal always-latest
    expect(result.current.signals.replicas?.value).toBe(3);

    act(() => {
      mockES._dispatch('signal.changed', { signal: 'status', value: 'Healthy' });
      mockES._dispatch('signal.changed', { signal: 'status', value: 'Progressing' });
    });
    // TERMINAL_ABSORBING signal stays Healthy
    expect(result.current.signals.status?.value).toBe('Healthy');
  });

  it('silently skips malformed signal.changed payload (does NOT crash)', () => {
    const { result } = renderHook(() => useStatusStream('my-app', 'production'));
    const mockES = MockEventSource.lastInstance!;

    act(() => {
      mockES._dispatch('signal.changed', 'NOT-VALID-JSON{{{');
    });
    // Hook still functional + signals unchanged (defensive try/catch).
    expect(result.current.signals).toEqual({});
  });
});
