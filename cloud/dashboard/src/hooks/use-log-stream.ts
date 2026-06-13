import { useEffect, useRef, useState } from 'react';
import type { LogLine } from '@/lib/api/types';
import { API_URL } from '@/lib/config';
import { ensureSSEToken } from '@/lib/api';

/**
 * use-log-stream — consumes a SSE log stream into a growing line buffer.
 *
 * Uses native EventSource. In dev mode (VITE_USE_MOCKS=true), lib/mocks/sse.ts
 * shim replaces window.EventSource globally → mock log lines emit per the
 * registered emitter (buildLogLines / runtimeLogLines).
 *
 * Buffer caps at `maxLines` (default 5000) to prevent unbounded memory growth
 * for long-running streams. When cap is exceeded, oldest lines are dropped
 * (FIFO).
 *
 * `paused` lets the consumer freeze updates without disconnecting; useful
 * when the user wants to read a specific section without auto-scroll
 * interference.
 */
export interface UseLogStreamOptions {
  maxLines?: number;
  paused?: boolean;
}

export function useLogStream(
  path: string | null,
  options: UseLogStreamOptions = {},
): {
  lines: LogLine[];
  connected: boolean;
  error: boolean;
  droppedCount: number;
  clear: () => void;
} {
  const { maxLines = 5000, paused = false } = options;
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState(false);
  const [droppedCount, setDroppedCount] = useState(0);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  const bufferRef = useRef<LogLine[]>([]);

  useEffect(() => {
    if (!path) return;
    setLines([]);
    setError(false);
    setConnected(false);
    setDroppedCount(0);
    bufferRef.current = [];

    let cancelled = false;
    let es: EventSource | null = null;

    // T6.0 of plan 2026-05-27-backend-frontend-integration v1.1 (EC-04):
    // EventSource cannot set Authorization headers, so we fetch the JWT
    // first and pass it as ?token=<jwt>. The token only lives long
    // enough to open the stream; the cloud BFF then proxies upstream
    // with X-Service-Auth.
    (async () => {
      const token = await ensureSSEToken();
      if (cancelled) return;
      if (!token) {
        setError(true);
        return;
      }
      const separator = path.includes('?') ? '&' : '?';
      const url = `${API_URL}${path}${separator}token=${encodeURIComponent(token)}`;
      es = new EventSource(url);

      es.onopen = () => {
        if (!cancelled) setConnected(true);
      };

      // 2026-06-13 J-19 LIVE walkthrough fix — loopzero-api SSE emits named
       // events `event: log` + payload shape {timestamp, line, step}.
       // EventSource.onmessage only fires for UNNAMED events; named events
       // need addEventListener. Also map loopzero-api fields to LogLine shape
       // (ts/level/msg). Build-logs SSE may continue emitting unnamed —
       // keep onmessage as legacy fallback for backward compat.
      const handleEvent = (event: MessageEvent) => {
        if (cancelled) return;
        let parsed: LogLine;
        try {
          const raw = JSON.parse(event.data) as Partial<LogLine> & {
            timestamp?: string;
            line?: string;
            step?: string;
          };
          // Map loopzero-api runtime shape (timestamp/line/step) to LogLine
          // (ts/level/msg) — fields may differ for build-logs (already
          // emits ts/level/msg). Use ?? so legacy shape passes through.
          parsed = {
            ts: raw.ts ?? raw.timestamp ?? new Date().toISOString(),
            level: raw.level ?? 'info',
            msg: raw.msg ?? raw.line ?? '',
            source: raw.source ?? raw.step,
          };
        } catch {
          parsed = { ts: new Date().toISOString(), level: 'info', msg: String(event.data) };
        }
        bufferRef.current.push(parsed);
        if (bufferRef.current.length > maxLines) {
          // J-19 T1.3 (R3): track dropped lines so the viewer surfaces an
          // explicit indicator instead of silently discarding (closes EC-8
          // silent-drop gap from impl edge cases review).
          const excess = bufferRef.current.length - maxLines;
          bufferRef.current.splice(0, excess);
          setDroppedCount((prev) => prev + excess);
        }
        if (!pausedRef.current) {
          setLines([...bufferRef.current]);
        }
      };

      es.onmessage = handleEvent; // unnamed events (build-logs legacy)
      es.addEventListener('log', handleEvent); // loopzero-api named events (runtime-logs)

      es.onerror = () => {
        if (cancelled) return;
        setError(true);
        setConnected(false);
      };
    })();

    return () => {
      cancelled = true;
      es?.close();
      setConnected(false);
    };
  }, [path, maxLines]);

  return {
    lines,
    connected,
    error,
    droppedCount,
    clear: () => {
      bufferRef.current = [];
      setLines([]);
      setDroppedCount(0);
    },
  };
}
