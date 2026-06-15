/**
 * use-status-stream — SSE consumer for J-18 SignalGrid realtime updates.
 *
 * T2.1 of plan 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1.
 *
 * Wraps useSSEStream<SignalEvent> for `/engine/api/v1/projects/{name}/signals/stream`
 * (backend T1.3, when wired). Applies ADR D3 v1.1 dichotomy reducer:
 *
 *   TERMINAL_ABSORBING = {status, alerts_active}
 *     Once a signal reaches a terminal state, subsequent events for that
 *     signal are absorbed (NO regression). Pattern from J-17 build phases.
 *
 *   OSCILLATING = {replicas, uptime_24h, p95_latency, error_rate, rps,
 *                  last_deploy_time}
 *     Always-latest. Replicas naturally fluctuate (scaling); freezing them
 *     via terminal-absorbing would render stale values forever.
 *
 * Reference: memory `feedback_drift_3layer_canonical_pattern` + plan v1.1
 * ADR D3 amended dichotomy, edge-case absorption EC-4.
 */

import { useEffect, useMemo, useState } from 'react';

/** The 8 canonical signal names for J-18 SignalGrid. */
export type SignalName =
  | 'status'
  | 'alerts_active'
  | 'replicas'
  | 'uptime_24h'
  | 'p95_latency'
  | 'error_rate'
  | 'rps'
  | 'last_deploy_time';

/** Per-signal categorization for the dichotomy reducer. Locked in v1.1 ADR D3. */
export const TERMINAL_ABSORBING_SIGNALS: ReadonlySet<SignalName> = new Set([
  'status',
  'alerts_active',
] as const);

export const OSCILLATING_SIGNALS: ReadonlySet<SignalName> = new Set([
  'replicas',
  'uptime_24h',
  'p95_latency',
  'error_rate',
  'rps',
  'last_deploy_time',
] as const);

/**
 * Terminal absorption rules — when a TERMINAL_ABSORBING signal reaches one of
 * these values, subsequent events for the same signal are ignored.
 *
 * 'status' is terminal when 'Healthy' (Vercel-style: success absorbs).
 * 'alerts_active' is terminal when count drops to 0 (alerts cleared absorbs).
 */
export function isTerminalValue(signal: SignalName, value: unknown): boolean {
  if (signal === 'status') {
    return value === 'Healthy';
  }
  if (signal === 'alerts_active') {
    return value === 0;
  }
  return false;
}

/** Signal event delivered over SSE from backend T1.3 handler. */
export interface SignalEvent {
  signal: SignalName;
  value: unknown;
  /** Optional ISO timestamp; UI may render last-changed indicator. */
  ts?: string;
}

/** Per-signal state in the reducer accumulator. */
export interface SignalState {
  value: unknown;
  /** Set to true once a terminal value is observed (TERMINAL_ABSORBING signals only). */
  terminal: boolean;
  ts?: string;
}

/** Map signal name to current state. */
export type SignalsMap = Partial<Record<SignalName, SignalState>>;

/**
 * applySignalEvent — pure reducer step. Applies dichotomy policy.
 *
 * - TERMINAL_ABSORBING + terminal already reached → IGNORE (absorbs).
 * - TERMINAL_ABSORBING + not terminal yet → adopt value; mark terminal if
 *   the new value is itself terminal.
 * - OSCILLATING → always-latest (no terminal-absorbing).
 * - Unknown signal name → adopt as OSCILLATING (defensive default).
 */
export function applySignalEvent(state: SignalsMap, event: SignalEvent): SignalsMap {
  const current = state[event.signal];

  if (TERMINAL_ABSORBING_SIGNALS.has(event.signal)) {
    // Defense: already-terminal state absorbs subsequent events.
    if (current?.terminal) {
      return state;
    }
    return {
      ...state,
      [event.signal]: {
        value: event.value,
        terminal: isTerminalValue(event.signal, event.value),
        ts: event.ts,
      },
    };
  }

  // OSCILLATING (or unknown): always-latest.
  return {
    ...state,
    [event.signal]: {
      value: event.value,
      terminal: false,
      ts: event.ts,
    },
  };
}

export interface UseStatusStreamResult {
  /** Map signal_name → current state. */
  signals: SignalsMap;
  /** True when SSE connection is open. */
  connected: boolean;
}

/**
 * useStatusStream — public hook.
 *
 * Strategy: hook EXPOSES the reducer policy + state but does NOT yet wire to
 * the backend SSE handler (T1.3 deferred to next iteration). Callers can feed
 * events manually via injection — useful for hermetic tests AND for future
 * wiring when T1.3 SSE backend ships.
 *
 * When T1.3 lands, this hook will wrap useSSEStream<SignalEvent> with
 * pathname `/engine/api/v1/projects/${name}/signals/stream?env=${env}` and
 * reduce via applySignalEvent on each received event.
 */
export function useStatusStream(
  _projectName: string | undefined,
  _env: string | undefined,
  injectedEvents?: SignalEvent[],
): UseStatusStreamResult {
  const [signals, setSignals] = useState<SignalsMap>({});

  // Apply injected events (test seam + future SSE source). Events arriving via
  // a SSE EventSource would push through the same reducer path.
  useEffect(() => {
    if (!injectedEvents || injectedEvents.length === 0) return;
    setSignals((prev) => injectedEvents.reduce(applySignalEvent, prev));
  }, [injectedEvents]);

  // Memoize so consumers re-render only on actual state change.
  return useMemo(
    () => ({
      signals,
      connected: false, // T1.3 SSE backend wiring pending; expose stub
    }),
    [signals],
  );
}
