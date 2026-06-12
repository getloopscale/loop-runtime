import { useEffect, useRef, useState } from 'react';
import { API_URL } from '@/lib/config';
import { ensureSSEToken, clearToken } from '@/lib/api';

/**
 * useSSEStream — generic typed SSE consumer (Plan J-17 T2.1).
 *
 * Wraps an EventSource lifecycle behind a typed event array. Each named
 * event type the caller registers in `eventTypes` is bound via
 * `addEventListener(type, handler)` so the server's `event: <type>`
 * lines flow into the same buffer.
 *
 * Features:
 * - Last-Event-ID resume on reconnect (EC-9) — the browser handles this
 *   natively. We also expose `lastEventID` for callers that want to seed
 *   the URL after a token refresh.
 * - Token refresh on persistent 401 (EC-3) — when 3 consecutive errors
 *   land without a successful open, we call `clearToken()` + the next
 *   `ensureSSEToken()` exchanges a fresh JWT.
 * - maxEvents FIFO cap (EC-10) — buffer cannot grow unbounded if the
 *   user leaves the tab in the background.
 * - `paused` lets the consumer freeze updates without disconnecting.
 *
 * Auth: SSE cannot set custom headers, so the JWT travels in `?token=`.
 * That matches the BFF pattern in use-log-stream (T6.0 of the older
 * backend-frontend integration plan).
 */
export interface SSEStreamEvent<T = unknown> {
  id: string;
  type: string;
  data: T;
}

export interface UseSSEStreamOptions<T = unknown> {
  /**
   * Names of SSE events to listen for. The default `message` listener
   * is also bound so servers using `data:` lines without an explicit
   * `event:` still deliver. Each event the listener receives is
   * pushed into the buffer with its `type`.
   */
  eventTypes?: string[];
  /** Buffer cap; oldest events dropped FIFO when exceeded. */
  maxEvents?: number;
  /** Freeze updates without closing the connection. */
  paused?: boolean;
  /** Side-effect callback on each event. Buffer state still updates. */
  onEvent?: (event: SSEStreamEvent<T>) => void;
  /**
   * Seed the initial Last-Event-ID (used after a REST snapshot so the
   * stream resumes from where the snapshot ended — EC-5).
   */
  initialLastEventID?: string | null;
}

export interface UseSSEStreamResult<T = unknown> {
  events: SSEStreamEvent<T>[];
  connected: boolean;
  error: boolean;
  lastEventID: string | null;
  clear: () => void;
}

// 3 consecutive errors without an open => assume token expired.
const TOKEN_REFRESH_THRESHOLD = 3;

export function useSSEStream<T = unknown>(
  path: string | null,
  options: UseSSEStreamOptions<T> = {},
): UseSSEStreamResult<T> {
  const {
    eventTypes = [],
    maxEvents = 1000,
    paused = false,
    onEvent,
    initialLastEventID = null,
  } = options;

  const [events, setEvents] = useState<SSEStreamEvent<T>[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState(false);
  const [lastEventID, setLastEventID] = useState<string | null>(initialLastEventID);

  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;
  const bufferRef = useRef<SSEStreamEvent<T>[]>([]);
  const lastIDRef = useRef<string | null>(initialLastEventID);

  // Stable join key for the eventTypes dep (re-binding on real change).
  const eventTypesKey = eventTypes.join('|');

  useEffect(() => {
    if (!path) {
      return;
    }
    setEvents([]);
    setError(false);
    setConnected(false);
    bufferRef.current = [];

    let cancelled = false;
    let es: EventSource | null = null;
    let failureCount = 0;

    const push = (id: string, type: string, raw: string) => {
      let data: T;
      try {
        data = JSON.parse(raw) as T;
      } catch {
        // Caller asked for typed events but server sent garbage; surface
        // the raw string under a known shape.
        data = raw as unknown as T;
      }
      const event: SSEStreamEvent<T> = { id, type, data };
      bufferRef.current.push(event);
      if (bufferRef.current.length > maxEvents) {
        bufferRef.current.splice(0, bufferRef.current.length - maxEvents);
      }
      if (id) {
        lastIDRef.current = id;
      }
      if (!pausedRef.current) {
        setEvents([...bufferRef.current]);
        setLastEventID(lastIDRef.current);
      }
      onEventRef.current?.(event);
    };

    const open = async () => {
      const token = await ensureSSEToken();
      if (cancelled) return;
      if (!token) {
        setError(true);
        return;
      }
      const separator = path.includes('?') ? '&' : '?';
      const baseUrl = `${API_URL}${path}${separator}token=${encodeURIComponent(token)}`;
      // Inject Last-Event-ID into URL because EventSource does not let
      // us set custom headers manually. Server-side handler treats
      // ?last_event_id= and the Last-Event-ID header equivalently
      // (existing stream_handler resume logic).
      const url = lastIDRef.current
        ? `${baseUrl}&last_event_id=${encodeURIComponent(lastIDRef.current)}`
        : baseUrl;

      es = new EventSource(url);

      es.onopen = () => {
        if (cancelled) return;
        failureCount = 0;
        setConnected(true);
        setError(false);
      };

      const messageHandler = (event: MessageEvent) => {
        if (cancelled) return;
        push(event.lastEventId || '', 'message', event.data);
      };
      es.onmessage = messageHandler;

      // Bind every named event type the caller asked for.
      for (const t of eventTypes) {
        es.addEventListener(t, ((event: MessageEvent) => {
          if (cancelled) return;
          push(event.lastEventId || '', t, event.data);
        }) as EventListener);
      }

      es.onerror = () => {
        if (cancelled) return;
        setError(true);
        setConnected(false);
        failureCount += 1;
        if (failureCount >= TOKEN_REFRESH_THRESHOLD) {
          // EC-3: token likely expired. Clear cached JWT so the next
          // ensureSSEToken() forces a fresh exchange. The browser will
          // auto-reconnect, but with a stale URL — close + reopen
          // explicitly so the URL gets the new token.
          clearToken();
          es?.close();
          failureCount = 0;
          if (!cancelled) {
            void open();
          }
        }
      };
    };

    void open();

    return () => {
      cancelled = true;
      es?.close();
      setConnected(false);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, eventTypesKey, maxEvents]);

  return {
    events,
    connected,
    error,
    lastEventID,
    clear: () => {
      bufferRef.current = [];
      setEvents([]);
    },
  };
}
