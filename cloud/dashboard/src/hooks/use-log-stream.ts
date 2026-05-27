import { useEffect, useRef, useState } from 'react';
import type { LogLine } from '@/lib/api/types';
import { API_URL } from '@/lib/config';

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
  clear: () => void;
} {
  const { maxLines = 5000, paused = false } = options;
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState(false);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  const bufferRef = useRef<LogLine[]>([]);

  useEffect(() => {
    if (!path) return;
    setLines([]);
    setError(false);
    setConnected(false);
    bufferRef.current = [];

    const url = `${API_URL}${path}`;
    const es = new EventSource(url);

    es.onopen = () => setConnected(true);

    es.onmessage = (event: MessageEvent) => {
      let parsed: LogLine | null = null;
      try {
        parsed = JSON.parse(event.data) as LogLine;
      } catch {
        parsed = { ts: new Date().toISOString(), level: 'info', msg: String(event.data) };
      }
      if (!parsed) return;
      bufferRef.current.push(parsed);
      if (bufferRef.current.length > maxLines) {
        bufferRef.current.splice(0, bufferRef.current.length - maxLines);
      }
      if (!pausedRef.current) {
        setLines([...bufferRef.current]);
      }
    };

    es.onerror = () => {
      setError(true);
      setConnected(false);
    };

    return () => {
      es.close();
      setConnected(false);
    };
  }, [path, maxLines]);

  return {
    lines,
    connected,
    error,
    clear: () => {
      bufferRef.current = [];
      setLines([]);
    },
  };
}
